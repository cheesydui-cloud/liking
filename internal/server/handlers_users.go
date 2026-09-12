package server

import (
	"crypto/rand"
	"net/http"
	"strings"
	"time"

	"liking/internal/db"
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

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListUsers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.User{}
	}
	jsonOK(w, map[string]any{"users": list})
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
	if req.Role != "admin" && req.Role != "user" {
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
	} else if exp > 0 {
		u.ExpiresAt = exp
		_ = db.UpdateUser(s.DB, u)
	}
	u, _ = db.GetUser(s.DB, u.ID)
	s.provisionAndSyncUser(u)
	admin := userFromCtx(r.Context())
	db.AddAudit(s.DB, &admin.ID, "user.create", u.Username)
	out := map[string]any{"user": u}
	if generated != "" {
		out["password"] = generated
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
	if err != nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	var req struct {
		Remark       string `json:"remark"`
		Enabled      *bool  `json:"enabled"`
		ExpiresAt    *int64 `json:"expires_at"`
		Days         *int   `json:"days"`
		PackageID    *int64 `json:"package_id"`
		Unbind       bool   `json:"unbind_package"`
		TrafficLimit *int64 `json:"traffic_limit"`
		ExtendDays   *int   `json:"extend_days"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	u.Remark = req.Remark
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
	u.TrafficLimit = req.TrafficLimit
	if err := db.UpdateUser(s.DB, u); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Unbind {
		_ = db.UnbindUserPackage(s.DB, u.ID)
	} else if req.PackageID != nil {
		if err := db.BindUserPackage(s.DB, u.ID, *req.PackageID, u.ExpiresAt); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if req.ExtendDays != nil || req.Days != nil || req.ExpiresAt != nil {
		_ = db.SetPackageExpiry(s.DB, u.ID, u.ExpiresAt)
	}
	u, _ = db.GetUser(s.DB, u.ID)
	s.provisionAndSyncUser(u)
	jsonOK(w, map[string]any{"user": u})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	u := userFromCtx(r.Context())
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
	u, _ := db.GetUser(s.DB, id)
	s.provisionAndSyncUser(u)
	jsonOK(w, map[string]any{"ok": true, "user": u})
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
	out := map[string]any{"ok": true}
	if generated != "" {
		out["password"] = generated
	}
	jsonOK(w, out)
}
