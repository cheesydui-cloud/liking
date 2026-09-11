package server

import (
	"encoding/json"
	"net/http"
	"strings"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func inboundJSON(in *db.Inbound) map[string]any {
	st := corecfg.ParseSettings(in.Settings)
	return map[string]any{
		"id":              in.ID,
		"server_id":       in.ServerID,
		"name":            in.Name,
		"profile":         in.Profile,
		"protocol":        in.Protocol,
		"network":         in.Network,
		"security":        in.Security,
		"core":            in.Core,
		"listen":          in.Listen,
		"port":            in.Port,
		"enabled":         in.Enabled,
		"settings":        st,
		"cert_id":         in.CertID,
		"line_kind":       in.LineKind,
		"exit_inbound_id": in.ExitInboundID,
		"exit_uri":        in.ExitURI,
		"created_at":      in.CreatedAt,
		"server_name":     in.ServerName,
		"server_host":     in.ServerHost,
		"server_online":   in.ServerOnline,
	}
}

type inboundReq struct {
	ServerID      int64           `json:"server_id"`
	Name          string          `json:"name"`
	Profile       string          `json:"profile"`
	Port          int             `json:"port"`
	Listen        string          `json:"listen"`
	Enabled       *bool           `json:"enabled"`
	Settings      json.RawMessage `json:"settings"`
	CertID        *int64          `json:"cert_id"`
	LineKind      string          `json:"line_kind"`
	ExitInboundID *int64          `json:"exit_inbound_id"`
	ExitURI       string          `json:"exit_uri"`
}

func (s *Server) handleListInbounds(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListInbounds(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]map[string]any, 0, len(list))
	for _, in := range list {
		out = append(out, inboundJSON(in))
	}
	jsonOK(w, map[string]any{"inbounds": out})
}

func (s *Server) handleCreateInbound(w http.ResponseWriter, r *http.Request) {
	var req inboundReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if req.ServerID == 0 || !corecfg.Known(req.Profile) {
		jsonErr(w, http.StatusBadRequest, "需要服务器和协议")
		return
	}
	in := &db.Inbound{
		ServerID:      req.ServerID,
		Name:          strings.TrimSpace(req.Name),
		Profile:       req.Profile,
		Port:          req.Port,
		Listen:        req.Listen,
		Enabled:       true,
		Settings:      "{}",
		CertID:        req.CertID,
		LineKind:      req.LineKind,
		ExitInboundID: req.ExitInboundID,
		ExitURI:       req.ExitURI,
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	if len(req.Settings) > 0 {
		in.Settings = string(req.Settings)
	}
	if err := s.prepareInbound(in); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := db.CreateInbound(s.DB, in)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncServers(created.ServerID)
	if created.ExitInboundID != nil {
		if land, err := db.GetInbound(s.DB, *created.ExitInboundID); err == nil {
			s.syncServers(land.ServerID)
		}
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "inbound.create", created.Name)
	jsonOK(w, map[string]any{"inbound": inboundJSON(created)})
}

func (s *Server) handleUpdateInbound(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	in, err := db.GetInbound(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "入站不存在")
		return
	}
	var req inboundReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		in.Name = strings.TrimSpace(req.Name)
	}
	if req.Port != 0 {
		in.Port = req.Port
	}
	if req.Listen != "" {
		in.Listen = req.Listen
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
	}
	if len(req.Settings) > 0 {
		in.Settings = string(req.Settings)
	}
	if req.CertID != nil {
		in.CertID = req.CertID
	}
	if req.LineKind != "" {
		in.LineKind = req.LineKind
	}
	if req.ExitInboundID != nil {
		in.ExitInboundID = req.ExitInboundID
	}
	in.ExitURI = req.ExitURI
	if err := s.prepareInbound(in); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.UpdateInbound(s.DB, in); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncServers(in.ServerID)
	if in.ExitInboundID != nil {
		if land, err := db.GetInbound(s.DB, *in.ExitInboundID); err == nil {
			s.syncServers(land.ServerID)
		}
	}
	fresh, _ := db.GetInbound(s.DB, in.ID)
	jsonOK(w, map[string]any{"inbound": inboundJSON(fresh)})
}

func (s *Server) handleDeleteInbound(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	in, err := db.GetInbound(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "入站不存在")
		return
	}
	n, _ := db.CountExitsTo(s.DB, id)
	if n > 0 {
		jsonErr(w, http.StatusConflict, "仍有链式线路指向此入站")
		return
	}
	sid := in.ServerID
	if err := db.DeleteInbound(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncServers(sid)
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) prepareInbound(in *db.Inbound) error {
	var exit *db.Inbound
	if in.ExitInboundID != nil && *in.ExitInboundID != 0 {
		e, err := db.GetInbound(s.DB, *in.ExitInboundID)
		if err != nil {
			return err
		}
		exit = e
	} else {
		in.ExitInboundID = nil
	}
	if err := corecfg.Normalize(in, exit); err != nil {
		return err
	}
	if corecfg.NeedTLS(in.Profile) && in.CertID == nil {
		return errNeedCert
	}
	used, err := db.UsedPortsOnServer(s.DB, in.ServerID, in.ID)
	if err != nil {
		return err
	}
	if _, ok := used[in.Port]; ok {
		return errPortTaken
	}
	return nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const (
	errNeedCert  simpleError = "该协议需要先上传 TLS 证书"
	errPortTaken simpleError = "该服务器上端口已被占用"
)
