package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

const (
	// Agent 请求体只包含配置名称和少量控制字段。
	agentMaxRequestBody        = 16 << 10
	defaultAgentOperationLimit = 32
	agentBodyReadTimeout       = 10 * time.Second
)

var (
	agentOperationLimit = defaultAgentOperationLimit
	agentOperationSlots chan struct{}
)

// beginAgentOperation 限制会占用数据库或等待浏览器的 Agent 请求数量。
func beginAgentOperation(w http.ResponseWriter) func() {
	select {
	case agentOperationSlots <- struct{}{}:
		return func() { <-agentOperationSlots }
	default:
		writeJSONStatus(w, http.StatusTooManyRequests, Response[any]{
			Code:    http.StatusTooManyRequests,
			Message: "too many agent requests",
		})
		return nil
	}
}

func decodeAgentJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	controller := http.NewResponseController(w)
	if err := controller.SetReadDeadline(time.Now().Add(agentBodyReadTimeout)); err == nil {
		defer func() { _ = controller.SetReadDeadline(time.Time{}) }()
	}
	r.Body = http.MaxBytesReader(w, r.Body, agentMaxRequestBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return false
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return false
	}
	return true
}

// agentBrowserInfo 是 /agent/browsers 单条记录。
// agent 面一律以配置名称标识 profile，不对外暴露内部 ID。
type agentBrowserInfo struct {
	Name     string `json:"name"`
	Group    string `json:"group"` // 所属分组名称，未分组为空
	Running  bool   `json:"running"`
	CDPReady bool   `json:"cdpReady"`
	CDPUrl   string `json:"cdpUrl,omitempty"`
	WsUrl    string `json:"wsUrl,omitempty"`
	Clients  int32  `json:"clients"`
}

// agentBrowsers 列出全部 profile 及其运行与 CDP 就绪状态。
func agentBrowsers(w http.ResponseWriter, r *http.Request) {
	release := beginAgentOperation(w)
	if release == nil {
		return
	}
	defer release()

	rows, err := db.Query(`SELECT p.id, p.name, COALESCE(g.name, '') FROM profiles p
		LEFT JOIN groups g ON g.id = p.group_id
		ORDER BY p.sort DESC, p.created_at DESC`)
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer rows.Close()

	host := publicHost(r)
	var list []agentBrowserInfo
	for rows.Next() {
		var rawID int64
		var info agentBrowserInfo
		if err := rows.Scan(&rawID, &info.Name, &info.Group); err != nil {
			continue
		}
		if rp, ok := getRunning(encodeID(rawID)); ok {
			info.Running = true
			info.Clients = rp.cdpClients.Load()
			// cdpReady channel 关闭后 port 已填充
			select {
			case <-rp.cdpReady:
				info.CDPReady = true
				info.CDPUrl = cdpBaseURL(host, info.Name)
				info.WsUrl = cdpBrowserWsURL(host, info.Name)
			default:
			}
		}
		list = append(list, info)
	}
	if list == nil {
		list = []agentBrowserInfo{}
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: list})
}

// resolveProfileName 按名称定位 profile，返回运行表所用的内部 ID；失败时附带响应码。
// 名称应全局唯一（见 initDB），旧库遗留的跨分组重名报 409 并列出所在分组，由用户改名消歧。
func resolveProfileName(name string) (string, int, error) {
	rows, err := db.Query(`SELECT p.id, COALESCE(g.name, '') FROM profiles p
		LEFT JOIN groups g ON g.id = p.group_id WHERE p.name = ?`, name)
	if err != nil {
		return "", 500, err
	}
	defer rows.Close()

	var rawIDs []int64
	var groups []string
	for rows.Next() {
		var rawID int64
		var group string
		if err := rows.Scan(&rawID, &group); err != nil {
			return "", 500, err
		}
		if group == "" {
			group = "(ungrouped)"
		}
		rawIDs = append(rawIDs, rawID)
		groups = append(groups, group)
	}
	if err := rows.Err(); err != nil {
		return "", 500, err
	}

	switch len(rawIDs) {
	case 0:
		return "", 404, fmt.Errorf("profile %q not found", name)
	case 1:
		return encodeID(rawIDs[0]), 0, nil
	default:
		return "", 409, fmt.Errorf("multiple profiles named %q in groups %q", name, groups)
	}
}

// agentProfileID 按请求体里的配置名称定位 profile，失败时已写出错误响应，调用方直接返回即可。
func agentProfileID(w http.ResponseWriter, name string) (string, bool) {
	if name == "" {
		writeJSON(w, Response[any]{Code: 400, Message: "name required"})
		return "", false
	}
	id, code, err := resolveProfileName(name)
	if err != nil {
		writeJSON(w, Response[any]{Code: code, Message: err.Error()})
		return "", false
	}
	return id, true
}

// acquireRequest 是 /agent/acquire 的请求体。
// Wait 为 true（默认）时等待 CDP 就绪后再返回，为 false 时立即返回。
type acquireRequest struct {
	Name string `json:"name"`
	Wait *bool  `json:"wait"` // 指针以区分 false 与未传
}

// acquireResponse 是 /agent/acquire 的成功响应体。
type acquireResponse struct {
	Name    string `json:"name"`
	CDPUrl  string `json:"cdpUrl"`
	WsUrl   string `json:"wsUrl"`
	Started bool   `json:"started"`
}

const acquireCDPTimeout = 20 * time.Second

// agentAcquire 按需启动 profile 并等待 CDP 就绪，返回可直接连接的 CDP 地址。
func agentAcquire(w http.ResponseWriter, r *http.Request) {
	var req acquireRequest
	release := beginAgentOperation(w)
	if release == nil {
		return
	}
	defer release()
	if !decodeAgentJSON(w, r, &req) {
		return
	}
	id, ok := agentProfileID(w, req.Name)
	if !ok {
		return
	}

	rp, started, err := startProfile(id)
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}

	// wait 默认为 true
	doWait := req.Wait == nil || *req.Wait
	if doWait {
		timer := time.NewTimer(acquireCDPTimeout)
		defer timer.Stop()
		select {
		case <-rp.cdpReady:
			// CDP 就绪，继续组装响应
		case <-rp.done:
			writeJSON(w, Response[any]{Code: 500, Message: "browser exited before cdp was ready"})
			return
		case <-timer.C:
			writeJSON(w, Response[any]{Code: 504, Message: "timed out waiting for cdp endpoint"})
			return
		}
	}

	// 两个地址都以名称寻址、不含本次启动的 guid，浏览器重启后依然有效
	host := publicHost(r)
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: acquireResponse{
		Name:    req.Name,
		CDPUrl:  cdpBaseURL(host, req.Name),
		WsUrl:   cdpBrowserWsURL(host, req.Name),
		Started: started,
	}})
}

// releaseRequest 是 /agent/release 的请求体。
type releaseRequest struct {
	Name string `json:"name"`
	Stop bool   `json:"stop"` // true 时顺便关闭浏览器
}

// agentRelease 释放对 profile 的占用。
func agentRelease(w http.ResponseWriter, r *http.Request) {
	var req releaseRequest
	release := beginAgentOperation(w)
	if release == nil {
		return
	}
	defer release()
	if !decodeAgentJSON(w, r, &req) {
		return
	}
	id, ok := agentProfileID(w, req.Name)
	if !ok {
		return
	}
	if req.Stop {
		rp, ok := getRunning(id)
		if !ok {
			writeJSON(w, Response[any]{Code: 404, Message: "not running"})
			return
		}
		stopRunning(rp)
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

// getAgentConfig 供前端拼接 CDP 地址用：返回 agent 面是否开启及其监听端口号。
// 该接口挂在管理面（:10101），不经过 token 校验，前端正常使用即可。
// 前端用 window.location.hostname + port 拼出 agent 面地址，
// 这样 UI 从任意客户端访问都能得到正确的 agent 接入地址。
func getAgentConfig(w http.ResponseWriter, r *http.Request) {
	type agentConfig struct {
		Enabled bool   `json:"enabled"`
		Port    string `json:"port,omitempty"`
	}
	cfg := agentConfig{Enabled: agentEnabled()}
	if cfg.Enabled {
		// agentAddr 形如 "0.0.0.0:10102" 或 ":10102"，只取端口部分
		_, port, err := net.SplitHostPort(agentAddr)
		if err == nil {
			cfg.Port = port
		}
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: cfg})
}
