package server

import (
	"net/http"
	"strings"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func (s *Server) handleListPackages(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListPackages(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.Package{}
	}
	jsonOK(w, map[string]any{"packages": list})
}

func (s *Server) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string    `json:"name"`
		TrafficBytes int64     `json:"traffic_bytes"`
		CycleDays    int       `json:"cycle_days"`
		ResetDay     int       `json:"reset_day"`
		Direction    string    `json:"direction"`
		InboundIDs   []int64   `json:"inbound_ids"`
		Multipliers  []float64 `json:"multipliers"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		jsonErr(w, http.StatusBadRequest, "需要套餐名")
		return
	}
	p, err := db.CreatePackage(s.DB, strings.TrimSpace(req.Name), req.TrafficBytes, req.CycleDays, req.ResetDay, req.Direction)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := db.SetPackageInbounds(s.DB, p.ID, req.InboundIDs, req.Multipliers); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, _ = db.GetPackage(s.DB, p.ID)
	jsonOK(w, map[string]any{"package": p})
}

func (s *Server) handleUpdatePackage(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	p, err := db.GetPackage(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "套餐不存在")
		return
	}
	var req struct {
		Name         string    `json:"name"`
		TrafficBytes *int64    `json:"traffic_bytes"`
		CycleDays    *int      `json:"cycle_days"`
		ResetDay     *int      `json:"reset_day"`
		Direction    string    `json:"direction"`
		InboundIDs   []int64   `json:"inbound_ids"`
		Multipliers  []float64 `json:"multipliers"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		p.Name = strings.TrimSpace(req.Name)
	}
	if req.TrafficBytes != nil {
		p.TrafficBytes = *req.TrafficBytes
	}
	if req.CycleDays != nil {
		p.CycleDays = *req.CycleDays
	}
	if req.ResetDay != nil {
		p.ResetDay = *req.ResetDay
	}
	if req.Direction != "" {
		p.Direction = req.Direction
	}
	if err := db.UpdatePackage(s.DB, p); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.InboundIDs != nil {
		if err := db.SetPackageInbounds(s.DB, p.ID, req.InboundIDs, req.Multipliers); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	p, _ = db.GetPackage(s.DB, p.ID)
	jsonOK(w, map[string]any{"package": p})
}

func (s *Server) handleDeletePackage(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := db.DeletePackage(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	jsonOK(w, map[string]any{"ok": true})
}
