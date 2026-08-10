package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

func newAuthTestHandler(auth *authManager) http.Handler {
	protected := http.NewServeMux()
	protected.HandleFunc("GET /", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	protected.HandleFunc("POST /auth/logout", auth.logoutHandler)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /login", auth.loginPageHandler)
	mux.HandleFunc("POST /auth/login", auth.loginHandler)
	mux.HandleFunc("/auth/check", auth.checkHandler)
	mux.Handle("/", auth.requireAuth(protected))
	return withAdminSecurityHeaders(mux)
}

func postLogin(t *testing.T, handler http.Handler, username, password string) *httptest.ResponseRecorder {
	return postLoginFrom(t, handler, username, password, "192.0.2.10:4321", "")
}

func postLoginFrom(t *testing.T, handler http.Handler, username, password, remoteAddr, realIP string) *httptest.ResponseRecorder {
	t.Helper()
	form := url.Values{"username": {username}, "password": {password}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.RemoteAddr = remoteAddr
	if realIP != "" {
		req.Header.Set("X-Real-IP", realIP)
	}
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	return res
}

func TestAuthRequiresLoginForPageAndAPI(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.Header.Set("Accept", "text/html")
	pageRes := httptest.NewRecorder()
	handler.ServeHTTP(pageRes, pageReq)
	if pageRes.Code != http.StatusSeeOther || pageRes.Header().Get("Location") != "/login" {
		t.Fatalf("page response = %d, location %q", pageRes.Code, pageRes.Header().Get("Location"))
	}

	apiReq := httptest.NewRequest(http.MethodGet, "/get_groups", nil)
	apiReq.Header.Set("Accept", "application/json")
	apiRes := httptest.NewRecorder()
	handler.ServeHTTP(apiRes, apiReq)
	if apiRes.Code != http.StatusUnauthorized {
		t.Fatalf("API response = %d, want %d", apiRes.Code, http.StatusUnauthorized)
	}
	body, _ := io.ReadAll(apiRes.Body)
	if !strings.Contains(string(body), `"code":401`) {
		t.Fatalf("unexpected API body: %s", body)
	}
}

func TestAuthLoginAndLogout(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))

	failed := postLogin(t, handler, "admin", "wrong-password")
	if failed.Code != http.StatusUnauthorized || len(failed.Result().Cookies()) != 0 {
		t.Fatalf("failed login response = %d, cookies = %d", failed.Code, len(failed.Result().Cookies()))
	}

	loggedIn := postLogin(t, handler, "admin", "correct-password")
	if loggedIn.Code != http.StatusSeeOther {
		t.Fatalf("login response = %d, want %d", loggedIn.Code, http.StatusSeeOther)
	}
	cookies := loggedIn.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	sessionCookie := cookies[0]
	if !sessionCookie.HttpOnly || sessionCookie.SameSite != http.SameSiteStrictMode || sessionCookie.Path != "/" {
		t.Fatalf("unexpected session cookie: %#v", sessionCookie)
	}

	pageReq := httptest.NewRequest(http.MethodGet, "/", nil)
	pageReq.AddCookie(sessionCookie)
	pageRes := httptest.NewRecorder()
	handler.ServeHTTP(pageRes, pageReq)
	if pageRes.Code != http.StatusOK {
		t.Fatalf("authenticated page response = %d", pageRes.Code)
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/auth/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	logoutRes := httptest.NewRecorder()
	handler.ServeHTTP(logoutRes, logoutReq)
	if logoutRes.Code != http.StatusOK {
		t.Fatalf("logout response = %d", logoutRes.Code)
	}

	retryReq := httptest.NewRequest(http.MethodGet, "/", nil)
	retryReq.AddCookie(sessionCookie)
	retryRes := httptest.NewRecorder()
	handler.ServeHTTP(retryRes, retryReq)
	if retryRes.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session response = %d", retryRes.Code)
	}
}

func TestAuthSecureCookieBehindHTTPSProxy(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))
	form := url.Values{"username": {"admin"}, "password": {"correct-password"}}
	req := httptest.NewRequest(http.MethodPost, "/auth/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Forwarded-Proto", "https")
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("expected secure session cookie, got %#v", cookies)
	}
}

func TestAuthRateLimitsFailedLogins(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))
	for i := 0; i < maxLoginAttempts; i++ {
		if res := postLogin(t, handler, "admin", "wrong-password"); res.Code != http.StatusUnauthorized {
			t.Fatalf("failed login %d response = %d", i+1, res.Code)
		}
	}
	if res := postLogin(t, handler, "admin", "correct-password"); res.Code != http.StatusTooManyRequests {
		t.Fatalf("rate-limited response = %d", res.Code)
	}
}

func TestAuthRateLimitIsAtomic(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))
	const requests = 20
	codes := make(chan int, requests)
	var wg sync.WaitGroup
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			codes <- postLogin(t, handler, "admin", "wrong-password").Code
		}()
	}
	wg.Wait()
	close(codes)

	unauthorized := 0
	rateLimited := 0
	for code := range codes {
		switch code {
		case http.StatusUnauthorized:
			unauthorized++
		case http.StatusTooManyRequests:
			rateLimited++
		default:
			t.Fatalf("unexpected response code: %d", code)
		}
	}
	if unauthorized != maxLoginAttempts || rateLimited != requests-maxLoginAttempts {
		t.Fatalf("unauthorized = %d, rate limited = %d", unauthorized, rateLimited)
	}
}

func TestAuthUsesGatewayClientAddressForRateLimit(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))
	for i := 0; i < maxLoginAttempts; i++ {
		res := postLoginFrom(t, handler, "admin", "wrong-password", "127.0.0.1:4321", "192.0.2.10")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("failed login %d response = %d", i+1, res.Code)
		}
	}
	res := postLoginFrom(t, handler, "admin", "correct-password", "127.0.0.1:4321", "192.0.2.11")
	if res.Code != http.StatusSeeOther {
		t.Fatalf("independent gateway client response = %d", res.Code)
	}
}

func TestAuthDoesNotTrustForwardedIPFromNonLoopback(t *testing.T) {
	handler := newAuthTestHandler(newAuthManager("admin", "correct-password"))
	for i := 0; i < maxLoginAttempts; i++ {
		res := postLoginFrom(t, handler, "admin", "wrong-password", "192.0.2.20:4321", "192.0.2.10")
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("failed login %d response = %d", i+1, res.Code)
		}
	}
	res := postLoginFrom(t, handler, "admin", "correct-password", "192.0.2.20:4321", "192.0.2.11")
	if res.Code != http.StatusTooManyRequests {
		t.Fatalf("spoofed forwarded client response = %d", res.Code)
	}
}

func TestAuthCheck(t *testing.T) {
	auth := newAuthManager("admin", "correct-password")
	handler := newAuthTestHandler(auth)

	unauthenticated := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticated, httptest.NewRequest(http.MethodGet, "/auth/check", nil))
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated check response = %d", unauthenticated.Code)
	}

	login := postLogin(t, handler, "admin", "correct-password")
	cookies := login.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies = %d, want 1", len(cookies))
	}
	checkReq := httptest.NewRequest(http.MethodGet, "/auth/check", nil)
	checkReq.AddCookie(cookies[0])
	authenticated := httptest.NewRecorder()
	handler.ServeHTTP(authenticated, checkReq)
	if authenticated.Code != http.StatusNoContent {
		t.Fatalf("authenticated check response = %d", authenticated.Code)
	}
}
