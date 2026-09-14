package server

import (
	"net/http"
	"strings"

	"liking/internal/db"
	"liking/internal/totp"
)

func (s *Server) handleTOTPBegin(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Password string `json:"password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	fresh, err := db.GetUser(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "登录已过期")
		return
	}
	if !checkPassword(fresh.PasswordHash, req.Password) {
		jsonErr(w, http.StatusBadRequest, "当前密码错误")
		return
	}
	if fresh.TOTPEnabled {
		jsonErr(w, http.StatusBadRequest, "已经开启两步验证")
		return
	}
	secret, err := totp.RandomSecret()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "内部错误")
		return
	}
	if err := db.SetUserTOTP(s.DB, fresh.ID, secret, false); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	issuer, _ := db.GetSetting(s.DB, "panel_name")
	if issuer == "" {
		issuer = "liking"
	}
	jsonOK(w, map[string]any{
		"secret": secret,
		"uri":    totp.URI(secret, issuer, fresh.Username),
	})
}

func (s *Server) handleTOTPEnable(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	fresh, err := db.GetUser(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "登录已过期")
		return
	}
	if strings.TrimSpace(fresh.TOTPSecret) == "" {
		jsonErr(w, http.StatusBadRequest, "请先开始绑定")
		return
	}
	if !totp.Verify(fresh.TOTPSecret, totp.ParseCode(req.Code)) {
		jsonErr(w, http.StatusBadRequest, "验证码不对")
		return
	}
	if err := db.SetUserTOTP(s.DB, fresh.ID, fresh.TOTPSecret, true); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.AddAudit(s.DB, &fresh.ID, "totp.enable", fresh.Username)
	fresh.TOTPEnabled = true
	jsonOK(w, map[string]any{"ok": true, "totp_enabled": true})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	fresh, err := db.GetUser(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusUnauthorized, "登录已过期")
		return
	}
	if !checkPassword(fresh.PasswordHash, req.Password) {
		jsonErr(w, http.StatusBadRequest, "当前密码错误")
		return
	}
	if fresh.TOTPEnabled && !totp.Verify(fresh.TOTPSecret, totp.ParseCode(req.Code)) {
		jsonErr(w, http.StatusBadRequest, "验证码不对")
		return
	}
	if err := db.ClearUserTOTP(s.DB, fresh.ID); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.AddAudit(s.DB, &fresh.ID, "totp.disable", fresh.Username)
	jsonOK(w, map[string]any{"ok": true, "totp_enabled": false})
}
