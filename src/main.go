package main

import (
	"context"
	"crypto/subtle"
	"database/sql/driver"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sqids/sqids-go"
	_ "modernc.org/sqlite"
)

//go:embed web/dist
var webFS embed.FS

var (
	sq      *sqids.Sqids
	dataDir = "data"

	// listenAddr 是管理面地址，承载 UI 与全部 CRUD 接口。
	// 默认只监听回环，保证管理接口不出容器。
	listenAddr = "127.0.0.1:10101"

	// agentAddr 是 agent 面地址，只承载 /agent/* 与 /cdp/* 路由。
	// 留空表示完全禁用：不启动第二个 server，也不给浏览器加远程调试参数。
	agentAddr string

	// agentToken 非空时，agent 面要求 Authorization: Bearer <token>。
	agentToken string

	// agentPublicHost 是拼接 CDP 地址时的兜底主机名，
	// 仅在请求未携带 Host 头时使用，默认取容器 hostname。
	agentPublicHost string
)

// agentEnabled 表示是否开启了 agent 面。
// 浏览器启动参数是否包含远程调试开关也由它决定。
func agentEnabled() bool {
	return agentAddr != ""
}

func init() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmsgprefix)

	if v := os.Getenv("DATA_DIR"); v != "" {
		dataDir = v
	}
	if v := os.Getenv("LISTEN_ADDR"); v != "" {
		listenAddr = v
	}
	agentAddr = os.Getenv("AGENT_ADDR")
	agentToken = os.Getenv("AGENT_TOKEN")
	agentPublicHost = os.Getenv("AGENT_PUBLIC_HOST")
	if agentPublicHost == "" {
		agentPublicHost, _ = os.Hostname()
	}

	var err error
	sq, err = sqids.New(sqids.Options{MinLength: 8})
	if err != nil {
		log.Fatalf("[App] failed to init sqids: %v", err)
	}
}

func encodeID(id int64) string {
	s, _ := sq.Encode([]uint64{uint64(id)})
	return s
}

func decodeID(s string) int64 {
	if s == "" || s == "all" {
		return 0
	}
	nums := sq.Decode(s)
	if len(nums) == 0 {
		return 0
	}
	return int64(nums[0])
}

type Group struct {
	ID        string `json:"_id"`
	Name      string `json:"name"`
	Sort      int    `json:"sort"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type Proxy struct {
	ID        string `json:"_id"`
	Name      string `json:"name"`
	URL       string `json:"url"`
	IP        string `json:"ip"`
	Lang      string `json:"lang"`
	Timezone  string `json:"timezone"`
	Location  string `json:"location"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type FingerprintConfig struct {
	Seed                int32    `json:"seed"`
	Platform            string   `json:"platform"`
	Brand               string   `json:"brand"`
	HardwareConcurrency string   `json:"hardwareConcurrency"`
	DeviceMemory        string   `json:"deviceMemory"`
	DisableFeatures     []string `json:"disableFeatures"`
	Screen              string   `json:"screen"`
	Lang                string   `json:"lang"`
	Timezone            string   `json:"timezone"`
	IP                  string   `json:"ip"`
	Location            string   `json:"location"`
	DisableFingerprint  []string `json:"disableFingerprint"`
	RandomFingerprint   bool     `json:"randomFingerprint"`
	ProxyLang           bool     `json:"proxyLang"`
	ProxyTimezone       bool     `json:"proxyTimezone"`
	ProxyLocation       bool     `json:"proxyLocation"`
}

func (f *FingerprintConfig) Scan(src interface{}) error {
	if src == nil {
		return nil
	}
	var data []byte
	switch v := src.(type) {
	case string:
		data = []byte(v)
	case []byte:
		data = v
	default:
		return fmt.Errorf("unsupported type for FingerprintConfig: %T", src)
	}
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, f)
}

func (f FingerprintConfig) Value() (driver.Value, error) {
	b, err := json.Marshal(f)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

type Profile struct {
	ID          string            `json:"_id"`
	Name        string            `json:"name"`
	GroupID     string            `json:"groupId"`
	Sort        int               `json:"sort"`
	Proxy       string            `json:"proxy"`
	Fingerprint FingerprintConfig `json:"fingerprint"`
	Args        string            `json:"args"`
	Notes       string            `json:"notes"`
	CreatedAt   int64             `json:"createdAt"`
	UpdatedAt   int64             `json:"updatedAt"`
}

type Response[T any] struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    T      `json:"data"`
}

// writeJSON writes a JSON-encoded response to the response writer.
func writeJSON(w http.ResponseWriter, resp any) {
	data, _ := json.Marshal(resp)
	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

// withMiddleware wraps the handler.
func withMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle preflight
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Panic recovery
		defer func() {
			if err := recover(); err != nil {
				log.Printf("[Server] panic recovered: %v", err)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		next.ServeHTTP(w, r)
	})
}

// withAgentAuth 在 agentToken 非空时校验 Bearer token。
// 用 subtle.ConstantTimeCompare 避免比较耗时泄漏 token 内容。
func withAgentAuth(next http.Handler) http.Handler {
	if agentToken == "" {
		return next
	}
	expected := []byte("Bearer " + agentToken)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := []byte(r.Header.Get("Authorization"))
		if subtle.ConstantTimeCompare(got, expected) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="agent"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func main() {
	initDB()
	defer db.Close()

	mux := http.NewServeMux()

	// Group routes
	mux.HandleFunc("GET /get_groups", getGroups)
	mux.HandleFunc("POST /add_group", addGroup)
	mux.HandleFunc("POST /update_group", updateGroup)
	mux.HandleFunc("POST /delete_group", deleteGroup)

	// Profile routes
	mux.HandleFunc("GET /get_profiles", getProfiles)
	mux.HandleFunc("GET /get_profile", getProfile)
	mux.HandleFunc("POST /add_profile", addProfile)
	mux.HandleFunc("POST /update_profile", updateProfile)
	mux.HandleFunc("POST /delete_profile", deleteProfile)
	mux.HandleFunc("POST /launch_profile", launchProfile)
	mux.HandleFunc("POST /stop_profile", stopProfile)
	mux.HandleFunc("GET /show_profile", showProfile)
	mux.HandleFunc("GET /export_cookies", exportCookies)
	mux.HandleFunc("POST /import_cookies", importCookies)

	// Proxy routes
	mux.HandleFunc("GET /get_proxies", getProxies)
	mux.HandleFunc("GET /get_proxy", getProxy)
	mux.HandleFunc("POST /add_proxy", addProxy)
	mux.HandleFunc("POST /update_proxy", updateProxy)
	mux.HandleFunc("POST /delete_proxy", deleteProxy)

	// Agent config (供前端展示 CDP 接入地址)
	mux.HandleFunc("GET /get_agent_config", getAgentConfig)

	// SSE
	mux.HandleFunc("GET /events", eventsHandler)

	// Static files
	fsSub, _ := fs.Sub(webFS, "web/dist")
	mux.Handle("/", http.FileServer(http.FS(fsSub)))

	servers := []*http.Server{
		{Addr: listenAddr, Handler: withMiddleware(mux)},
	}
	log.Printf("[Server] admin listening on %s", listenAddr)

	// agent 面单独一个 mux 与 server：只挂载 /agent/* 与 /cdp/*，
	// 管理接口与静态资源不在其上，即便对外监听也不会暴露 CRUD 能力。
	if agentEnabled() {
		agentMux := http.NewServeMux()
		agentMux.HandleFunc("GET /agent/browsers", agentBrowsers)
		agentMux.HandleFunc("POST /agent/acquire", agentAcquire)
		agentMux.HandleFunc("POST /agent/release", agentRelease)
		agentMux.HandleFunc("/cdp/{id}/{path...}", cdpProxyHandler)

		servers = append(servers, &http.Server{
			Addr:    agentAddr,
			Handler: withMiddleware(withAgentAuth(agentMux)),
			// CDP 会话是长连接，禁用写超时，交给客户端与浏览器自行断开
			ReadHeaderTimeout: 10 * time.Second,
		})
		authMode := "no auth"
		if agentToken != "" {
			authMode = "bearer token"
		}
		log.Printf("[Server] agent listening on %s (%s)", agentAddr, authMode)
	}

	for _, s := range servers {
		go func() {
			if err := s.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[Server] listen error on %s: %v", s.Addr, err)
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	<-quit
	log.Printf("[Server] shutdown signal received, shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, s := range servers {
		if err := s.Shutdown(ctx); err != nil {
			log.Printf("[Server] forced shutdown on %s: %v", s.Addr, err)
		}
	}
	log.Printf("[Server] exited gracefully")
}
