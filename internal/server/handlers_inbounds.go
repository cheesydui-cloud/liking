package server

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
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
		"user_facing":     corecfg.UserFacing(in.Profile),
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
	ExitURI       *string         `json:"exit_uri"`
}

func (s *Server) handleListInbounds(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListInbounds(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	totals, _ := db.InboundTrafficTotals(s.DB)
	out := make([]map[string]any, 0, len(list))
	for _, in := range list {
		m := inboundJSON(in)
		if t, ok := totals[in.ID]; ok {
			m["used_up"] = t.Up
			m["used_down"] = t.Down
		}
		out = append(out, m)
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
	}
	if req.ExitURI != nil {
		in.ExitURI = strings.TrimSpace(*req.ExitURI)
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
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "inbound.create", created.Name)
	s.respondAfterApply(w, map[string]any{"inbound": inboundJSON(created)}, created.ServerID, exitServerID(s, created))
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
	if req.ExitURI != nil {
		in.ExitURI = strings.TrimSpace(*req.ExitURI)
	}
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
	fresh, _ := db.GetInbound(s.DB, in.ID)
	s.respondAfterApply(w, map[string]any{"inbound": inboundJSON(fresh)}, in.ServerID, exitServerID(s, in))
}

func (s *Server) handleInboundShare(w http.ResponseWriter, r *http.Request) {
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
	if !corecfg.UserFacing(in.Profile) {
		jsonErr(w, http.StatusBadRequest, "端口中转没有分享链接")
		return
	}
	if corecfg.ShareHost(in) == "" {
		jsonErr(w, http.StatusBadRequest, "节点未填写公开地址，也还没有上报连接 IP。先点「改」填 IP 或域名。")
		return
	}
	clients, err := db.ListClientsByInbound(s.DB, in.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var picked *db.Client
	var pickedUser *db.User
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		u, err := db.GetUser(s.DB, c.UserID)
		if err != nil || u == nil || u.Role == "admin" {
			continue
		}
		var pkg *db.Package
		if u.PackageID != nil {
			pkg, _ = db.GetPackage(s.DB, *u.PackageID)
		}
		if !db.UserAccessOK(u, pkg) {
			continue
		}
		picked, pickedUser = c, u
		break
	}
	if picked == nil {
		jsonErr(w, http.StatusBadRequest, "这条线路还没有可用用户。先给用户绑定包含此节点的套餐，再到用户页复制订阅。")
		return
	}
	uri, err := corecfg.ShareURI(in, picked)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"uri":       uri,
		"username":  pickedUser.Username,
		"profile":   in.Profile,
		"sub_token": pickedUser.SubToken,
	})
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
	s.respondAfterApply(w, map[string]any{"ok": true}, sid)
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
	used, err := db.UsedPortsOnServer(s.DB, in.ServerID, in.ID)
	if err != nil {
		return err
	}
	if in.Port == 0 {
		p, err := corecfg.PickFreePort(used)
		if err != nil {
			return err
		}
		in.Port = p
	} else if in.Port < 1 || in.Port > 65535 {
		return errPortInvalid
	} else if _, ok := used[in.Port]; ok {
		return errPortTaken
	}
	if err := corecfg.Normalize(in, exit); err != nil {
		return err
	}
	if corecfg.NeedTLS(in.Profile) && in.CertID == nil {
		return errNeedCert
	}
	if corecfg.Reality(in.Profile) {
		srv, err := db.GetServer(s.DB, in.ServerID)
		if err != nil {
			return err
		}
		dest := corecfg.ParseSettings(in.Settings).String("dest")
		if corecfg.DestIsSelf(dest, srv.PublicHost, srv.ConnectIP) {
			return errRealitySelf
		}
	}
	return nil
}

type simpleError string

func (e simpleError) Error() string { return string(e) }

const (
	errNeedCert    simpleError = "该协议需要先上传 TLS 证书"
	errPortTaken   simpleError = "该服务器上端口已被占用"
	errPortInvalid simpleError = "端口范围 1–65535，或不填则随机"
	errRealitySelf simpleError = "REALITY dest 不能指向本机，否则无法伪装成真实网站"
)

func exitServerID(s *Server, in *db.Inbound) int64 {
	if in == nil || in.ExitInboundID == nil || *in.ExitInboundID == 0 {
		return 0
	}
	land, err := db.GetInbound(s.DB, *in.ExitInboundID)
	if err != nil {
		return 0
	}
	return land.ServerID
}

func (s *Server) respondAfterApply(w http.ResponseWriter, payload map[string]any, ids ...int64) {
	var applyErr error
	seen := map[int64]struct{}{}
	var uniq []int64
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
		if err := s.pushServer(id); err != nil && applyErr == nil {
			applyErr = err
		}
	}
	if applyErr != nil {
		payload["apply_error"] = applyErr.Error()
		ports := parseBusyPorts(applyErr.Error())
		if len(ports) > 0 {
			changed := 0
			for _, id := range uniq {
				n, err := db.DisableInboundsOnPorts(s.DB, id, ports)
				if err == nil {
					changed += n
				}
			}
			if changed > 0 {
				_, _ = corecfg.ProvisionAll(s.DB)
				for _, id := range uniq {
					_ = s.pushServer(id)
				}
				if raw, ok := payload["inbound"].(map[string]any); ok {
					if n, ok := asInt64(raw["id"]); ok {
						if fresh, err := db.GetInbound(s.DB, n); err == nil {
							payload["inbound"] = inboundJSON(fresh)
						}
					}
				}
			}
		}
	}
	jsonOK(w, payload)
}

func asInt64(v any) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case json.Number:
		n, err := t.Int64()
		return n, err == nil
	default:
		return 0, false
	}
}

var busyPortRe = regexp.MustCompile(`(?:端口|占用端口)[^\d]{0,12}(\d{1,5})`)

func parseBusyPorts(msg string) []int {
	seen := map[int]struct{}{}
	var out []int
	add := func(p int) {
		if p < 1 || p > 65535 {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if i := strings.Index(msg, "已跳过占用端口:"); i >= 0 {
		rest := msg[i+len("已跳过占用端口:"):]
		for _, part := range strings.FieldsFunc(rest, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '、' || r == '/'
		}) {
			p, err := strconv.Atoi(strings.TrimSpace(part))
			if err == nil {
				add(p)
			}
		}
	}
	for _, m := range busyPortRe.FindAllStringSubmatch(msg, -1) {
		p, _ := strconv.Atoi(m[1])
		add(p)
	}
	return out
}
