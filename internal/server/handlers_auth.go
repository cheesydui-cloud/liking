package server

import (
	"net/http"
	"strings"
	"time"

	"liking/internal/db"
	"liking/internal/version"
)

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginLimiter.Allow(ip) {
		jsonErr(w, http.StatusTooManyRequests, "登录过于频繁，请稍后再试")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	u, err := db.GetUserByName(s.DB, req.Username)
	if err != nil || !checkPassword(u.PasswordHash, req.Password) {
		s.loginLimiter.Fail(ip)
		jsonErr(w, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	if !u.Enabled {
		jsonErr(w, http.StatusForbidden, "账号已被禁用")
		return
	}
	tok, err := db.RandomHex(24)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	exp := time.Now().Add(sessionTTL).Unix()
	if err := db.PutSession(s.DB, tok, u.ID, exp); err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	http.SetCookie(w, newSessionCookie(r, tok, int(sessionTTL.Seconds())))
	s.loginLimiter.Clear(ip)
	db.AddAudit(s.DB, &u.ID, "login", u.Username)
	jsonOK(w, s.sessionPayload(u, r))
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = db.DeleteSession(s.DB, c.Value)
	}
	http.SetCookie(w, newSessionCookie(r, "", -1))
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	jsonOK(w, s.sessionPayload(u, r))
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	var req struct {
		Old string `json:"old"`
		New string `json:"new"`
	}
	if err := decodeJSON(r, &req); err != nil || len(req.New) < 6 {
		jsonErr(w, http.StatusBadRequest, "新密码至少 6 位")
		return
	}
	if !checkPassword(u.PasswordHash, req.Old) {
		jsonErr(w, http.StatusBadRequest, "原密码错误")
		return
	}
	hash, err := HashPassword(req.New)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	if err := db.SetUserPassword(s.DB, u.ID, hash); err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) sessionPayload(u *db.User, r *http.Request) map[string]any {
	name, _ := db.GetSetting(s.DB, "panel_name")
	if name == "" {
		name = "liking"
	}
	out := map[string]any{
		"user":       u,
		"panel_name": name,
		"version":    version.Version,
	}
	if u != nil && u.Role != "admin" {
		base := panelURL(s.DB, r)
		out["sub"] = map[string]string{
			"auto":    base + "/api/sub/" + u.SubToken,
			"clash":   base + "/api/sub/" + u.SubToken + "/clash",
			"singbox": base + "/api/sub/" + u.SubToken + "/singbox",
			"uri":     base + "/api/sub/" + u.SubToken + "/uri",
		}
		if u.TrafficLimit != nil {
			u.TrafficCap = *u.TrafficLimit
		} else if u.PackageID != nil {
			if p, err := db.GetPackage(s.DB, *u.PackageID); err == nil {
				u.TrafficCap = p.TrafficBytes
			}
		}
	}
	return out
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	servers, _ := db.ListServers(s.DB)
	users, _ := db.ListUsers(s.DB)
	ins, _ := db.ListInbounds(s.DB)
	s.decorateServers(servers)
	online := 0
	for _, x := range servers {
		if x.Online == 1 {
			online++
		}
	}
	pkgs, _ := db.ListPackages(s.DB)
	var used int64
	members := 0
	for _, u := range users {
		used += u.UsedUp + u.UsedDown
		if u.Role != "admin" {
			members++
		}
	}
	jsonOK(w, map[string]any{
		"version":     version.Version,
		"servers":     len(servers),
		"online":      online,
		"users":       len(users),
		"members":     members,
		"inbounds":    len(ins),
		"packages":    len(pkgs),
		"used_bytes":  used,
		"server_list": servers,
	})
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"profiles": corecfgCatalog()})
}
