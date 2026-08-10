package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeAgentJSON(t *testing.T) {
	tests := []struct {
		name string
		body string
		ok   bool
	}{
		{name: "valid", body: `{"id":"profile"}`, ok: true},
		{name: "trailing value", body: `{"id":"profile"} {}`, ok: false},
		{name: "too large", body: `{"id":"` + strings.Repeat("x", agentMaxRequestBody) + `"}`, ok: false},
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
