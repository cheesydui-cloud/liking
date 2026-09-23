package server

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/wsproto"
)

func randomPassword(n int) (string, error) {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	if n < 6 {
		n = 10
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(out), nil
}

// adminUserView exposes the stored login password to admin APIs for 名片 copy.
// Session /me never uses this wrapper.
func adminUserView(u *db.User) any {
	return adminUserViewPW(u, "")
}

type userLiveNode struct {
	ServerID    int64  `json:"server_id"`
	ServerName  string `json:"server_name"`
	InboundID   int64  `json:"inbound_id"`
	InboundName string `json:"inbound_name"`
	UpBps       int64  `json:"up_bps"`
	DownBps     int64  `json:"down_bps"`
	CanKick     bool   `json:"can_kick"`
}

func adminUserViewPW(u *db.User, loginPW string) any {
	return adminUserViewLive(u, loginPW, nil)
}

func adminUserViewLive(u *db.User, loginPW string, live []userLiveNode) any {
	if u == nil {
		return nil
	}
	type view struct {
		*db.User
		Password  string         `json:"password,omitempty"`
		LiveNodes []userLiveNode `json:"live_nodes,omitempty"`
	}
	pw := ""
	if u.Role != "admin" {
		pw = loginPW
	}
	return view{User: u, Password: pw, LiveNodes: live}
}

func (s *Server) userLiveNodes(uid int64) []userLiveNode {
	parts := s.Hub.UserLiveParts(uid)
	if len(parts) == 0 {
		return nil
	}
	out := make([]userLiveNode, 0, len(parts))
	for _, p := range parts {
		n := userLiveNode{
			ServerID:  p.ServerID,
			InboundID: p.InboundID,
			UpBps:     p.Up,
			DownBps:   p.Down,
			CanKick:   s.Hub.HasCap(p.ServerID, wsproto.CapKick),
		}
		if in, err := db.GetInbound(s.DB, p.InboundID); err == nil && in != nil {
			n.InboundName = in.Name
			n.ServerName = in.ServerName
		}
		if n.ServerName == "" {
			if srv, err := db.GetServer(s.DB, p.ServerID); err == nil && srv != nil {
				n.ServerName = srv.Name
			}
		}
		out = append(out, n)
	}
	return out
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListUsers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]any, 0, len(list))
	for _, u := range list {
		if up, down, ok := s.Hub.UserLive(u.ID); ok {
			u.NetUpBps = up
			u.NetDownBps = down
		}
		out = append(out, adminUserViewLive(u, "", s.userLiveNodes(u.ID)))
	}
	jsonOK(w, map[string]any{"users": out})
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username  string `json:"username"`
		Password  string `json:"password"`
		Remark    string `json:"remark"`
		Role      string `json:"role"`
		PackageID *int64 `json:"package_id"`
		ExpiresAt int64  `json:"expires_at"`
		Days      int    `json:"days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		jsonErr(w, http.StatusBadRequest, "用户名不能为空")
		return
	}
	generated := ""
	if strings.TrimSpace(req.Password) == "" {
		pw, err := randomPassword(10)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.Password = pw
		generated = pw
	} else if len(req.Password) < 6 {
		jsonErr(w, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	if req.Role == "" {
		req.Role = "user"
	}
	if req.Role == "admin" {
		jsonErr(w, http.StatusBadRequest, "不能再创建管理员")
		return
	}
	if req.Role != "user" {
		jsonErr(w, http.StatusBadRequest, "角色无效")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := db.CreateUser(s.DB, req.Username, hash, req.Role, req.Remark)
	if err != nil {
		jsonErr(w, http.StatusConflict, "用户名已存在")
		return
	}
	exp := req.ExpiresAt
	if req.Days > 0 {
		exp = time.Now().Add(time.Duration(req.Days) * 24 * time.Hour).Unix()
	}
	if req.PackageID != nil {
		if err := db.BindUserPackage(s.DB, u.ID, *req.PackageID, exp); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		u.ExpiresAt = exp
		if p, err := db.GetPackage(s.DB, *req.PackageID); err == nil {
			db.CopyPackagePolicy(u, p)
		}
		_ = db.UpdateUser(s.DB, u)
	} else if exp > 0 {
		u.ExpiresAt = exp
		_ = db.UpdateUser(s.DB, u)
	}
	if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
		u = fresh
	}
	s.provisionAndSyncUser(u)
	admin := userFromCtx(r.Context())
	if admin != nil {
		db.AddAudit(s.DB, &admin.ID, "user.create", u.Username)
	}
	pw := req.Password
	if generated != "" {
		pw = generated
	}
	out := map[string]any{"user": adminUserViewPW(u, pw)}
	if pw != "" {
		out["password"] = pw
	}
	jsonOK(w, out)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	oldPkgID := int64(0)
	hadPkg := u.PackageID != nil
	if hadPkg {
		oldPkgID = *u.PackageID
	}
	var req struct {
		Username           *string         `json:"username"`
		Remark             *string         `json:"remark"`
		Enabled            *bool           `json:"enabled"`
		ExpiresAt          *int64          `json:"expires_at"`
		Days               *int            `json:"days"`
		PackageID          *int64          `json:"package_id"`
		Unbind             bool            `json:"unbind_package"`
		TrafficLimit       json.RawMessage `json:"traffic_limit"`
		ExtendDays         *int            `json:"extend_days"`
		Password           *string         `json:"password"`
		TrafficResetDay    *int            `json:"traffic_reset_day"`
		SpeedLimit         *int64          `json:"speed_limit"`
		SubRulePreset      *string         `json:"sub_rule_preset"`
		SubRuleCategories  *[]string       `json:"sub_rule_categories"`
		SiteDenyCategories *[]string       `json:"site_deny_categories"`
		SiteDenyDomains    *[]string       `json:"site_deny_domains"`
		SiteFilterMode     *string         `json:"site_filter_mode"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if req.Username != nil {
		name, err := normalizeUsername(*req.Username)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if name != u.Username {
			if u.Role == "admin" {
				jsonErr(w, http.StatusBadRequest, "请在设置里修改管理员用户名")
				return
			}
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
		}
	}
	if req.Remark != nil {
		u.Remark = *req.Remark
	}
	if req.Enabled != nil {
		u.Enabled = *req.Enabled
	}
	if req.ExpiresAt != nil {
		u.ExpiresAt = *req.ExpiresAt
	}
	if req.Days != nil {
		if *req.Days <= 0 {
			u.ExpiresAt = 0
		} else {
			u.ExpiresAt = time.Now().Add(time.Duration(*req.Days) * 24 * time.Hour).Unix()
		}
	}
	if req.ExtendDays != nil && *req.ExtendDays > 0 {
		base := u.ExpiresAt
		now := time.Now().Unix()
		if base < now {
			base = now
		}
		u.ExpiresAt = base + int64(*req.ExtendDays)*24*3600
	}
	if len(req.TrafficLimit) > 0 && string(req.TrafficLimit) != "null" {
		var n int64
		if err := json.Unmarshal(req.TrafficLimit, &n); err != nil {
			jsonErr(w, http.StatusBadRequest, "流量上限无效")
			return
		}
		u.TrafficLimit = &n
	} else if string(req.TrafficLimit) == "null" {
		u.TrafficLimit = nil
	}
	if req.TrafficResetDay != nil {
		if *req.TrafficResetDay < 0 || *req.TrafficResetDay > 31 {
			jsonErr(w, http.StatusBadRequest, "重置日无效")
			return
		}
		u.TrafficResetDay = *req.TrafficResetDay
	}
	if req.SpeedLimit != nil {
		if !corecfg.ValidSpeedKBps(*req.SpeedLimit) {
			jsonErr(w, http.StatusBadRequest, "限速无效")
			return
		}
		u.SpeedLimit = *req.SpeedLimit
	}
	if req.SubRulePreset != nil {
		if !corecfg.ValidUserSubRulePreset(*req.SubRulePreset) {
			jsonErr(w, http.StatusBadRequest, "规则模式无效")
			return
		}
		u.SubRulePreset = corecfg.NormalizeUserSubRulePreset(*req.SubRulePreset)
		if u.SubRulePreset == "" {
			u.SubRuleCategories = []string{}
		}
	}
	if req.SubRuleCategories != nil {
		if u.SubRulePreset == "" {
			u.SubRuleCategories = []string{}
		} else {
			u.SubRuleCategories = corecfg.NormalizeCategoryNames(*req.SubRuleCategories)
		}
	}
	if req.SiteFilterMode != nil || req.SiteDenyCategories != nil || req.SiteDenyDomains != nil {
		mode := u.SiteFilterMode
		cats := u.SiteDenyCategories
		doms := u.SiteDenyDomains
		if req.SiteFilterMode != nil {
			mode = *req.SiteFilterMode
		}
		if req.SiteDenyCategories != nil {
			cats = *req.SiteDenyCategories
		}
		if req.SiteDenyDomains != nil {
			doms = *req.SiteDenyDomains
		}
		nmode, err := corecfg.NormalizeSiteFilterMode(mode)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		deny, err := corecfg.NormalizeSiteDeny(cats, doms)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if req.SiteFilterMode != nil && nmode == "" {
			u.SiteFilterMode = ""
			u.SiteDenyCategories = []string{}
			u.SiteDenyDomains = []string{}
		} else {
			if nmode == "" && !deny.Empty() {
				nmode = corecfg.SiteFilterDeny
			}
			if nmode == corecfg.SiteFilterAllow && deny.Empty() {
				jsonErr(w, http.StatusBadRequest, "只允许至少选一个网站")
				return
			}
			if nmode == corecfg.SiteFilterDeny && deny.Empty() {
				nmode = ""
			}
			if nmode == "" {
				deny = corecfg.SiteDeny{}
			}
			u.SiteFilterMode = nmode
			u.SiteDenyCategories = deny.Categories
			u.SiteDenyDomains = deny.Domains
		}
	}
	if err := db.UpdateUser(s.DB, u); err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			jsonErr(w, http.StatusConflict, "用户名已存在")
			return
		}
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Password != nil {
		pw := strings.TrimSpace(*req.Password)
		if pw != "" {
			if len(pw) < 6 {
				jsonErr(w, http.StatusBadRequest, "密码至少 6 位")
				return
			}
			hash, err := HashPassword(pw)
			if err != nil {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			if err := db.SetUserPassword(s.DB, u.ID, hash); err != nil {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
			_ = db.ClearUserPasswordPlain(s.DB, u.ID)
			_ = db.DeleteSessionsForUser(s.DB, u.ID)
		}
	}
	if req.Unbind {
		if err := db.UnbindUserPackage(s.DB, u.ID); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if req.PackageID != nil {
		same := hadPkg && oldPkgID == *req.PackageID
		if !same {
			if err := db.BindUserPackage(s.DB, u.ID, *req.PackageID, u.ExpiresAt); err != nil {
				jsonErr(w, http.StatusBadRequest, err.Error())
				return
			}
			if p, err := db.GetPackage(s.DB, *req.PackageID); err == nil {
				if req.SpeedLimit == nil {
					u.SpeedLimit = p.SpeedLimit
				}
				if req.SubRulePreset == nil && req.SubRuleCategories == nil {
					u.SubRulePreset = p.SubRulePreset
					u.SubRuleCategories = append([]string{}, p.SubRuleCategories...)
				}
				if req.SiteFilterMode == nil && req.SiteDenyCategories == nil && req.SiteDenyDomains == nil {
					u.SiteFilterMode = p.SiteFilterMode
					u.SiteDenyCategories = append([]string{}, p.SiteDenyCategories...)
					u.SiteDenyDomains = append([]string{}, p.SiteDenyDomains...)
				}
				_ = db.UpdateUser(s.DB, u)
			}
		} else if req.ExpiresAt != nil || req.Days != nil || req.ExtendDays != nil {
			if err := db.SetPackageExpiry(s.DB, u.ID, u.ExpiresAt); err != nil {
				jsonErr(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	} else if req.ExtendDays != nil || req.Days != nil || req.ExpiresAt != nil {
		if err := db.SetPackageExpiry(s.DB, u.ID, u.ExpiresAt); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
		u = fresh
	}
	s.provisionAndSyncUser(u)
	jsonOK(w, map[string]any{"user": adminUserView(u)})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if u.ID == id {
		jsonErr(w, http.StatusBadRequest, "不能删除当前登录账号")
		return
	}
	if err := db.DeleteUser(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleResetTraffic(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := db.ResetUserTraffic(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	s.provisionAndSyncUser(u)
	jsonOK(w, map[string]any{"ok": true, "user": adminUserView(u)})
}

func (s *Server) handleRotateSub(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	tok, err := db.RotateSubToken(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"sub_token": tok})
}

func (s *Server) handleRotateMeSub(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	tok, err := db.RotateSubToken(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fresh, err := db.GetUser(s.DB, u.ID)
	if err == nil && fresh != nil {
		u = fresh
	} else {
		u.SubToken = tok
	}
	db.AddAudit(s.DB, &u.ID, "user.rotate_sub", u.Username)
	jsonOK(w, map[string]any{"sub_token": tok, "user": u})
}

func (s *Server) handleBatchUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		IDs    []int64 `json:"ids"`
		Action string  `json:"action"`
		Days   int     `json:"days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if len(req.IDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "没有用户")
		return
	}
	if len(req.IDs) > 200 {
		jsonErr(w, http.StatusBadRequest, "一次最多 200 个")
		return
	}
	action := strings.TrimSpace(req.Action)
	switch action {
	case "extend", "reset_traffic", "enable", "disable":
	default:
		jsonErr(w, http.StatusBadRequest, "操作无效")
		return
	}
	if action == "extend" && req.Days <= 0 {
		jsonErr(w, http.StatusBadRequest, "天数无效")
		return
	}
	admin := userFromCtx(r.Context())
	type row struct {
		ID    int64  `json:"id"`
		Error string `json:"error,omitempty"`
		OK    bool   `json:"ok"`
	}
	out := make([]row, 0, len(req.IDs))
	okN := 0
	seen := map[int64]struct{}{}
	for _, id := range req.IDs {
		rec := row{ID: id}
		if id <= 0 {
			rec.Error = "无效 ID"
			out = append(out, rec)
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		u, err := db.GetUser(s.DB, id)
		if err != nil || u == nil {
			rec.Error = "用户不存在"
			out = append(out, rec)
			continue
		}
		if u.Role == "admin" {
			rec.Error = "不能批量操作管理员"
			out = append(out, rec)
			continue
		}
		switch action {
		case "extend":
			base := u.ExpiresAt
			now := time.Now().Unix()
			if base < now {
				base = now
			}
			u.ExpiresAt = base + int64(req.Days)*24*3600
			if err := db.UpdateUser(s.DB, u); err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
			_ = db.SetPackageExpiry(s.DB, u.ID, u.ExpiresAt)
		case "reset_traffic":
			if err := db.ResetUserTraffic(s.DB, u.ID); err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
		case "enable":
			u.Enabled = true
			if err := db.UpdateUser(s.DB, u); err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
		case "disable":
			u.Enabled = false
			if err := db.UpdateUser(s.DB, u); err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
		}
		if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
			u = fresh
		}
		s.provisionAndSyncUser(u)
		rec.OK = true
		okN++
		out = append(out, rec)
	}
	if admin != nil {
		db.AddAudit(s.DB, &admin.ID, "user.batch", action+" "+strconv.Itoa(okN)+"/"+strconv.Itoa(len(req.IDs)))
	}
	jsonOK(w, map[string]any{"ok": okN, "total": len(req.IDs), "users": out})
}

func (s *Server) handleSetUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	generated := ""
	req.Password = strings.TrimSpace(req.Password)
	if req.Password == "" {
		pw, err := randomPassword(10)
		if err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		req.Password = pw
		generated = pw
	} else if len(req.Password) < 6 {
		jsonErr(w, http.StatusBadRequest, "密码至少 6 位")
		return
	}
	hash, err := HashPassword(req.Password)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := db.SetUserPassword(s.DB, id, hash); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = db.ClearUserPasswordPlain(s.DB, id)
	_ = db.DeleteSessionsForUser(s.DB, id)
	out := map[string]any{"ok": true, "password": req.Password}
	if generated != "" {
		out["password"] = generated
	}
	jsonOK(w, out)
}

func (s *Server) handleForgetPassword(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	if u.Role == "admin" {
		jsonErr(w, http.StatusBadRequest, "管理员不保存明文密码")
		return
	}
	if err := db.ClearUserPasswordPlain(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}
