package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

func getProfiles(w http.ResponseWriter, r *http.Request) {
	groupIDStr := r.URL.Query().Get("groupId")
	proxyIDStr := r.URL.Query().Get("proxyId")
	keyword := r.URL.Query().Get("keyword")
	page, pageSize := 1, 10
	fmt.Sscanf(r.URL.Query().Get("page"), "%d", &page)
	fmt.Sscanf(r.URL.Query().Get("pageSize"), "%d", &pageSize)

	// Clamp pagination parameters to reasonable bounds
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 200 {
		pageSize = 10
	}

	query := `SELECT p.id, p.name, p.group_id, p.sort, p.proxy, p.args, p.fingerprint, p.notes, p.created_at, p.updated_at
		FROM profiles p LEFT JOIN proxies px ON p.proxy = px.id WHERE 1=1`
	var args []interface{}
	if rawGroupID := decodeID(groupIDStr); rawGroupID > 0 {
		query += " AND p.group_id = ?"
		args = append(args, rawGroupID)
	}
	if rawProxyID := decodeID(proxyIDStr); rawProxyID > 0 {
		query += " AND p.proxy = ?"
		args = append(args, rawProxyID)
	}
	if keyword != "" {
		query += " AND (p.name LIKE ? OR px.ip LIKE ? OR px.lang LIKE ? OR px.timezone LIKE ?)"
		k := "%" + keyword + "%"
		args = append(args, k, k, k, k)
	}

	var total int
	if err := db.QueryRow("SELECT COUNT(*) FROM ("+query+")", args...).Scan(&total); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: "count query failed: " + err.Error()})
		return
	}

	query += " ORDER BY p.sort DESC, p.created_at DESC LIMIT ? OFFSET ?"
	args = append(args, pageSize, (page-1)*pageSize)

	rows, err := db.Query(query, args...)
	if err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	defer rows.Close()

	var list []Profile
	for rows.Next() {
		var p Profile
		var rawID, rawGroupID, rawProxy int64
		if err := rows.Scan(&rawID, &p.Name, &rawGroupID, &p.Sort, &rawProxy, &p.Args, &p.Fingerprint, &p.Notes, &p.CreatedAt, &p.UpdatedAt); err == nil {
			p.ID = encodeID(rawID)
			if rawGroupID > 0 {
				p.GroupID = encodeID(rawGroupID)
			}
			if rawProxy > 0 {
				p.Proxy = encodeID(rawProxy)
			}
			list = append(list, p)
		}
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: map[string]interface{}{
		"list":  list,
		"total": total,
	}})
}

func getProfile(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	rawID := decodeID(idStr)
	if rawID <= 0 {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	var p Profile
	var rawGroupID, rawProxy int64
	err := db.QueryRow("SELECT id, name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at FROM profiles WHERE id=?", rawID).
		Scan(&rawID, &p.Name, &rawGroupID, &p.Sort, &rawProxy, &p.Args, &p.Fingerprint, &p.Notes, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		writeJSON(w, Response[any]{Code: 404, Message: "profile not found"})
		return
	}
	p.ID = encodeID(rawID)
	if rawGroupID > 0 {
		p.GroupID = encodeID(rawGroupID)
	}
	if rawProxy > 0 {
		p.Proxy = encodeID(rawProxy)
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func addProfile(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	p.CreatedAt = time.Now().UnixMilli()
	p.UpdatedAt = p.CreatedAt

	enrichFingerprint(&p, true)

	rawGroupID := decodeID(p.GroupID)
	rawProxy := decodeID(p.Proxy)

	res, err := db.Exec("INSERT INTO profiles (name, group_id, sort, proxy, args, fingerprint, notes, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		p.Name, rawGroupID, p.Sort, rawProxy, p.Args, p.Fingerprint, p.Notes, p.CreatedAt, p.UpdatedAt)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "配置名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	rowID, _ := res.LastInsertId()
	p.ID = encodeID(rowID)
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func updateProfile(w http.ResponseWriter, r *http.Request) {
	var p Profile
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	p.UpdatedAt = time.Now().UnixMilli()

	enrichFingerprint(&p, false)

	rawID := decodeID(p.ID)
	rawGroupID := decodeID(p.GroupID)
	rawProxy := decodeID(p.Proxy)

	_, err := db.Exec("UPDATE profiles SET name=?, group_id=?, sort=?, proxy=?, args=?, fingerprint=?, notes=?, updated_at=? WHERE id=?",
		p.Name, rawGroupID, p.Sort, rawProxy, p.Args, p.Fingerprint, p.Notes, p.UpdatedAt, rawID)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "UNIQUE") {
			msg = "配置名称已存在！"
		}
		writeJSON(w, Response[any]{Code: 500, Message: msg})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success", Data: p})
}

func deleteProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	rawID := decodeID(req.ID)

	// Query fingerprint seed before deletion to locate user data directory
	var fp FingerprintConfig
	if err := db.QueryRow("SELECT fingerprint FROM profiles WHERE id=?", rawID).Scan(&fp); err != nil {
		writeJSON(w, Response[any]{Code: 404, Message: "profile not found"})
		return
	}

	if _, err := db.Exec("DELETE FROM profiles WHERE id=?", rawID); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}

	// Clean up profile user data directory from disk
	if fp.Seed != 0 {
		userDataDir := filepath.Join(dataDir, "profiles", encodeID(int64(fp.Seed)))
		if err := os.RemoveAll(userDataDir); err != nil {
			log.Printf("[Profile] failed to remove user data dir %s: %v", userDataDir, err)
		} else {
			log.Printf("[Profile] removed user data dir: %s", userDataDir)
		}
	}

	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

func showProfile(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	rp, ok := getRunning(id)

	if !ok {
		writeJSON(w, Response[any]{Code: 404, Message: "not running"})
		return
	}

	writeJSON(w, Response[any]{Code: 200, Message: "success"})

	if rp.cmd.Process != nil {
		pid := uint32(rp.cmd.Process.Pid)
		go func() {
			time.Sleep(200 * time.Millisecond)
			bringWindowToFront(pid)
		}()
	}
}

func launchProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}

	if _, _, err := startProfile(req.ID); err != nil {
		writeJSON(w, Response[any]{Code: 500, Message: err.Error()})
		return
	}
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

func stopProfile(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, Response[any]{Code: 400, Message: "invalid request body"})
		return
	}
	rp, ok := getRunning(req.ID)

	if !ok {
		writeJSON(w, Response[any]{Code: 404, Message: "not running"})
		return
	}

	stopRunning(rp)
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

// stopRunning 请求实例退出：Windows 走关窗口消息，其余平台发 SIGTERM，
// 5 秒内未退出则强杀。函数立即返回，不等待进程结束。
func stopRunning(rp *runningProfile) {
	if rp.cmd.Process == nil {
		return
	}
	if runtime.GOOS == "windows" {
		closeWindowsByPID(uint32(rp.cmd.Process.Pid))
	} else {
		rp.cmd.Process.Signal(syscall.SIGTERM)
	}
	go func() {
		timer := time.NewTimer(5 * time.Second)
		defer timer.Stop()
		select {
		case <-rp.done:
		case <-timer.C:
			rp.cmd.Process.Kill()
		}
	}()
}
