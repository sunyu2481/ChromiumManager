package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"html/template"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	adminSessionCookie = "chromium_manager_session"
	adminSessionTTL    = 12 * time.Hour
	loginAttemptWindow = 5 * time.Minute
	maxLoginAttempts   = 5
)

type loginAttempt struct {
	count     int
	expiresAt time.Time
}

type authManager struct {
	usernameHash [sha256.Size]byte
	passwordHash [sha256.Size]byte

	mu       sync.Mutex
	sessions map[[sha256.Size]byte]time.Time
	attempts map[string]loginAttempt
}

func newAuthManager(username, password string) *authManager {
	return &authManager{
		usernameHash: sha256.Sum256([]byte(username)),
		passwordHash: sha256.Sum256([]byte(password)),
		sessions:     make(map[[sha256.Size]byte]time.Time),
		attempts:     make(map[string]loginAttempt),
	}
}

func (a *authManager) credentialsValid(username, password string) bool {
	usernameHash := sha256.Sum256([]byte(username))
	passwordHash := sha256.Sum256([]byte(password))
	usernameOK := subtle.ConstantTimeCompare(usernameHash[:], a.usernameHash[:])
	passwordOK := subtle.ConstantTimeCompare(passwordHash[:], a.passwordHash[:])
	return usernameOK&passwordOK == 1
}

func (a *authManager) newSession() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	tokenHash := sha256.Sum256([]byte(token))

	now := time.Now()
	a.mu.Lock()
	for key, expiresAt := range a.sessions {
		if !expiresAt.After(now) {
			delete(a.sessions, key)
		}
	}
	a.sessions[tokenHash] = now.Add(adminSessionTTL)
	a.mu.Unlock()

	return token, nil
}

func (a *authManager) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || cookie.Value == "" {
		return false
	}

	tokenHash := sha256.Sum256([]byte(cookie.Value))
	now := time.Now()
	a.mu.Lock()
	expiresAt, ok := a.sessions[tokenHash]
	if ok && !expiresAt.After(now) {
		delete(a.sessions, tokenHash)
		ok = false
	}
	a.mu.Unlock()
	return ok
}

func (a *authManager) revokeSession(r *http.Request) {
	cookie, err := r.Cookie(adminSessionCookie)
	if err != nil || cookie.Value == "" {
		return
	}
	tokenHash := sha256.Sum256([]byte(cookie.Value))
	a.mu.Lock()
	delete(a.sessions, tokenHash)
	a.mu.Unlock()
}

func (a *authManager) consumeLoginAttempt(clientAddr string) bool {
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	attempt := a.attempts[clientAddr]
	if !attempt.expiresAt.After(now) {
		attempt = loginAttempt{expiresAt: now.Add(loginAttemptWindow)}
	}
	if attempt.count >= maxLoginAttempts {
		return false
	}
	attempt.count++
	a.attempts[clientAddr] = attempt
	return true
}

func (a *authManager) clearLoginFailures(clientAddr string) {
	a.mu.Lock()
	delete(a.attempts, clientAddr)
	a.mu.Unlock()
}

func requestPeerIP(remoteAddr string) net.IP {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return net.ParseIP(host)
	}
	return net.ParseIP(remoteAddr)
}

func requestClientAddress(r *http.Request) string {
	peerIP := requestPeerIP(r.RemoteAddr)
	if peerIP != nil {
		if peerIP.IsLoopback() {
			if forwardedIP := net.ParseIP(strings.TrimSpace(r.Header.Get("X-Real-IP"))); forwardedIP != nil {
				return forwardedIP.String()
			}
		}
		return peerIP.String()
	}
	return r.RemoteAddr
}

func requestIsHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     adminSessionCookie,
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   requestIsHTTPS(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func requireLoopback(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peerIP := requestPeerIP(r.RemoteAddr)
		if peerIP == nil || !peerIP.IsLoopback() {
			http.Error(w, "管理面仅允许容器内部访问", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *authManager) loginPageHandler(w http.ResponseWriter, r *http.Request) {
	if a.authenticated(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	renderLoginPage(w, http.StatusOK, "", "")
}

func (a *authManager) loginHandler(w http.ResponseWriter, r *http.Request) {
	clientAddr := requestClientAddress(r)
	if !a.consumeLoginAttempt(clientAddr) {
		renderLoginPage(w, http.StatusTooManyRequests, "登录尝试次数过多，请稍后再试", "")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	if err := r.ParseForm(); err != nil {
		renderLoginPage(w, http.StatusBadRequest, "提交内容无效", "")
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	if !a.credentialsValid(username, r.PostFormValue("password")) {
		renderLoginPage(w, http.StatusUnauthorized, "用户名或密码错误", username)
		return
	}

	token, err := a.newSession()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, Response[any]{Code: 500, Message: "无法创建登录会话"})
		return
	}
	a.clearLoginFailures(clientAddr)
	setSessionCookie(w, r, token)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (a *authManager) checkHandler(w http.ResponseWriter, r *http.Request) {
	if !a.authenticated(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (a *authManager) logoutHandler(w http.ResponseWriter, r *http.Request) {
	a.revokeSession(r)
	clearSessionCookie(w, r)
	writeJSON(w, Response[any]{Code: 200, Message: "success"})
}

func withAdminSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

type loginPageData struct {
	Error    string
	Username string
}

func renderLoginPage(w http.ResponseWriter, status int, message, username string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(status)
	_ = loginPageTemplate.Execute(w, loginPageData{Error: message, Username: username})
}

var loginPageTemplate = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>登录 - Chromium Manager</title>
  <style>
    * { box-sizing: border-box; }
    html, body { min-height: 100%; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
      color: #1f2937;
      background: #f3f4f6;
      font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
    }
    main { width: min(100%, 380px); }
    .brand { display: flex; align-items: center; gap: 12px; margin-bottom: 22px; }
    .brand-mark {
      width: 42px; height: 42px; display: grid; place-items: center;
      border-radius: 8px; color: #fff; background: #16a34a;
      font-size: 20px; font-weight: 700;
    }
    h1 { margin: 0; color: #111827; font-size: 24px; font-weight: 650; letter-spacing: 0; }
    .subtitle { margin: 2px 0 0; color: #6b7280; }
    form { padding: 28px; border: 1px solid #d1d5db; border-radius: 8px; background: #fff; box-shadow: 0 8px 24px rgba(17, 24, 39, .08); }
    label { display: block; margin-bottom: 16px; color: #374151; font-weight: 600; }
    input {
      width: 100%; height: 42px; margin-top: 7px; padding: 0 12px;
      border: 1px solid #cbd5e1; border-radius: 6px; outline: 0;
      color: #111827; background: #fff; font: inherit;
    }
    input:focus { border-color: #16a34a; box-shadow: 0 0 0 3px rgba(22, 163, 74, .14); }
    .error { margin: 0 0 16px; padding: 10px 12px; border-left: 3px solid #dc2626; color: #991b1b; background: #fef2f2; }
    button {
      width: 100%; height: 42px; border: 0; border-radius: 6px;
      color: #fff; background: #15803d; font: inherit; font-weight: 650; cursor: pointer;
    }
    button:hover { background: #166534; }
    button:focus-visible { outline: 3px solid rgba(22, 163, 74, .25); outline-offset: 2px; }
  </style>
</head>
<body>
  <main>
    <div class="brand">
      <div class="brand-mark" aria-hidden="true">CM</div>
      <div><h1>Chromium Manager</h1><p class="subtitle">登录管理控制台</p></div>
    </div>
    <form method="post" action="/auth/login">
      {{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
      <label>用户名<input name="username" value="{{.Username}}" autocomplete="username" required autofocus></label>
      <label>密码<input type="password" name="password" autocomplete="current-password" required></label>
      <button type="submit">登录</button>
    </form>
  </main>
</body>
</html>`))
