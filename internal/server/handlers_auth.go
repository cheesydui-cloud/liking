package server

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"liking/internal/db"
	"liking/internal/totp"
	"liking/internal/version"
)

func normalizeUsername(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("用户名不能为空")
	}
	if strings.ContainsAny(s, " \t\n\r") {
		return "", fmt.Errorf("用户名不能含空格")
	}
	if utf8.RuneCountInString(s) > 32 {
		return "", fmt.Errorf("用户名最多 32 个字")
	}
	return s, nil
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if !s.loginLimiter.Allow(ip) {
		jsonErr(w, http.StatusTooManyRequests, "登录过于频繁，请稍后再试")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		TOTP     string `json:"totp"`
		Remember bool   `json:"remember"`
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
	if u.Role == "admin" && !s.adminIPAllowed(r) {
		jsonErr(w, http.StatusForbidden, "不在管理员 IP 白名单")
		return
	}
	if u.TOTPEnabled {
		code := totp.ParseCode(req.TOTP)
		if code == "" {
			jsonErrExtra(w, http.StatusUnauthorized, "需要两步验证码", map[string]any{"need_totp": true})
			return
		}
		if !totp.Verify(u.TOTPSecret, code) {
			s.loginLimiter.Fail(ip)
			jsonErrExtra(w, http.StatusUnauthorized, "两步验证码错误", map[string]any{"need_totp": true})
			return
		}
	}
	tok, err := db.RandomHex(24)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	ttl := sessionTTL
	if req.Remember {
		ttl = sessionRememberTTL
	}
	exp := time.Now().Add(ttl).Unix()
	if err := db.PutSession(s.DB, tok, u.ID, exp); err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	http.SetCookie(w, newSessionCookie(r, tok, int(ttl.Seconds())))
	s.loginLimiter.Clear(ip)
	if u.Role != "admin" {
		_ = db.ClearUserPasswordPlain(s.DB, u.ID)
	}
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
	_ = db.ClearUserPasswordPlain(s.DB, u.ID)
	_ = db.DeleteSessionsForUserExcept(s.DB, u.ID, currentSessionToken(r))
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	ctxUser := userFromCtx(r.Context())
	u, err := db.GetUser(s.DB, ctxUser.ID)
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "登录已过期")
		return
	}
	var req struct {
		Username    *string `json:"username"`
		OldPassword string  `json:"old_password"`
		NewPassword string  `json:"new_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.OldPassword) == "" {
		jsonErr(w, http.StatusBadRequest, "请填写当前密码")
		return
	}
	if !checkPassword(u.PasswordHash, req.OldPassword) {
		jsonErr(w, http.StatusBadRequest, "当前密码错误")
		return
	}
	changed := false
	if req.Username != nil {
		name, err := normalizeUsername(*req.Username)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if name != u.Username {
			other, err := db.GetUserByName(s.DB, name)
			if err == nil && other != nil && other.ID != u.ID {
				jsonErr(w, http.StatusConflict, "用户名已存在")
				return
			}
			if err != nil && err != sql.ErrNoRows {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			u.Username = name
			if err := db.UpdateUser(s.DB, u); err != nil {
				if strings.Contains(strings.ToLower(err.Error()), "unique") {
					jsonErr(w, http.StatusConflict, "用户名已存在")
					return
				}
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			changed = true
		}
	}
	if req.NewPassword != "" {
		if len(req.NewPassword) < 6 {
			jsonErr(w, http.StatusBadRequest, "新密码至少 6 位")
			return
		}
		hash, err := HashPassword(req.NewPassword)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, "内部错误")
			return
		}
		if err := db.SetUserPassword(s.DB, u.ID, hash); err != nil {
			jsonErr(w, http.StatusInternalServerError, "内部错误")
			return
		}
		_ = db.ClearUserPasswordPlain(s.DB, u.ID)
		_ = db.DeleteSessionsForUserExcept(s.DB, u.ID, currentSessionToken(r))
		changed = true
	}
	if !changed {
		jsonErr(w, http.StatusBadRequest, "没有要保存的更改")
		return
	}
	u, _ = db.GetUser(s.DB, u.ID)
	db.AddAudit(s.DB, &u.ID, "profile.update", u.Username)
	jsonOK(w, s.sessionPayload(u, r))
}

func (s *Server) sessionPayload(u *db.User, r *http.Request) map[string]any {
	name, _ := db.GetSetting(s.DB, "panel_name")
	if name == "" {
		name = "liking"
	}
	view := u
	if u != nil {
		c := *u
		c.PasswordPlain = ""
		view = &c
	}
	out := map[string]any{
		"user":       view,
		"panel_name": name,
		"version":    version.Version,
	}
	announce, _ := db.GetSetting(s.DB, "announce")
	out["announce"] = announce
	out["timezone"] = db.Timezone(s.DB)
	if view != nil && view.SubToken != "" {
		base := panelURL(s.DB, r)
		out["sub"] = map[string]string{
			"auto":    base + "/api/sub/" + view.SubToken,
			"clash":   base + "/api/sub/" + view.SubToken + "/clash",
			"singbox": base + "/api/sub/" + view.SubToken + "/singbox",
			"uri":     base + "/api/sub/" + view.SubToken + "/uri",
		}
		if view.Role != "admin" {
			if view.TrafficLimit != nil {
				view.TrafficCap = *view.TrafficLimit
			} else if view.PackageID != nil {
				if p, err := db.GetPackage(s.DB, *view.PackageID); err == nil {
					view.TrafficCap = p.TrafficBytes
				}
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
	var used, raw int64
	members := 0
	for _, u := range users {
		if u.Role == "admin" {
			continue
		}
		members++
		raw += u.UsedUp + u.UsedDown
		used += u.BilledBytes
	}
	from, to := dayRangeDB(s.DB, 14)
	days, _ := db.TrafficSeries(s.DB, from, to, 0, 0)
	days = db.FillTrafficDays(from, to, days)
	var today int64
	if n := len(days); n > 0 {
		today = days[n-1].Up + days[n-1].Down
	}
	alertItems, alerts := dashboardAlerts(s.DB, servers, users)
	announce, _ := db.GetSetting(s.DB, "announce")
	jsonOK(w, map[string]any{
		"version":     version.Version,
		"servers":     len(servers),
		"online":      online,
		"users":       len(users),
		"members":     members,
		"inbounds":    len(ins),
		"packages":    len(pkgs),
		"used_bytes":  used,
		"raw_bytes":   raw,
		"today_bytes": today,
		"days":        days,
		"server_list": servers,
		"alerts":      alerts,
		"alert_items": alertItems,
		"announce":    announce,
		"timezone":    db.Timezone(s.DB),
	})
}

type dashAlert struct {
	Text string `json:"text"`
	To   string `json:"to,omitempty"`
	Kind string `json:"kind,omitempty"`
}

func dashboardAlerts(d *sql.DB, servers []*db.Server, users []*db.User) ([]dashAlert, []string) {
	var items []dashAlert
	push := func(text, to, kind string) {
		items = append(items, dashAlert{Text: text, To: to, Kind: kind})
	}
	for _, x := range servers {
		if x.LastError != "" {
			push(x.Name+" 下发失败", "/servers", "danger")
		}
		if x.NeedsReinstall {
			push(x.Name+" Agent 太旧，请用安装命令重装", "/servers", "warn")
		} else if x.NeedsUpgrade {
			push(x.Name+" Agent 可升级到 "+version.Version, "/servers", "warn")
		}
		if x.OverQuota {
			push(x.Name+" 已达流量上限，节点已停用", "/servers", "danger")
		}
	}
	for _, u := range users {
		if u == nil || u.Role == "admin" {
			continue
		}
		if u.QuotaRatio >= 100 {
			push(u.Username+" 流量已用尽", "/users", "danger")
		} else if u.QuotaRatio >= 80 {
			push(u.Username+" 流量已用 "+strconv.Itoa(u.QuotaRatio)+"%", "/users", "warn")
		}
	}
	certs, _ := db.ListCerts(d)
	now := time.Now().Unix()
	for _, c := range certs {
		if c.ExpiresAt > 0 && c.ExpiresAt < now {
			push("证书 "+c.Name+" 已过期", "/settings?tab=certs", "danger")
		} else if c.ExpiresAt > 0 && c.ExpiresAt < now+30*86400 {
			push("证书 "+c.Name+" 即将到期", "/settings?tab=certs", "warn")
		}
	}
	if len(items) > 12 {
		items = items[:12]
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Text)
	}
	return items, out
}

func (s *Server) handleProfiles(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{"profiles": corecfgCatalog()})
}
