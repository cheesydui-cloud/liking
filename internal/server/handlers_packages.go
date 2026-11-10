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
	if err := db.ArrangeByDisplay(s.DB, db.CardPackages, list, func(p *db.Package) int64 { return p.ID }); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"packages": list})
}

type packagePolicyReq struct {
	SpeedLimit         *int64    `json:"speed_limit"`
	SubRulePreset      *string   `json:"sub_rule_preset"`
	SubRuleCategories  *[]string `json:"sub_rule_categories"`
	SiteDenyCategories *[]string `json:"site_deny_categories"`
	SiteDenyDomains    *[]string `json:"site_deny_domains"`
	SiteFilterMode     *string   `json:"site_filter_mode"`
}

func applyPackagePolicyReq(p *db.Package, req packagePolicyReq) error {
	if p == nil {
		return nil
	}
	if req.SpeedLimit != nil {
		if !corecfg.ValidSpeedKBps(*req.SpeedLimit) {
			return errSpeedLimit
		}
		p.SpeedLimit = *req.SpeedLimit
	}
	if req.SubRulePreset != nil {
		if !corecfg.ValidUserSubRulePreset(*req.SubRulePreset) {
			return errSubPreset
		}
		p.SubRulePreset = corecfg.NormalizeUserSubRulePreset(*req.SubRulePreset)
		if p.SubRulePreset == "" {
			p.SubRuleCategories = []string{}
		}
	}
	if req.SubRuleCategories != nil {
		if p.SubRulePreset == "" {
			p.SubRuleCategories = []string{}
		} else {
			p.SubRuleCategories = corecfg.NormalizeCategoryNames(*req.SubRuleCategories)
		}
	}
	if req.SiteFilterMode != nil || req.SiteDenyCategories != nil || req.SiteDenyDomains != nil {
		mode := p.SiteFilterMode
		cats := p.SiteDenyCategories
		doms := p.SiteDenyDomains
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
			return err
		}
		deny, err := corecfg.NormalizeSiteDeny(cats, doms)
		if err != nil {
			return err
		}
		if req.SiteFilterMode != nil && nmode == "" {
			p.SiteFilterMode = ""
			p.SiteDenyCategories = []string{}
			p.SiteDenyDomains = []string{}
		} else {
			if nmode == "" && !deny.Empty() {
				nmode = corecfg.SiteFilterDeny
			}
			if nmode == corecfg.SiteFilterAllow && deny.Empty() {
				return errAllowEmpty
			}
			if nmode == corecfg.SiteFilterDeny && deny.Empty() {
				nmode = ""
			}
			if nmode == "" {
				deny = corecfg.SiteDeny{}
			}
			p.SiteFilterMode = nmode
			p.SiteDenyCategories = deny.Categories
			p.SiteDenyDomains = deny.Domains
		}
	}
	return nil
}

var (
	errSpeedLimit = simpleError("限速无效")
	errSubPreset  = simpleError("规则模式无效")
	errAllowEmpty = simpleError("只允许至少选一个网站")
)

func hasPositiveID(ids []int64) bool {
	for _, id := range ids {
		if id > 0 {
			return true
		}
	}
	return false
}

func (s *Server) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string    `json:"name"`
		TrafficBytes int64     `json:"traffic_bytes"`
		CycleDays    int       `json:"cycle_days"`
		ResetDay     int       `json:"reset_day"`
		ResetMode    string    `json:"reset_mode"`
		Direction    string    `json:"direction"`
		InboundIDs   []int64   `json:"inbound_ids"`
		Multipliers  []float64 `json:"multipliers"`
		TransitIDs   []int64   `json:"transit_ids"`
		ServerIDs    []int64   `json:"server_ids"`
		packagePolicyReq
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.Name) == "" {
		jsonErr(w, http.StatusBadRequest, "需要套餐名")
		return
	}
	if req.TrafficBytes < 0 {
		jsonErr(w, http.StatusBadRequest, "流量无效")
		return
	}
	if req.CycleDays < 0 {
		jsonErr(w, http.StatusBadRequest, "周期天数无效")
		return
	}
	if req.ResetDay < 0 || req.ResetDay > 31 {
		jsonErr(w, http.StatusBadRequest, "重置日无效")
		return
	}
	resetMode, err := db.NormalizeResetMode(req.ResetMode)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Direction != "" && req.Direction != "oneway" && req.Direction != "twoway" {
		jsonErr(w, http.StatusBadRequest, "计费方向无效")
		return
	}
	if !hasPositiveID(req.ServerIDs) && !hasPositiveID(req.InboundIDs) {
		jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
		return
	}
	pol := &db.Package{}
	if err := applyPackagePolicyReq(pol, req.packagePolicyReq); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p := &db.Package{Name: strings.TrimSpace(req.Name), TrafficBytes: req.TrafficBytes, CycleDays: req.CycleDays, ResetDay: req.ResetDay, ResetMode: resetMode, Direction: req.Direction}
	p.SpeedLimit = pol.SpeedLimit
	p.SubRulePreset = pol.SubRulePreset
	p.SubRuleCategories = pol.SubRuleCategories
	p.SiteFilterMode = pol.SiteFilterMode
	p.SiteDenyCategories = pol.SiteDenyCategories
	p.SiteDenyDomains = pol.SiteDenyDomains
	relation := db.PackageRelationsServer
	ids := req.ServerIDs
	if req.InboundIDs != nil {
		if !hasPositiveID(req.InboundIDs) {
			jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
			return
		}
		relation = db.PackageRelationsInbound
		ids = req.InboundIDs
		if err := db.ValidatePackageNodes(s.DB, req.InboundIDs, req.TransitIDs); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	p, err = db.CreatePackageConfigured(s.DB, p, relation, ids, req.Multipliers, req.TransitIDs)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "节点或套餐参数无效："+err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
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
		ResetMode    *string   `json:"reset_mode"`
		Direction    string    `json:"direction"`
		InboundIDs   []int64   `json:"inbound_ids"`
		Multipliers  []float64 `json:"multipliers"`
		TransitIDs   []int64   `json:"transit_ids"`
		ServerIDs    []int64   `json:"server_ids"`
		packagePolicyReq
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		p.Name = strings.TrimSpace(req.Name)
	}
	if req.TrafficBytes != nil {
		if *req.TrafficBytes < 0 {
			jsonErr(w, http.StatusBadRequest, "流量无效")
			return
		}
		p.TrafficBytes = *req.TrafficBytes
	}
	if req.CycleDays != nil {
		if *req.CycleDays < 0 {
			jsonErr(w, http.StatusBadRequest, "周期天数无效")
			return
		}
		p.CycleDays = *req.CycleDays
	}
	if req.ResetDay != nil {
		if *req.ResetDay < 0 || *req.ResetDay > 31 {
			jsonErr(w, http.StatusBadRequest, "重置日无效")
			return
		}
		p.ResetDay = *req.ResetDay
	}
	if req.ResetMode != nil {
		mode, err := db.NormalizeResetMode(*req.ResetMode)
		if err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		p.ResetMode = mode
	}
	if req.Direction != "" {
		if req.Direction != "oneway" && req.Direction != "twoway" {
			jsonErr(w, http.StatusBadRequest, "计费方向无效")
			return
		}
		p.Direction = req.Direction
	}
	if err := applyPackagePolicyReq(p, req.packagePolicyReq); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	relation := db.PackageRelationsNone
	var ids []int64
	if req.InboundIDs != nil {
		if !hasPositiveID(req.InboundIDs) {
			jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
			return
		}
		relation = db.PackageRelationsInbound
		ids = req.InboundIDs
		if err := db.ValidatePackageNodes(s.DB, req.InboundIDs, req.TransitIDs); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
	} else if req.ServerIDs != nil {
		if !hasPositiveID(req.ServerIDs) {
			jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
			return
		}
		relation = db.PackageRelationsServer
		ids = req.ServerIDs
	}
	if err := db.UpdatePackageConfigured(s.DB, p, relation, ids, req.Multipliers, req.TransitIDs); err != nil {
		jsonErr(w, http.StatusBadRequest, "节点或套餐参数无效："+err.Error())
		return
	}
	if err := s.syncPackageForwards(id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	p, _ = db.GetPackage(s.DB, p.ID)
	jsonOK(w, map[string]any{"package": p})
}

func (s *Server) syncPackageForwards(packageID int64) error {
	userIDs, err := db.UserIDsByPackage(s.DB, packageID)
	if err != nil {
		return err
	}
	for _, uid := range userIDs {
		u, err := db.GetUser(s.DB, uid)
		if err != nil || u == nil {
			continue
		}
		if err := db.SyncUserForwards(s.DB, u); err != nil {
			return err
		}
	}
	return nil
}

func refuseIfInUse(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if db.IsInUse(err) {
		jsonErr(w, http.StatusConflict, err.Error())
		return true
	}
	jsonErr(w, http.StatusInternalServerError, err.Error())
	return true
}

func (s *Server) handleDeletePackage(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if refuseIfInUse(w, db.PackageInUse(s.DB, id)) {
		return
	}
	userIDs, err := db.UserIDsByPackage(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := db.DeletePackage(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	for _, uid := range userIDs {
		u, err := db.GetUser(s.DB, uid)
		if err != nil || u == nil {
			continue
		}
		if err := db.SyncUserForwards(s.DB, u); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if _, err := corecfg.ProvisionAll(s.DB); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncAll()
	jsonOK(w, map[string]any{"ok": true})
}
