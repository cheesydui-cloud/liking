package server

import (
	"net/http"
	"strings"

	"liking/internal/db"
)

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
	jsonOK(w, map[string]any{"cert": c})
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
	if strings.TrimSpace(req.Name) == "" || !strings.Contains(req.CertPEM, "BEGIN") || !strings.Contains(req.KeyPEM, "BEGIN") {
		jsonErr(w, http.StatusBadRequest, "请提供名称和 PEM 证书/私钥")
		return
	}
	c, err := db.CreateCert(s.DB, strings.TrimSpace(req.Name), req.CertPEM, req.KeyPEM, req.Domains)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.KeyPEM = ""
	jsonOK(w, map[string]any{"cert": c})
}

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
	c.Domains = req.Domains
	if err := db.UpdateCert(s.DB, c); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	c.KeyPEM = ""
	jsonOK(w, map[string]any{"cert": c})
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

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	name, _ := db.GetSetting(s.DB, "panel_name")
	url, _ := db.GetSetting(s.DB, "panel_url")
	jsonOK(w, map[string]any{
		"panel_name": name,
		"panel_url":  url,
	})
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		PanelName string `json:"panel_name"`
		PanelURL  string `json:"panel_url"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	_ = db.SetSetting(s.DB, "panel_name", strings.TrimSpace(req.PanelName))
	_ = db.SetSetting(s.DB, "panel_url", strings.TrimSpace(req.PanelURL))
	jsonOK(w, map[string]any{"ok": true})
}
