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
	sessionCookie = "liking_session"
	sessionTTL    = 12 * time.Hour
)

type ctxKey int

const userKey ctxKey = iota

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

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate()) {
		if proto := r.Header.Get("X-Forwarded-Proto"); strings.EqualFold(proto, "https") {
			return true
		}
	}
	return false
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
		site := r.Header.Get("Sec-Fetch-Site")
		switch strings.ToLower(site) {
		case "same-origin", "same-site", "":
			if site == "" {
				if origin := r.Header.Get("Origin"); origin != "" && !originOK(r, origin) {
					jsonErr(w, http.StatusForbidden, "CSRF")
					return
				}
			}
			next.ServeHTTP(w, r)
			return
		default:
			jsonErr(w, http.StatusForbidden, "CSRF")
			return
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
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
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
