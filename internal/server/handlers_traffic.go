package server

import (
	"net/http"
	"strings"

	"liking/internal/corecfg"
	"liking/internal/db"
)

type trafficHopReq struct {
	ServerID   int64  `json:"server_id"`
	ListenPort int    `json:"listen_port"`
	DialHost   string `json:"dial_host"`
}

type trafficForwardReq struct {
	Name             string          `json:"name"`
	Enabled          *bool           `json:"enabled"`
	EntryServerID    int64           `json:"entry_server_id"`
	EntryPort        int             `json:"entry_port"`
	LandingInboundID int64           `json:"landing_inbound_id"`
	LandingDialHost  string          `json:"landing_dial_host"`
	Hops             []trafficHopReq `json:"hops"`
}

func trafficFromReq(req trafficForwardReq, prev *db.TrafficForward) *db.TrafficForward {
	fw := &db.TrafficForward{
		Name:             strings.TrimSpace(req.Name),
		Enabled:          true,
		EntryServerID:    req.EntryServerID,
		EntryPort:        req.EntryPort,
		LandingInboundID: req.LandingInboundID,
		LandingDialHost:  req.LandingDialHost,
		Hops:             make([]db.TrafficHop, 0, len(req.Hops)),
	}
	if prev != nil {
		fw.ID = prev.ID
		fw.Enabled = prev.Enabled
		fw.CreatedAt = prev.CreatedAt
	}
	if req.Enabled != nil {
		fw.Enabled = *req.Enabled
	}
	for _, h := range req.Hops {
		fw.Hops = append(fw.Hops, db.TrafficHop{
			ServerID:   h.ServerID,
			ListenPort: h.ListenPort,
			DialHost:   h.DialHost,
		})
	}
	return fw
}

func (s *Server) handleListTrafficForwards(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListTrafficForwards(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.TrafficForward{}
	}
	jsonOK(w, map[string]any{"forwards": list})
}

func (s *Server) handleCreateTrafficForward(w http.ResponseWriter, r *http.Request) {
	var req trafficForwardReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	fw := trafficFromReq(req, nil)
	if err := corecfg.PrepareTrafficForward(s.DB, fw); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.InsertTrafficForward(s.DB, fw); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fresh, _ := db.GetTrafficForward(s.DB, fw.ID)
	if fresh == nil {
		fresh = fw
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.AddAudit(s.DB, &u.ID, "forward.create", fresh.Name)
	}
	s.respondAfterApply(w, map[string]any{"forward": fresh}, db.ServerIDsForTrafficForward(fresh)...)
}

func (s *Server) handleUpdateTrafficForward(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	prev, err := db.GetTrafficForward(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "流量中转不存在")
		return
	}
	var req trafficForwardReq
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	fw := trafficFromReq(req, prev)
	if err := corecfg.PrepareTrafficForward(s.DB, fw); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.UpdateTrafficForward(s.DB, fw); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	fresh, _ := db.GetTrafficForward(s.DB, fw.ID)
	if fresh == nil {
		fresh = fw
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.AddAudit(s.DB, &u.ID, "forward.update", fresh.Name)
	}
	ids := append(db.ServerIDsForTrafficForward(prev), db.ServerIDsForTrafficForward(fresh)...)
	s.respondAfterApply(w, map[string]any{"forward": fresh}, ids...)
}

func (s *Server) handleDeleteTrafficForward(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	prev, err := db.GetTrafficForward(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "流量中转不存在")
		return
	}
	ids := db.ServerIDsForTrafficForward(prev)
	if err := db.DeleteTrafficForward(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if u := userFromCtx(r.Context()); u != nil {
		db.AddAudit(s.DB, &u.ID, "forward.delete", prev.Name)
	}
	s.respondAfterApply(w, map[string]any{"ok": true}, ids...)
}
