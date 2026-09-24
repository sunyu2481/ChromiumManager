package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDecodeAgentJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "valid", body: `{"name":"profile"}`, ok: true},
		{name: "trailing value", body: `{"name":"profile"} {}`, ok: false},
		{name: "too large", body: `{"name":"` + strings.Repeat("x", agentMaxRequestBody) + `"}`, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/agent/acquire", strings.NewReader(tt.body))
			res := httptest.NewRecorder()
			var dst acquireRequest
			if got := decodeAgentJSON(res, req, &dst); got != tt.ok {
				t.Fatalf("decodeAgentJSON() = %v, want %v", got, tt.ok)
			}
		})
	}
}

func TestBeginAgentOperationRejectsWhenFull(t *testing.T) {
	for i := 0; i < agentOperationLimit; i++ {
		agentOperationSlots <- struct{}{}
	}
	defer func() {
		for i := 0; i < agentOperationLimit; i++ {
			<-agentOperationSlots
		}
	}()

	res := httptest.NewRecorder()
	if release := beginAgentOperation(res); release != nil {
		release()
		t.Fatal("beginAgentOperation() accepted a request while full")
	}
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusTooManyRequests)
	}
}

// callAgent 以 JSON 请求体调用 agent handler，返回解码后的响应。
func callAgent(t *testing.T, handler http.HandlerFunc, body string) Response[json.RawMessage] {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	req.Host = "chromium-manager:10102"
	res := httptest.NewRecorder()
	handler(res, req)
	var resp Response[json.RawMessage]
	if err := json.Unmarshal(res.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response %q: %v", res.Body.String(), err)
	}
	return resp
}

func TestResolveProfileName(t *testing.T) {
	useTestDB(t)
	id := insertTestProfile(t, "HK-01", 0)

	got, _, err := resolveProfileName("HK-01")
	if err != nil || got != id {
		t.Fatalf("resolveProfileName(HK-01) = (%q, %v), want (%q, nil)", got, err, id)
	}
	// 名称区分大小写，内部 ID 也不能当名称用
	for _, name := range []string{"hk-01", id} {
		if _, code, err := resolveProfileName(name); code != 404 || err == nil {
			t.Errorf("resolveProfileName(%q) = %d, %v; want 404", name, code, err)
		}
	}
}

// 预先登记运行态，acquire 直接复用而不会真正拉起浏览器。
func TestAgentAcquireByName(t *testing.T) {
	useTestDB(t)
	rp := &runningProfile{cdpReady: make(chan struct{}), done: make(chan struct{})}
	close(rp.cdpReady)
	id := insertTestProfile(t, "账号 1", 0)
	putRunning(t, id, rp)

	resp := callAgent(t, agentAcquire, `{"name":"账号 1"}`)
	var got acquireResponse
	if err := json.Unmarshal(resp.Data, &got); err != nil || resp.Code != 200 {
		t.Fatalf("acquire = %d %s (err %v), want 200", resp.Code, resp.Message, err)
	}
	// 地址以转义后的名称寻址，且不含每次启动都会变化的 guid
	const base = "chromium-manager:10102/cdp/%E8%B4%A6%E5%8F%B7%201"
	want := acquireResponse{Name: "账号 1", CDPUrl: "http://" + base, WsUrl: "ws://" + base + "/devtools/browser"}
	if got != want {
		t.Fatalf("acquire data = %+v, want %+v", got, want)
	}

	// 旧调用方只传 id 时应明确报错，而不是静默选中某个实例
	if resp := callAgent(t, agentAcquire, `{"id":"`+id+`"}`); resp.Code != 400 {
		t.Fatalf("acquire by legacy id = %d %s, want 400", resp.Code, resp.Message)
	}
}

func TestAgentReleaseByName(t *testing.T) {
	useTestDB(t)
	id := insertTestProfile(t, "HK-01", 0)

	tests := []struct {
		name string
		body string
		code int
	}{
		{name: "missing name", body: `{"stop":true}`, code: 400},
		{name: "legacy id field", body: `{"id":"` + id + `","stop":true}`, code: 400},
		{name: "unknown name", body: `{"name":"HK-02"}`, code: 404},
		{name: "release only", body: `{"name":"HK-01"}`, code: 200},
		{name: "stop not running", body: `{"name":"HK-01","stop":true}`, code: 404},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if resp := callAgent(t, agentRelease, tt.body); resp.Code != tt.code {
				t.Fatalf("release %s = %d %s, want %d", tt.body, resp.Code, resp.Message, tt.code)
			}
		})
	}
}

func TestAgentReleaseStopsRunningProfile(t *testing.T) {
	useTestDB(t)
	// 用常驻的 sleep 进程充当浏览器，验证按名称 stop 会让它退出
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start helper process: %v", err)
	}
	done := make(chan struct{})
	go func() {
		cmd.Wait()
		close(done)
	}()
	putRunning(t, insertTestProfile(t, "HK-01", 0), &runningProfile{cmd: cmd, done: done})

	if resp := callAgent(t, agentRelease, `{"name":"HK-01","stop":true}`); resp.Code != 200 {
		t.Fatalf("release = %d %s, want 200", resp.Code, resp.Message)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("release with stop did not terminate the browser process")
	}
}

func TestAgentBrowsersListsNamesAndGroups(t *testing.T) {
	useTestDB(t)
	res, err := db.Exec("INSERT INTO groups (name, sort) VALUES ('电商', 0)")
	if err != nil {
		t.Fatal(err)
	}
	groupID, _ := res.LastInsertId()
	runningID := insertTestProfile(t, "HK-01", groupID)
	insertTestProfile(t, "US-01", 0)
	rp := &runningProfile{cdpReady: make(chan struct{})}
	close(rp.cdpReady)
	putRunning(t, runningID, rp)

	req := httptest.NewRequest(http.MethodGet, "/agent/browsers", nil)
	req.Host = "chromium-manager:10102"
	rec := httptest.NewRecorder()
	agentBrowsers(rec, req)

	// agent 面不应再暴露任何内部 ID
	if strings.Contains(rec.Body.String(), runningID) {
		t.Fatalf("response leaks internal id %s: %s", runningID, rec.Body.String())
	}
	var body Response[[]agentBrowserInfo]
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	got := map[string]agentBrowserInfo{}
	for _, b := range body.Data {
		got[b.Name] = b
	}
	want := map[string]agentBrowserInfo{
		"HK-01": {
			Name: "HK-01", Group: "电商", Running: true, CDPReady: true,
			CDPUrl: "http://chromium-manager:10102/cdp/HK-01",
			WsUrl:  "ws://chromium-manager:10102/cdp/HK-01/devtools/browser",
		},
		"US-01": {Name: "US-01"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("browsers = %+v, want %+v", got, want)
	}
}
