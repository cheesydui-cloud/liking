package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"liking/internal/certs"
	"liking/internal/db"
)

func publicCert(c *db.Certificate) *db.Certificate {
	if c == nil {
		return nil
	}
	out := *c
	out.KeyPEM = ""
	out.CertPEM = ""
	return &out
}

func (s *Server) handleListCerts(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListCerts(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.Certificate{}
	}
	jsonOK(w, map[string]any{"certs": list})
}

func (s *Server) handleGetCert(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	c, err := db.GetCert(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "证书不存在")
		return
	}
	jsonOK(w, map[string]any{"cert": publicCert(c)})
}

func (s *Server) handleCreateCert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		CertPEM string `json:"cert_pem"`
		KeyPEM  string `json:"key_pem"`
		Domains string `json:"domains"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	c, err := s.saveUploadedCert(strings.TrimSpace(req.Name), req.Domains, req.CertPEM, req.KeyPEM)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{"cert": publicCert(c)})
}

func (s *Server) saveUploadedCert(name, domains, certPEM, keyPEM string) (*db.Certificate, error) {
	if !strings.Contains(certPEM, "BEGIN") || !strings.Contains(keyPEM, "BEGIN") {
		return nil, errBad("请提供 PEM 证书和私钥")
	}
	key, err := certs.ParsePrivateKey(keyPEM)
	if err != nil {
		return nil, err
	}
	if err := certs.KeyMatchesCert(certPEM, key); err != nil {
		return nil, err
	}
	parsed, notAfter, err := certs.ParseMeta(certPEM)
	if err != nil {
		return nil, err
	}
	names := certs.SplitNames(domains)
	if len(names) == 0 {
		names = parsed
	}
	if name == "" && len(names) > 0 {
		name = names[0]
	}
	if name == "" {
		return nil, errBad("请提供名称")
	}
	var exp int64
	if !notAfter.IsZero() {
		exp = notAfter.Unix()
	}
	return db.CreateCert(s.DB, &db.Certificate{
		Name:      name,
		CertPEM:   certPEM,
		KeyPEM:    keyPEM,
		Domains:   certs.JoinNames(names),
		Source:    "upload",
		ExpiresAt: exp,
	})
}

type badReq struct{ msg string }

func (e badReq) Error() string { return e.msg }

func errBad(msg string) error { return badReq{msg: msg} }

func (s *Server) handleUpdateCert(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	c, err := db.GetCert(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "证书不存在")
		return
	}
	var req struct {
		Name    string `json:"name"`
		CertPEM string `json:"cert_pem"`
		KeyPEM  string `json:"key_pem"`
		Domains string `json:"domains"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		c.Name = strings.TrimSpace(req.Name)
	}
	if req.CertPEM != "" {
		c.CertPEM = req.CertPEM
	}
	if req.KeyPEM != "" {
		c.KeyPEM = req.KeyPEM
	}
	if req.Domains != "" || req.CertPEM != "" {
		c.Domains = req.Domains
	}
	if req.CertPEM != "" || req.KeyPEM != "" {
		key, err := certs.ParsePrivateKey(c.KeyPEM)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := certs.KeyMatchesCert(c.CertPEM, key); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		parsed, notAfter, err := certs.ParseMeta(c.CertPEM)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if strings.TrimSpace(c.Domains) == "" {
			c.Domains = certs.JoinNames(parsed)
		}
		if !notAfter.IsZero() {
			c.ExpiresAt = notAfter.Unix()
		}
		c.LastError = ""
	}
	if err := db.UpdateCert(s.DB, c); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	jsonOK(w, map[string]any{"cert": publicCert(c)})
}

func (s *Server) handleDeleteCert(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	n, _ := db.CountInboundsByCert(s.DB, id)
	if n > 0 {
		jsonErr(w, http.StatusConflict, "仍有入站使用此证书")
		return
	}
	if err := db.DeleteCert(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleSelfSignCert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Domains string `json:"domains"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	names := certs.SplitNames(req.Domains)
	name := strings.TrimSpace(req.Name)
	if name == "" && len(names) > 0 {
		name = names[0]
	}
	if name == "" {
		jsonErr(w, http.StatusBadRequest, "请提供名称或域名")
		return
	}
	certPEM, keyPEM, exp, err := certs.SelfSign(name, names)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	c, err := db.CreateCert(s.DB, &db.Certificate{
		Name:      name,
		CertPEM:   certPEM,
		KeyPEM:    keyPEM,
		Domains:   certs.JoinNames(names),
		Source:    "selfsigned",
		ExpiresAt: exp,
	})
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"cert": publicCert(c)})
}

func (s *Server) handleIssueACME(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Domains string `json:"domains"`
		Email   string `json:"email"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	names := certs.SplitNames(req.Domains)
	c, err := s.issueACME(r.Context(), 0, strings.TrimSpace(req.Name), names, strings.TrimSpace(req.Email))
	if err != nil {
		code := http.StatusBadRequest
		if _, ok := err.(badReq); !ok {
			code = http.StatusBadGateway
		}
		jsonErr(w, code, err.Error())
		return
	}
	s.syncAll()
	jsonOK(w, map[string]any{"cert": publicCert(c)})
}

func (s *Server) handleRenewCert(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	cur, err := db.GetCert(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "证书不存在")
		return
	}
	if cur.Source != "acme-cf" {
		jsonErr(w, http.StatusBadRequest, "只有 Let's Encrypt 证书可以续期")
		return
	}
	c, err := s.issueACME(r.Context(), id, cur.Name, certs.SplitNames(cur.Domains), cur.AcmeEmail)
	if err != nil {
		_ = db.SetCertError(s.DB, id, err.Error())
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	s.syncAll()
	jsonOK(w, map[string]any{"cert": publicCert(c)})
}

func (s *Server) issueACME(parent context.Context, replaceID int64, name string, names []string, email string) (*db.Certificate, error) {
	s.acmeMu.Lock()
	defer s.acmeMu.Unlock()

	token, _ := db.GetSetting(s.DB, "cf_api_token")
	if strings.TrimSpace(token) == "" {
		return nil, errBad("请先在设置里保存 Cloudflare API Token")
	}
	if email == "" {
		email, _ = db.GetSetting(s.DB, "acme_email")
	}
	email = strings.TrimSpace(email)
	if email == "" || !strings.Contains(email, "@") {
		return nil, errBad("请填写 ACME 邮箱")
	}
	domains, err := certs.NormalizeDNS(names)
	if err != nil {
		return nil, errBad(err.Error())
	}
	if name == "" {
		name = domains[0]
	}
	acctPEM, _ := db.GetSetting(s.DB, "acme_account_key")
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	res, err := certs.Issue(ctx, certs.IssueRequest{
		Domains:       domains,
		Email:         email,
		AccountKeyPEM: acctPEM,
		CFToken:       token,
	})
	if res != nil && res.AccountKeyPEM != "" {
		_ = db.SetSetting(s.DB, "acme_account_key", res.AccountKeyPEM)
	}
	if err != nil {
		return nil, err
	}
	row := &db.Certificate{
		Name:      name,
		CertPEM:   res.CertPEM,
		KeyPEM:    res.KeyPEM,
		Domains:   res.Domains,
		Source:    "acme-cf",
		ExpiresAt: res.ExpiresAt,
		AcmeEmail: email,
		AutoRenew: true,
	}
	if replaceID > 0 {
		cur, err := db.GetCert(s.DB, replaceID)
		if err != nil {
			return nil, err
		}
		row.ID = cur.ID
		if cur.Name != "" {
			row.Name = cur.Name
		}
		if err := db.UpdateCert(s.DB, row); err != nil {
			return nil, err
		}
		return db.GetCert(s.DB, row.ID)
	}
	return db.CreateCert(s.DB, row)
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	name, _ := db.GetSetting(s.DB, "panel_name")
	url, _ := db.GetSetting(s.DB, "panel_url")
	email, _ := db.GetSetting(s.DB, "acme_email")
	token, _ := db.GetSetting(s.DB, "cf_api_token")
	announce, _ := db.GetSetting(s.DB, "announce")
	cidrs, _ := db.GetSetting(s.DB, "admin_cidrs")
	tlsID, _ := db.GetSetting(s.DB, "panel_tls_cert_id")
	backupHour, _ := db.GetSetting(s.DB, "backup_hour")
	backupPass, _ := db.GetSetting(s.DB, "backup_password")
	jsonOK(w, map[string]any{
		"panel_name":          name,
		"panel_url":           url,
		"acme_email":          email,
		"cf_api_token_set":    strings.TrimSpace(token) != "",
		"announce":            announce,
		"admin_cidrs":         cidrs,
		"timezone":            db.Timezone(s.DB),
		"panel_tls_cert_id":   tlsID,
		"backup_hour":         backupHour,
		"backup_keep":         db.SettingInt(s.DB, "backup_keep", 7),
		"backup_password_set": strings.TrimSpace(backupPass) != "",
		"tls_active":          s.TLSActive,
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PanelName      *string `json:"panel_name"`
		PanelURL       *string `json:"panel_url"`
		AcmeEmail      *string `json:"acme_email"`
		CFToken        *string `json:"cf_api_token"`
		Announce       *string `json:"announce"`
		AdminCIDRs     *string `json:"admin_cidrs"`
		Timezone       *string `json:"timezone"`
		PanelTLSCertID *string `json:"panel_tls_cert_id"`
		BackupHour     *string `json:"backup_hour"`
		BackupKeep     *int    `json:"backup_keep"`
		BackupPassword *string `json:"backup_password"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if req.PanelName != nil {
		_ = db.SetSetting(s.DB, "panel_name", strings.TrimSpace(*req.PanelName))
	}
	if req.PanelURL != nil {
		_ = db.SetSetting(s.DB, "panel_url", strings.TrimSpace(*req.PanelURL))
	}
	if req.AcmeEmail != nil {
		_ = db.SetSetting(s.DB, "acme_email", strings.TrimSpace(*req.AcmeEmail))
	}
	if req.CFToken != nil && strings.TrimSpace(*req.CFToken) != "" {
		_ = db.SetSetting(s.DB, "cf_api_token", strings.TrimSpace(*req.CFToken))
	}
	if req.Announce != nil {
		_ = db.SetSetting(s.DB, "announce", strings.TrimSpace(*req.Announce))
	}
	if req.AdminCIDRs != nil {
		_ = db.SetSetting(s.DB, "admin_cidrs", strings.TrimSpace(*req.AdminCIDRs))
	}
	if req.Timezone != nil {
		tz := strings.TrimSpace(*req.Timezone)
		if tz == "" {
			tz = db.DefaultTimezone
		}
		if _, err := time.LoadLocation(tz); err != nil {
			jsonErr(w, http.StatusBadRequest, "时区无效")
			return
		}
		_ = db.SetSetting(s.DB, "timezone", tz)
	}
	if req.PanelTLSCertID != nil {
		_ = db.SetSetting(s.DB, "panel_tls_cert_id", strings.TrimSpace(*req.PanelTLSCertID))
	}
	if req.BackupHour != nil {
		h := strings.TrimSpace(*req.BackupHour)
		if h != "" && h != "off" && h != "-" {
			n, err := strconv.Atoi(h)
			if err != nil || n < 0 || n > 23 {
				jsonErr(w, http.StatusBadRequest, "备份小时无效")
				return
			}
		}
		_ = db.SetSetting(s.DB, "backup_hour", h)
	}
	if req.BackupKeep != nil {
		n := *req.BackupKeep
		if n < 1 {
			n = 1
		}
		if n > 30 {
			n = 30
		}
		_ = db.SetSetting(s.DB, "backup_keep", strconv.Itoa(n))
	}
	if req.BackupPassword != nil {
		_ = db.SetSetting(s.DB, "backup_password", *req.BackupPassword)
	}
	jsonOK(w, map[string]any{"ok": true, "tls_active": s.TLSActive})
}
