package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// agentBrowserInfo 是 /agent/browsers 单条记录。
type agentBrowserInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	GroupID  string `json:"groupId"`
	Running  bool   `json:"running"`
	CDPReady bool   `json:"cdpReady"`
	CDPUrl   string `json:"cdpUrl,omitempty"`
	Clients  int32  `json:"clients"`
}

// agentBrowsers 列出全部 profile 及其运行与 CDP 就绪状态。
func agentBrowsers(w http.ResponseWriter, r *http.Request) {
	rows, err := db.Query(`SELECT id, name, group_id FROM profiles ORDER BY sort DESC, created_at DESC`)
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer rows.Close()

	host := publicHost(r)
	var list []agentBrowserInfo
	for rows.Next() {
		var rawID, rawGroupID int64
		var name string
		if err := rows.Scan(&rawID, &name, &rawGroupID); err != nil {
			continue
		}
		id := encodeID(rawID)
		info := agentBrowserInfo{
			ID:      id,
			Name:    name,
			GroupID: encodeID(rawGroupID),
		}
		if rp, ok := getRunning(id); ok {
			info.Running = true
			info.Clients = rp.cdpClients.Load()
			// cdpReady channel 关闭后 port 已填充
			select {
			case <-rp.cdpReady:
				info.CDPReady = true
				info.CDPUrl = cdpBaseURL(host, id)
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

// acquireRequest 是 /agent/acquire 的请求体。
// 优先用 ID 定位，ID 为空时按 Name 精确匹配。
// Wait 为 true（默认）时等待 CDP 就绪后再返回，为 false 时立即返回。
type acquireRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Wait *bool  `json:"wait"` // 指针以区分 false 与未传
}

// acquireResponse 是 /agent/acquire 的成功响应体。
type acquireResponse struct {
	ID      string `json:"id"`
	CDPUrl  string `json:"cdpUrl"`
	WsUrl   string `json:"wsUrl,omitempty"`
	Started bool   `json:"started"`
}

const acquireCDPTimeout = 20 * time.Second

// agentAcquire 按需启动 profile 并等待 CDP 就绪，返回可直接连接的 CDP 地址。
func agentAcquire(w http.ResponseWriter, r *http.Request) {
	var req acquireRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}

	// 按名称查找时要求唯一，避免歧义
	idStr := req.ID
	if idStr == "" {
		if req.Name == "" {
			writeJSON(w, Response[any]{Code: 400, Message: "id or name required"})
			return
		}
		rows, err := db.Query("SELECT id FROM profiles WHERE name=?", req.Name)
		if err != nil {
			writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
			return
		}
		var rawIDs []int64
		for rows.Next() {
			var rawID int64
			rows.Scan(&rawID)
			rawIDs = append(rawIDs, rawID)
		}
		rows.Close()
		switch len(rawIDs) {
		case 0:
			writeJSON(w, Response[any]{Code: 404, Message: fmt.Sprintf("profile %q not found", req.Name)})
			return
		case 1:
			idStr = encodeID(rawIDs[0])
		default:
			candidates := make([]string, len(rawIDs))
			for i, rid := range rawIDs {
				candidates[i] = encodeID(rid)
			}
			writeJSON(w, Response[any]{Code: 409, Message: fmt.Sprintf(
				"multiple profiles named %q: %v", req.Name, candidates)})
			return
		}
	}

	rp, started, err := startProfile(idStr)
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

	host := publicHost(r)
	resp := acquireResponse{
		ID:      idStr,
		CDPUrl:  cdpBaseURL(host, idStr),
		Started: started,
	}
	// 如果 CDP 已就绪，附带 browser 级别的 WebSocket 地址方便直连
	if port := rp.cdpPort(); port > 0 {
		resp.WsUrl = fmt.Sprintf("ws://%s/cdp/%s/json/version", host, idStr)
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: resp})
}

// releaseRequest 是 /agent/release 的请求体。
type releaseRequest struct {
	ID   string `json:"id"`
	Stop bool   `json:"stop"` // true 时顺便关闭浏览器
}

// agentRelease 释放对 profile 的占用。
func agentRelease(w http.ResponseWriter, r *http.Request) {
	var req releaseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	if req.Stop {
		rp, ok := getRunning(req.ID)
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
