package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// ---- readDevToolsPort ----

func TestReadDevToolsPort_OK(t *testing.T) {
	// Chromium 写入 "端口\nbrowser 路径" 且不带结尾换行；browser target 可被发现时路径不含 guid
	tests := []struct{ content, browserPath string }{
		{"54321\n/devtools/browser/abc", "/devtools/browser/abc"},
		{"54321\n/devtools/browser/abc\n", "/devtools/browser/abc"},
		{"54321\n/devtools/browser", "/devtools/browser"},
	}
	for _, tt := range tests {
		p := filepath.Join(t.TempDir(), devToolsPortFile)
		os.WriteFile(p, []byte(tt.content), 0644)

		port, browserPath, err := readDevToolsPort(p)
		if err != nil {
			t.Fatalf("readDevToolsPort(%q): unexpected error: %v", tt.content, err)
		}
		if port != 54321 || browserPath != tt.browserPath {
			t.Fatalf("readDevToolsPort(%q) = (%d, %q), want (54321, %q)", tt.content, port, browserPath, tt.browserPath)
		}
	}
}

func TestReadDevToolsPort_Missing(t *testing.T) {
	_, _, err := readDevToolsPort(filepath.Join(t.TempDir(), "nonexistent"))
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestReadDevToolsPort_Incomplete(t *testing.T) {
	// 次行尚未写入、为空或只写了一半 —— 都视为文件不完整
	for _, content := range []string{"54321", "54321\n", "54321\n/devtools/brow", "54321\n/devtools/browser/"} {
		p := filepath.Join(t.TempDir(), devToolsPortFile)
		os.WriteFile(p, []byte(content), 0644)

		if _, _, err := readDevToolsPort(p); err == nil {
			t.Fatalf("readDevToolsPort(%q): expected error for incomplete file", content)
		}
	}
}

func TestReadDevToolsPort_InvalidPort(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, devToolsPortFile)
	os.WriteFile(p, []byte("notaport\n/devtools/browser/abc\n"), 0644)

	_, _, err := readDevToolsPort(p)
	if err == nil {
		t.Fatal("expected error for invalid port")
	}
}

// ---- waitDevToolsPort ----

func TestWaitDevToolsPort_AbortOnClose(t *testing.T) {
	dir := t.TempDir()
	abort := make(chan struct{})
	close(abort) // 模拟浏览器立即退出

	_, _, err := waitDevToolsPort(dir, 5*time.Second, abort)
	if err == nil {
		t.Fatal("expected error when abort channel closed")
	}
}

func TestWaitDevToolsPort_FileWrittenLater(t *testing.T) {
	dir := t.TempDir()
	abort := make(chan struct{})
	portFile := filepath.Join(dir, devToolsPortFile)

	// 100ms 后写入端口文件，模拟 Chromium 启动延迟
	go func() {
		time.Sleep(100 * time.Millisecond)
		os.WriteFile(portFile, []byte("12345\n/devtools/browser/x\n"), 0644)
	}()

	port, browserPath, err := waitDevToolsPort(dir, 2*time.Second, abort)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if port != 12345 || browserPath != "/devtools/browser/x" {
		t.Fatalf("want (12345, /devtools/browser/x), got (%d, %q)", port, browserPath)
	}
}

// ---- rewriteCDPEndpoints ----

func TestRewriteCDPEndpoints_ReplacesAll(t *testing.T) {
	port := 49876
	publicBase := "chromium-manager:10102/cdp/Xk3mP9qR"

	original := fmt.Sprintf(
		`[{"webSocketDebuggerUrl":"ws://127.0.0.1:%d/devtools/browser/abc"},`+
			`{"devtoolsFrontendUrl":"http://127.0.0.1:%d/devtools/inspector.html"},`+
			`{"extra":"ws://localhost:%d/other"}]`,
		port, port, port,
	)

	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(original)),
	}
	if err := rewriteCDPEndpoints(resp, port, publicBase); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	body, _ := io.ReadAll(resp.Body)
	result := string(body)

	old := fmt.Sprintf("127.0.0.1:%d", port)
	if strings.Contains(result, old) {
		t.Errorf("result still contains %q:\n%s", old, result)
	}
	oldLocal := fmt.Sprintf("localhost:%d", port)
	if strings.Contains(result, oldLocal) {
		t.Errorf("result still contains %q:\n%s", oldLocal, result)
	}
	if !strings.Contains(result, publicBase) {
		t.Errorf("result does not contain publicBase %q:\n%s", publicBase, result)
	}
	// Content-Length 应当被更新
	wantLen := fmt.Sprintf("%d", len(body))
	if got := resp.Header.Get("Content-Length"); got != wantLen {
		t.Errorf("Content-Length: want %s, got %s", wantLen, got)
	}
}

func TestRewriteCDPEndpoints_RejectsOversizedResponse(t *testing.T) {
	resp := &http.Response{
		Header: http.Header{"Content-Type": []string{"application/json"}},
		Body:   io.NopCloser(strings.NewReader(strings.Repeat("x", cdpMaxJSONResponse+1))),
	}
	if err := rewriteCDPEndpoints(resp, 12345, "localhost/cdp/test"); err == nil {
		t.Fatal("expected oversized response error")
	}
}

func TestTryAcquireCDPClientLimit(t *testing.T) {
	rp := &runningProfile{}
	for i := int32(0); i < maxCDPClientsPerProfile; i++ {
		if !tryAcquireCDPClient(rp) {
			t.Fatalf("client %d unexpectedly rejected", i+1)
		}
	}
	if tryAcquireCDPClient(rp) {
		t.Fatal("client accepted after reaching the limit")
	}
	if got := rp.cdpClients.Load(); got != maxCDPClientsPerProfile {
		t.Fatalf("client count = %d, want %d", got, maxCDPClientsPerProfile)
	}
}

func TestIsWebSocketUpgradeRequiresHandshakeHeaders(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		upgrade    string
		connection string
		want       bool
	}{
		{name: "valid", method: http.MethodGet, upgrade: "websocket", connection: "keep-alive, Upgrade", want: true},
		{name: "missing connection", method: http.MethodGet, upgrade: "websocket", want: false},
		{name: "wrong method", method: http.MethodPost, upgrade: "websocket", connection: "Upgrade", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, "/cdp/id/path", nil)
			req.Header.Set("Upgrade", tt.upgrade)
			if tt.connection != "" {
				req.Header.Set("Connection", tt.connection)
			}
			if got := isWebSocketUpgrade(req); got != tt.want {
				t.Fatalf("isWebSocketUpgrade() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---- cdpProxyHandler（整合测试）----
// 用 httptest 伪造一个 Chrome DevTools HTTP/WebSocket 服务，
// 按名称登记到临时库与 runningProfiles 后通过 cdpProxyHandler 代理访问，
// 断言 /json/version 响应体里的地址被改写，以及 WebSocket 升级能穿透。

func TestCDPProxyHandler_JsonVersionRewrite(t *testing.T) {
	useTestDB(t)

	// 1. 伪造 Chrome DevTools 后端
	fakePort := 0
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/json/version" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w,
				`{"webSocketDebuggerUrl":"ws://127.0.0.1:%d/devtools/browser/abc"}`,
				fakePort)
			return
		}
		http.NotFound(w, r)
	}))
	defer fakeSrv.Close()

	// 拿到实际监听端口
	parts := strings.Split(fakeSrv.URL, ":")
	fmt.Sscanf(parts[len(parts)-1], "%d", &fakePort)

	// 2. 登记运行中的实例；名称含中文与空格，验证路由与改写后的地址都保持转义
	rp := &runningProfile{
		cdpReady:            make(chan struct{}),
		devtoolsPort:        fakePort,
		devtoolsBrowserPath: "/devtools/browser/abc",
	}
	close(rp.cdpReady)
	putRunning(t, insertTestProfile(t, "香港 01", 0), rp)

	// 3. 通过带 PathValue 的 mux 代理请求 /json/version
	// 必须经过 ServeMux 注册，r.PathValue("name") 才能正确解析
	mux := http.NewServeMux()
	mux.HandleFunc("/cdp/{name}/{path...}", cdpProxyHandler)
	proxySrv := httptest.NewServer(mux)
	defer proxySrv.Close()

	const escapedName = "%E9%A6%99%E6%B8%AF%2001"
	resp, err := http.Get(proxySrv.URL + "/cdp/" + escapedName + "/json/version")
	if err != nil {
		t.Fatalf("GET failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	result := string(body)

	// 127.0.0.1:fakePort 应被替换为代理地址
	old := fmt.Sprintf("127.0.0.1:%d", fakePort)
	if strings.Contains(result, old) {
		t.Errorf("response still contains raw address %q:\n%s", old, result)
	}
	// 入站 Host 是 proxySrv 的地址，拼上转义后的 /cdp/{name}
	want := "ws://" + strings.TrimPrefix(proxySrv.URL, "http://") + "/cdp/" + escapedName + "/devtools/browser/abc"
	if !strings.Contains(result, want) {
		t.Errorf("response does not contain rewritten url %q:\n%s", want, result)
	}
}

// 不认识的名称、未运行的实例都应 404；agent 面也不再接受内部 ID。
func TestCDPProxyHandler_NotFound(t *testing.T) {
	useTestDB(t)
	insertTestProfile(t, "stopped", 0)
	runningID := insertTestProfile(t, "running", 0)
	putRunning(t, runningID, &runningProfile{})

	mux := http.NewServeMux()
	mux.HandleFunc("/cdp/{name}/{path...}", cdpProxyHandler)
	proxySrv := httptest.NewServer(mux)
	defer proxySrv.Close()

	for _, key := range []string{"missing", "stopped", runningID} {
		resp, err := http.Get(proxySrv.URL + "/cdp/" + key + "/json/version")
		if err != nil {
			t.Fatalf("GET %s failed: %v", key, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("GET /cdp/%s: want 404, got %d", key, resp.StatusCode)
		}
	}
}

// 请求固定的 /devtools/browser 时，代理应补上本次启动的 guid 再升级为 WebSocket。
func TestCDPProxyHandler_WebSocketUpgrade(t *testing.T) {
	useTestDB(t)

	// 伪造支持 WebSocket 升级的后端，记录实际收到的路径
	var wsFrameReceived atomic.Bool
	var upstreamPath atomic.Value
	fakeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamPath.Store(r.URL.Path)
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
			return
		}
		// 手动完成升级握手（不做完整的 WebSocket 帧解析，只验证连通性）
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Errorf("hijack failed: %v", err)
			return
		}
		defer conn.Close()
		fmt.Fprintf(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		// 读一行确认代理转发的数据到达了后端
		line, _ := buf.ReadString('\n')
		if strings.TrimSpace(line) == "ping" {
			wsFrameReceived.Store(true)
		}
	}))
	defer fakeSrv.Close()

	var fakePort int
	parts := strings.Split(fakeSrv.URL, ":")
	fmt.Sscanf(parts[len(parts)-1], "%d", &fakePort)

	// 名称含斜杠，验证转义后仍作为单个路径段路由
	rp := &runningProfile{
		cdpReady:            make(chan struct{}),
		devtoolsPort:        fakePort,
		devtoolsBrowserPath: "/devtools/browser/abc",
	}
	close(rp.cdpReady)
	putRunning(t, insertTestProfile(t, "team/a", 0), rp)

	// 代理 server
	mux := http.NewServeMux()
	mux.HandleFunc("/cdp/{name}/{path...}", cdpProxyHandler)
	proxySrv := httptest.NewServer(mux)
	defer proxySrv.Close()

	// 用 net.Dial 发原始 HTTP 升级请求，避免 http.Transport 收走连接
	proxyAddr := strings.TrimPrefix(proxySrv.URL, "http://")
	path := "/cdp/team%2Fa" + cdpStableBrowserPath

	rawConn, err := dialHTTPUpgrade(proxyAddr, path)
	if err != nil {
		t.Fatalf("websocket upgrade failed: %v", err)
	}
	defer rawConn.Close()

	// 发送数据，验证穿透到后端
	fmt.Fprintf(rawConn, "ping\n")
	time.Sleep(80 * time.Millisecond)
	if !wsFrameReceived.Load() {
		t.Error("data did not reach the backend through the websocket proxy")
	}
	if got := upstreamPath.Load(); got != "/devtools/browser/abc" {
		t.Errorf("upstream path = %v, want /devtools/browser/abc", got)
	}
}

// dialTCP 建立到指定地址的 TCP 连接。
func dialTCP(addr string) (net.Conn, error) {
	return net.Dial("tcp", addr)
}

// 使用 net.Dial 而非 http.Transport，以便在收到 101 后继续读写同一 TCP 连接。
func dialHTTPUpgrade(addr, path string) (io.ReadWriteCloser, error) {
	conn, err := dialTCP(addr)
	if err != nil {
		return nil, err
	}

	req := fmt.Sprintf(
		"GET %s HTTP/1.1\r\nHost: %s\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n"+
			"Sec-WebSocket-Version: 13\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n\r\n",
		path, addr,
	)
	if _, err := fmt.Fprint(conn, req); err != nil {
		conn.Close()
		return nil, err
	}

	// 读响应行直到空行，确认 101
	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !strings.Contains(statusLine, "101") {
		conn.Close()
		return nil, fmt.Errorf("want 101 in status line, got: %s", statusLine)
	}
	// 消费剩余 headers
	for {
		line, err := reader.ReadString('\n')
		if err != nil || strings.TrimSpace(line) == "" {
			break
		}
	}
	return conn, nil
}
