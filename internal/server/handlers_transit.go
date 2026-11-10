package server

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"

	"liking/internal/corecfg"
	"liking/internal/db"
)

const maxUserForwards = 20

type myForwardReq struct {
	EntryInboundID int64  `json:"entry_inbound_id"`
	Kind           string `json:"kind"`
	Name           string `json:"name"`
	Port           int    `json:"port"`
	Enabled        *bool  `json:"enabled"`
	ExitURI        string `json:"exit_uri"`
	DestHost       string `json:"dest_host"`
	DestPort       int    `json:"dest_port"`
	Network        string `json:"network"`
}

func (s *Server) handleListMyForwards(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
		u = fresh
	}
	entries, err := db.TransitEntries(s.DB, u)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	owned, err := db.ListInboundsByOwner(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	byEntry := map[int64]*db.Inbound{}
	outEntries := make([]map[string]any, 0, len(entries))
	for _, in := range entries {
		if in == nil {
			continue
		}
		byEntry[in.ID] = in
		outEntries = append(outEntries, map[string]any{
			"id":          in.ID,
			"name":        in.Name,
			"profile":     in.Profile,
			"port":        in.Port,
			"server_id":   in.ServerID,
			"server_name": in.ServerName,
			"server_host": in.ServerHost,
		})
	}
	for _, in := range owned {
		if in == nil || in.EntryInboundID == 0 {
			continue
		}
		if _, ok := byEntry[in.EntryInboundID]; ok {
			continue
		}
		if e, err := db.GetInbound(s.DB, in.EntryInboundID); err == nil && e != nil {
			byEntry[e.ID] = e
		}
	}
	forwards := make([]map[string]any, 0, len(owned))
	for _, in := range owned {
		forwards = append(forwards, myForwardJSON(s.DB, u, in, byEntry))
	}
	jsonOK(w, map[string]any{
		"entries":  outEntries,
		"forwards": forwards,
		"max":      maxUserForwards,
	})
}

func (s *Server) handleCreateMyForward(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
		u = fresh
	}
	if u.Role == "admin" {
		jsonErr(w, http.StatusForbidden, "请在管理的中转里新建")
		return
	}
	var req myForwardReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	n, err := db.CountOwnedInbounds(s.DB, u.ID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if n >= maxUserForwards {
		jsonErr(w, http.StatusBadRequest, "中转最多 20 条")
		return
	}
	entry, err := s.transitEntry(u, req.EntryInboundID)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	in, err := s.forwardFromReq(nil, entry, u, req)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.prepareInbound(in); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	created, err := db.CreateInbound(s.DB, in)
	if err != nil {
		if strings.Contains(err.Error(), "端口已被占用") {
			jsonErr(w, http.StatusConflict, err.Error())
			return
		}
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionUser(s.DB, u); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.AddAudit(s.DB, &u.ID, "forward.create", created.Name)
	fresh, _ := db.GetInbound(s.DB, created.ID)
	if fresh == nil {
		fresh = created
	}
	s.respondAfterApply(w, map[string]any{"forward": myForwardJSON(s.DB, u, fresh, map[int64]*db.Inbound{entry.ID: entry})}, applyTargets(s, fresh)...)
}

func (s *Server) handleUpdateMyForward(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
		u = fresh
	}
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	in, err := db.GetInbound(s.DB, id)
	if err != nil || in == nil || in.OwnerUserID != u.ID {
		jsonErr(w, http.StatusNotFound, "中转不存在")
		return
	}
	var req myForwardReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if req.Enabled != nil && *req.Enabled && !db.UserTransitServer(s.DB, u, in.ServerID) {
		jsonErr(w, http.StatusForbidden, "这个入口已取消授权")
		return
	}
	entry, _ := db.GetInbound(s.DB, in.EntryInboundID)
	next, err := s.forwardFromReq(in, entry, u, req)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.prepareInbound(next); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.UpdateInbound(s.DB, next); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.Enabled != nil {
		hold := false
		if !*req.Enabled && in.AuthHold {
			hold = true
		}
		if err := db.SetInboundRunState(s.DB, next.ID, next.Enabled, hold); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := corecfg.ProvisionUser(s.DB, u); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fresh, _ := db.GetInbound(s.DB, next.ID)
	if fresh == nil {
		fresh = next
	}
	entries := map[int64]*db.Inbound{}
	if entry != nil {
		entries[entry.ID] = entry
	}
	s.respondAfterApply(w, map[string]any{"forward": myForwardJSON(s.DB, u, fresh, entries)}, applyTargets(s, fresh)...)
}

func (s *Server) handleDeleteMyForward(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	in, err := db.GetInbound(s.DB, id)
	if err != nil || in == nil || in.OwnerUserID != u.ID {
		jsonErr(w, http.StatusNotFound, "中转不存在")
		return
	}
	n, _ := db.CountExitsTo(s.DB, id)
	if list, err := db.ListInbounds(s.DB); err == nil {
		n += corecfg.CountHopsTo(list, id)
	}
	if n > 0 {
		jsonErr(w, http.StatusConflict, "仍有链式线路指向这条中转")
		return
	}
	sid := in.ServerID
	peers := chainPeerServerIDs(s, in)
	if err := db.DeleteInbound(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
		u = fresh
	}
	if _, err := corecfg.ProvisionUser(s.DB, u); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.respondAfterApply(w, map[string]any{"ok": true}, append([]int64{sid}, peers...)...)
}

func (s *Server) transitEntry(u *db.User, id int64) (*db.Inbound, error) {
	if id <= 0 {
		return nil, simpleError("请选择中转入口")
	}
	entries, err := db.TransitEntries(s.DB, u)
	if err != nil {
		return nil, err
	}
	for _, in := range entries {
		if in != nil && in.ID == id {
			return in, nil
		}
	}
	return nil, simpleError("这个节点没有授权给你当中转入口")
}

func (s *Server) forwardFromReq(cur, entry *db.Inbound, u *db.User, req myForwardReq) (*db.Inbound, error) {
	kind := req.Kind
	if cur != nil {
		if cur.Profile == corecfg.ProfilePortForward {
			kind = "port"
		} else {
			kind = "chain"
		}
	}
	if kind != "chain" && kind != "port" {
		return nil, simpleError("请选择链式或端口中转")
	}
	name := strings.TrimSpace(req.Name)
	if utf8.RuneCountInString(name) > 64 {
		return nil, simpleError("名称最多 64 个字")
	}
	if cur == nil {
		if entry == nil {
			return nil, simpleError("请选择中转入口")
		}
		if !db.UserTransitServer(s.DB, u, entry.ServerID) {
			return nil, simpleError("这个节点没有授权给你当中转入口")
		}
	} else if entry == nil {
		entry, _ = db.GetInbound(s.DB, cur.EntryInboundID)
	}
	in := &db.Inbound{}
	if cur != nil {
		*in = *cur
	}
	if name != "" {
		in.Name = name
	}
	if req.Port != 0 {
		in.Port = req.Port
	}
	if req.Enabled != nil {
		in.Enabled = *req.Enabled
		if *req.Enabled {
			in.AuthHold = false
		}
	} else if cur == nil {
		in.Enabled = true
	}
	if cur == nil {
		in.ServerID = entry.ServerID
		in.Listen = entry.Listen
		if strings.TrimSpace(in.Listen) == "" {
			in.Listen = "0.0.0.0"
		}
		in.OwnerUserID = u.ID
		in.EntryInboundID = entry.ID
		in.CertID = nil
		if kind == "chain" {
			in.Profile = entry.Profile
			in.CertID = entry.CertID
			in.LineKind = "chain"
			in.Settings = corecfg.CloneTransitSettings(entry.Settings)
		} else {
			in.Profile = corecfg.ProfilePortForward
			in.LineKind = "direct"
		}
		if in.Name == "" {
			label := entry.ServerName
			if label == "" {
				label = entry.Name
			}
			if kind == "port" {
				in.Name = strings.TrimSpace(label + " 端口")
			} else {
				in.Name = strings.TrimSpace(label + " 链式")
			}
		}
	}
	if kind == "port" {
		host := strings.TrimSpace(req.DestHost)
		if host == "" && cur != nil {
			host = corecfg.ParseSettings(cur.Settings).String("dest_host")
		}
		port := req.DestPort
		if port == 0 && cur != nil {
			port = corecfg.ParseSettings(cur.Settings).Int("dest_port", 0)
		}
		network := req.Network
		if network == "" && cur != nil {
			network = corecfg.ParseSettings(cur.Settings).String("network")
		}
		raw, err := json.Marshal(map[string]any{
			"dest_host": host,
			"dest_port": port,
			"network":   network,
		})
		if err != nil {
			return nil, err
		}
		in.Settings = string(raw)
		in.LineKind = "direct"
		in.ExitInboundID = nil
		in.ExitURI = ""
		in.Profile = corecfg.ProfilePortForward
		return in, nil
	}
	exitURI := strings.TrimSpace(req.ExitURI)
	if exitURI == "" && cur != nil {
		exitURI = cur.ExitURI
	}
	if exitURI == "" {
		return nil, simpleError("请填写落地")
	}
	st := corecfg.ParseSettings(in.Settings)
	delete(st, "hops")
	raw, err := st.Marshal()
	if err != nil {
		return nil, err
	}
	in.Settings = raw
	in.LineKind = "chain"
	in.ExitInboundID = nil
	in.ExitURI = exitURI
	return in, nil
}

func myForwardJSON(d *sql.DB, u *db.User, in *db.Inbound, entries map[int64]*db.Inbound) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	kind := "chain"
	if in.Profile == corecfg.ProfilePortForward {
		kind = "port"
	}
	st := corecfg.ParseSettings(in.Settings)
	m := map[string]any{
		"id":               in.ID,
		"name":             in.Name,
		"kind":             kind,
		"profile":          in.Profile,
		"port":             in.Port,
		"enabled":          in.Enabled,
		"auth_hold":        in.AuthHold,
		"server_id":        in.ServerID,
		"server_name":      in.ServerName,
		"entry_inbound_id": in.EntryInboundID,
		"exit_uri":         in.ExitURI,
		"dest_host":        st.String("dest_host"),
		"dest_port":        st.Int("dest_port", 0),
		"network":          st.String("network"),
	}
	if e := entries[in.EntryInboundID]; e != nil {
		m["entry_name"] = e.Name
		m["entry_profile"] = e.Profile
	}
	if kind == "chain" && u != nil && d != nil {
		if c, err := db.GetClient(d, in.ID, u.ID); err == nil && c != nil && c.Enabled {
			if uri, err := corecfg.ShareURI(in, c); err == nil {
				m["uri"] = uri
			}
		}
	}
	return m
}
