package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// devToolsPortFile 是 Chromium 启动后写��实际调试端口的文件名，
	// 位于 user-data-dir 根目录，首行为端口号，次行为 browser target 路径��
	devToolsPortFile = "DevToolsActivePort"

	// cdpReadyTimeout 是等待 Chromium 写出调试端口的上限。
	cdpReadyTimeout = 15 * time.Second

	// cdpPollInterval 是轮询 DevToolsActivePort 的间隔。
	cdpPollInterval = 100 * time.Millisecond

	// CDP HTTP 请求只需要传输小型协议消息；WebSocket 升级请求不受此限制。
	cdpMaxRequestBody                    = 1 << 20
	cdpMaxJSONResponse                   = 4 << 20
	defaultMaxCDPClientsPerProfile int32 = 32
)

var maxCDPClientsPerProfile = defaultMaxCDPClientsPerProfile

// waitDevToolsPort 轮询 user-data-dir 下的 DevToolsActivePort，返回 Chromium 自选的调试端口。
// abort 关闭（浏览器进程已退出）时立即放弃等待。
func waitDevToolsPort(userDataDir string, timeout time.Duration, abort <-chan struct{}) (int, error) {
	path := userDataDir + string(os.PathSeparator) + devToolsPortFile
	deadline := time.Now().Add(timeout)
	ticker := time.NewTicker(cdpPollInterval)
	defer ticker.Stop()

	for {
		if port, err := readDevToolsPort(path); err == nil {
			return port, nil
		}
		select {
		case <-abort:
			return 0, errors.New("browser exited before devtools port was written")
		case <-ticker.C:
			if time.Now().After(deadline) {
				return 0, fmt.Errorf("timed out after %s waiting for %s", timeout, devToolsPortFile)
			}
		}
	}
}

// readDevToolsPort 读取端口文件首行。文件可能被 Chromium 写到一半，解析失败即视为未就绪。
func readDevToolsPort(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	line, _, ok := strings.Cut(string(data), "\n")
	if !ok {
		// 次行尚未写入，说明文件不完整
		return 0, errors.New("incomplete port file")
	}
	port, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || port <= 0 {
		return 0, errors.New("invalid port")
	}
	return port, nil
}

// cdpProxyHandler 把 /cdp/{id}/... 转发到对应实例的 Chromium 调试端口。
//
// 直接暴露调试端口不可行，此代理解决三件事：
//  1. Chrome 111+ 校验 Host 头，非 localhost/IP 一律 403 —— 转发时改写为 127.0.0.1:port
//  2. /json/* 返回的 webSocketDebuggerUrl 指向 127.0.0.1:随机端口，
//     外部客户端拿到后会连自己的回环地址 —— 响应体中重写为本代理地址
//  3. 端口随机且每实例不同 —— 用稳定的 profile ID 作为路由键
//
// WebSocket 无需特殊处理：标准库 ReverseProxy 收到 101 后接管为裸 TCP 双向拷贝。
func cdpProxyHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rp, ok := getRunning(id)
	if !ok {
		http.Error(w, "profile not running", http.StatusNotFound)
		return
	}
	port := rp.cdpPort()
	if port == 0 {
		http.Error(w, "devtools endpoint not ready", http.StatusServiceUnavailable)
		return
	}
	if !tryAcquireCDPClient(rp) {
		http.Error(w, "too many cdp clients", http.StatusTooManyRequests)
		return
	}
	defer rp.cdpClients.Add(-1)
	if !isWebSocketUpgrade(r) {
		controller := http.NewResponseController(w)
		if err := controller.SetReadDeadline(time.Now().Add(10 * time.Second)); err == nil {
			defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
		}
		r.Body = http.MaxBytesReader(w, r.Body, cdpMaxRequestBody)
	}

	// 剥掉 /cdp/{id} 前缀，其余原样转发给 Chromium
	prefix := "/cdp/" + id
	outPath := strings.TrimPrefix(r.URL.Path, prefix)
	if outPath == "" {
		outPath = "/"
	}

	target := fmt.Sprintf("127.0.0.1:%d", port)
	publicBase := publicHost(r) + prefix

	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = target
			pr.Out.URL.Path = outPath
			// 置空则由 URL.Host 充当 Host 头，从而通过 Chrome 的 Host 白名单校验
			pr.Out.Host = ""
			// Chrome 会校验 WebSocket 的 Origin，删掉即豁免
			pr.Out.Header.Del("Origin")
			// 关闭压缩协商，保证 ModifyResponse 拿到的是明文 JSON
			pr.Out.Header.Set("Accept-Encoding", "identity")
		},
		ModifyResponse: func(resp *http.Response) error {
			// 只有 /json 系列端点的响应体里含有需要改写的地址
			if !strings.HasPrefix(outPath, "/json") {
				return nil
			}
			return rewriteCDPEndpoints(resp, port, publicBase)
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			log.Printf("[CDP] proxy error for profile %s: %v", id, err)
			http.Error(w, "cdp upstream error", http.StatusBadGateway)
		},
	}

	proxy.ServeHTTP(w, r)
}

func tryAcquireCDPClient(rp *runningProfile) bool {
	for {
		current := rp.cdpClients.Load()
		if current >= maxCDPClientsPerProfile {
			return false
		}
		if rp.cdpClients.CompareAndSwap(current, current+1) {
			return true
		}
	}
}

func isWebSocketUpgrade(r *http.Request) bool {
	if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	for _, value := range r.Header.Values("Connection") {
		for _, token := range strings.Split(value, ",") {
			if strings.EqualFold(strings.TrimSpace(token), "upgrade") {
				return true
			}
		}
	}
	return false
}

// rewriteCDPEndpoints 把响应体中指向 Chromium 本地调试端口的地址替换为本代理的对外地址，
// 覆盖 webSocketDebuggerUrl 与 devtoolsFrontendUrl 两类字段。
func rewriteCDPEndpoints(resp *http.Response, port int, publicBase string) error {
	body, err := io.ReadAll(io.LimitReader(resp.Body, cdpMaxJSONResponse+1))
	resp.Body.Close()
	if err != nil {
		return err
	}
	if len(body) > cdpMaxJSONResponse {
		return fmt.Errorf("cdp response exceeds %d bytes", cdpMaxJSONResponse)
	}

	local := strconv.Itoa(port)
	for _, host := range []string{"127.0.0.1:" + local, "localhost:" + local, "[::1]:" + local} {
		body = bytes.ReplaceAll(body, []byte(host), []byte(publicBase))
	}

	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	resp.Header.Set("Content-Length", strconv.Itoa(len(body)))
	return nil
}

// publicHost 返回客户端访问本服务所用的主机地址。
// 优先取入站 Host 头，这样无论 agent 用服务名、容器名还是 IP 访问，
// 拿回的 WebSocket 地址都能原路连回。
func publicHost(r *http.Request) string {
	if r.Host != "" {
		return r.Host
	}
	return agentPublicHost
}

// cdpBaseURL 拼出某个实例的 CDP 接入地址，供 agent 接口与前端展示。
func cdpBaseURL(host, id string) string {
	return (&url.URL{Scheme: "http", Host: host, Path: "/cdp/" + id}).String()
}
