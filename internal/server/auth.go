package server

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"liking/internal/db"
)

const (
	sessionCookie      = "liking_session"
	sessionTTL         = 12 * time.Hour
	sessionRememberTTL = 30 * 24 * time.Hour
)

type ctxKey int

const (
	userKey ctxKey = iota
	secureReqKey
)

func userFromCtx(ctx context.Context) *db.User {
	v, _ := ctx.Value(userKey).(*db.User)
	return v
}

func withUser(ctx context.Context, u *db.User) context.Context {
	return context.WithValue(ctx, userKey, u)
}

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

func checkPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func remoteIP(r *http.Request) net.IP {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host)
}

func isLoopbackRequest(r *http.Request) bool {
	ip := remoteIP(r)
	return ip != nil && ip.IsLoopback()
}

func isSecureRequest(r *http.Request) bool {
	if v, ok := r.Context().Value(secureReqKey).(bool); ok {
		return v
	}
	return detectSecureRequest(r)
}

func detectSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	if isLoopbackRequest(r) {
		if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
			return true
		}
	}
	return false
}

// trustedForwarded copies X-Real-IP / X-Forwarded-For into RemoteAddr only when
// the immediate peer is loopback (nginx on this machine). Public :8899 ignores spoofed headers.
// Secure-cookie / HSTS use the peer address from before this rewrite.
func trustedForwarded(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secure := detectSecureRequest(r)
		r = r.WithContext(context.WithValue(r.Context(), secureReqKey, secure))
		if isLoopbackRequest(r) {
			if ip := forwardedClientIP(r); ip != "" {
				_, port, err := net.SplitHostPort(r.RemoteAddr)
				if err != nil {
					port = "0"
				}
				r.RemoteAddr = net.JoinHostPort(ip, port)
			}
		}
		next.ServeHTTP(w, r)
	})
}

func forwardedClientIP(r *http.Request) string {
	if v := strings.TrimSpace(r.Header.Get("X-Real-IP")); v != "" {
		if ip := net.ParseIP(v); ip != nil {
			return ip.String()
		}
	}
	xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if xff == "" {
		return ""
	}
	parts := strings.Split(xff, ",")
	for i := len(parts) - 1; i >= 0; i-- {
		p := strings.TrimSpace(parts[i])
		if ip := net.ParseIP(p); ip != nil {
			return ip.String()
		}
	}
	return ""
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

func newSessionCookie(r *http.Request, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     sessionCookie,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecureRequest(r),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   maxAge,
	}
}

func (s *Server) requireAPIAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(sessionCookie)
		if err != nil || c.Value == "" {
			jsonErr(w, http.StatusUnauthorized, "未登录")
			return
		}
		u, err := db.GetSessionUser(s.DB, c.Value)
		if err != nil || u == nil {
			http.SetCookie(w, newSessionCookie(r, "", -1))
			jsonErr(w, http.StatusUnauthorized, "会话已过期")
			return
		}
		if !u.Enabled {
			jsonErr(w, http.StatusForbidden, "账号已被禁用")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

func (s *Server) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := userFromCtx(r.Context())
		if u == nil || u.Role != "admin" {
			jsonErr(w, http.StatusForbidden, "权限不足")
			return
		}
		if !s.adminIPAllowed(r) {
			jsonErr(w, http.StatusForbidden, "不在管理员 IP 白名单")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) adminIPAllowed(r *http.Request) bool {
	raw, _ := db.GetSetting(s.DB, "admin_cidrs")
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return true
	}
	ip := net.ParseIP(clientIP(r))
	if ip == nil {
		return false
	}
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == ' ' || r == ';'
	}) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "/") {
			_, n, err := net.ParseCIDR(part)
			if err == nil && n.Contains(ip) {
				return true
			}
			continue
		}
		if p := net.ParseIP(part); p != nil && p.Equal(ip) {
			return true
		}
	}
	return false
}

func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		site := strings.ToLower(r.Header.Get("Sec-Fetch-Site"))
		switch site {
		case "same-origin":
			next.ServeHTTP(w, r)
			return
		case "same-site", "cross-site", "none":
			jsonErr(w, http.StatusForbidden, "CSRF")
			return
		default:
			if origin := r.Header.Get("Origin"); origin != "" && !originOK(r, origin) {
				jsonErr(w, http.StatusForbidden, "CSRF")
				return
			}
			next.ServeHTTP(w, r)
		}
	})
}

func originOK(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	want := strings.ToLower(r.Host)
	got := strings.ToLower(u.Host)
	return want == got
}

func clientIP(r *http.Request) string {
	ip := remoteIP(r)
	if ip == nil {
		return r.RemoteAddr
	}
	return ip.String()
}

func currentSessionToken(r *http.Request) string {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return c.Value
}

func ResetAdminPassword(d *sql.DB, username, pw string) (string, error) {
	if strings.TrimSpace(pw) == "" {
		return "", fmt.Errorf("密码不能为空")
	}
	u, err := db.GetUserByName(d, username)
	if err != nil {
		return "", fmt.Errorf("找不到用户 %s", username)
	}
	if u.Role != "admin" {
		return "", fmt.Errorf("%s 不是管理员", username)
	}
	hash, err := HashPassword(pw)
	if err != nil {
		return "", err
	}
	if err := db.SetUserPassword(d, u.ID, hash); err != nil {
		return "", err
	}
	_ = db.ClearUserTOTP(d, u.ID)
	_ = db.DeleteSessionsForUser(d, u.ID)
	return "已重置 " + username + " 的密码，并关闭两步验证", nil
}
