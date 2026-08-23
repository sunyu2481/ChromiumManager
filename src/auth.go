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
    /* 设计令牌与管理面（src/web/src/assets/css/style.scss）保持同一套值 */
    :root {
      color-scheme: light;
      --bg: #f5f6f8;
      --surface: #ffffff;
      --surface-2: #f4f5f7;
      --text: #1f2328;
      --text-2: #596170;
      --text-3: #8b929e;
      --accent: #2f6feb;
      --accent-soft: rgb(47 111 235 / 9%);
      --danger: #dc2626;
      --danger-soft: #fef2f2;
      --border: #e4e7eb;
      --border-strong: #cfd4db;
      --shadow: 0 12px 32px rgb(16 24 40 / 12%);
    }
    @media (prefers-color-scheme: dark) {
      :root {
        color-scheme: dark;
        --bg: #15171c;
        --surface: #1c1f25;
        --surface-2: #23272f;
        --text: #e7e9ee;
        --text-2: #a4acba;
        --text-3: #79818f;
        --accent: #5b8dff;
        --accent-soft: rgb(91 141 255 / 16%);
        --danger: #f87171;
        --danger-soft: rgb(248 113 113 / 12%);
        --border: #2c313a;
        --border-strong: #3b414c;
        --shadow: 0 12px 32px rgb(0 0 0 / 50%);
      }
    }
    * { box-sizing: border-box; }
    html, body { min-height: 100%; }
    body {
      margin: 0;
      min-height: 100vh;
      display: grid;
      place-items: center;
      padding: 24px;
      color: var(--text);
      background: var(--bg);
      font: 14px/1.5 -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC",
        "Microsoft YaHei", Roboto, Helvetica, Arial, sans-serif;
      -webkit-font-smoothing: antialiased;
    }

    /* 卡片：自带头部，与管理面顶栏同构（surface-2 底 + 下边框） */
    .card {
      width: min(100%, 360px);
      border: 1px solid var(--border);
      border-radius: 12px;
      background: var(--surface);
      box-shadow: var(--shadow);
      overflow: hidden;
    }
    .card-header {
      display: flex;
      align-items: center;
      gap: 10px;
      padding: 0 16px;
      height: 48px;
      border-bottom: 1px solid var(--border);
      background: var(--surface-2);
    }

    /* 品牌标记尺寸与管理面顶栏一致：26px / 11px */
    .brand-mark {
      flex: 0 0 auto;
      width: 26px; height: 26px; display: grid; place-items: center;
      border-radius: 6px; color: #fff; background: var(--accent);
      font-size: 11px; font-weight: 700; letter-spacing: .3px;
    }
    .brand-name { color: var(--text); font-size: 14px; font-weight: 600; }

    .card-body { padding: 22px 20px 20px; }
    h1 { margin: 0 0 2px; color: var(--text); font-size: 15px; font-weight: 600; }
    .subtitle { margin: 0 0 18px; color: var(--text-3); font-size: 12.5px; }

    .field { display: block; margin-bottom: 14px; }
    .field-label { display: block; margin-bottom: 6px; color: var(--text-2); font-weight: 500; font-size: 13px; }
    input {
      width: 100%; height: 34px; padding: 0 11px;
      border: 1px solid var(--border-strong); border-radius: 6px; outline: 0;
      color: var(--text); background: var(--surface); font: inherit; font-size: 13px;
      transition: border-color .18s ease, box-shadow .18s ease;
    }
    input:focus { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-soft); }

    .error {
      margin: 0 0 16px; padding: 9px 12px; border-radius: 6px;
      border-left: 3px solid var(--danger); color: var(--danger); background: var(--danger-soft);
      font-size: 13px;
    }

    button {
      width: 100%; height: 34px; margin-top: 6px; border: 0; border-radius: 6px;
      color: #fff; background: var(--accent); font: inherit; font-size: 13px; font-weight: 600;
      cursor: pointer; transition: filter .18s ease;
    }
    button:hover { filter: brightness(1.08); }
    button:focus-visible { outline: 2px solid var(--accent); outline-offset: 2px; }
  </style>
</head>
<body>
  <main class="card">
    <div class="card-header">
      <span class="brand-mark" aria-hidden="true">CM</span>
      <span class="brand-name">Chromium Manager</span>
    </div>
    <div class="card-body">
      <h1>登录</h1>
      <p class="subtitle">请输入管理控制台凭据</p>
      <form method="post" action="/auth/login">
        {{if .Error}}<p class="error" role="alert">{{.Error}}</p>{{end}}
        <label class="field">
          <span class="field-label">用户名</span>
          <input name="username" value="{{.Username}}" autocomplete="username" required autofocus>
        </label>
        <label class="field">
          <span class="field-label">密码</span>
          <input type="password" name="password" autocomplete="current-password" required>
        </label>
        <button type="submit">登录</button>
      </form>
    </div>
  </main>
</body>
</html>`))
