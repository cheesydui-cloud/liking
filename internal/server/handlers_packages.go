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
		if *req.SpeedLimit < 0 || *req.SpeedLimit > 10000 {
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

func (s *Server) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name         string    `json:"name"`
		TrafficBytes int64     `json:"traffic_bytes"`
		CycleDays    int       `json:"cycle_days"`
		ResetDay     int       `json:"reset_day"`
		Direction    string    `json:"direction"`
		InboundIDs   []int64   `json:"inbound_ids"`
		Multipliers  []float64 `json:"multipliers"`
		ServerIDs    []int64   `json:"server_ids"`
		packagePolicyReq
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		jsonErr(w, http.StatusBadRequest, "需要套餐名")
		return
	}
	if len(req.ServerIDs) == 0 && len(req.InboundIDs) == 0 {
		jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
		return
	}
	pol := &db.Package{}
	if err := applyPackagePolicyReq(pol, req.packagePolicyReq); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	p, err := db.CreatePackage(s.DB, strings.TrimSpace(req.Name), req.TrafficBytes, req.CycleDays, req.ResetDay, req.Direction)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	p.SpeedLimit = pol.SpeedLimit
	p.SubRulePreset = pol.SubRulePreset
	p.SubRuleCategories = pol.SubRuleCategories
	p.SiteFilterMode = pol.SiteFilterMode
	p.SiteDenyCategories = pol.SiteDenyCategories
	p.SiteDenyDomains = pol.SiteDenyDomains
	if err := db.UpdatePackage(s.DB, p); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.InboundIDs != nil {
		if err := db.SetPackageInbounds(s.DB, p.ID, req.InboundIDs, req.Multipliers); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := db.SetPackageServers(s.DB, p.ID, nil); err != nil {
			jsonErr(w, http.StatusBadRequest, "节点无效")
			return
		}
	} else {
		if err := db.SetPackageServers(s.DB, p.ID, req.ServerIDs); err != nil {
			jsonErr(w, http.StatusBadRequest, "节点无效")
			return
		}
		if err := db.SetPackageInbounds(s.DB, p.ID, nil, nil); err != nil {
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
	if err := applyPackagePolicyReq(p, req.packagePolicyReq); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.UpdatePackage(s.DB, p); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if req.InboundIDs != nil {
		if len(req.InboundIDs) == 0 {
			jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
			return
		}
		if err := db.SetPackageInbounds(s.DB, p.ID, req.InboundIDs, req.Multipliers); err != nil {
			jsonErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := db.SetPackageServers(s.DB, p.ID, nil); err != nil {
			jsonErr(w, http.StatusBadRequest, "节点无效")
			return
		}
	} else if req.ServerIDs != nil {
		if len(req.ServerIDs) == 0 {
			jsonErr(w, http.StatusBadRequest, "请选择实例或节点")
			return
		}
		if err := db.SetPackageServers(s.DB, p.ID, req.ServerIDs); err != nil {
			jsonErr(w, http.StatusBadRequest, "节点无效")
			return
		}
		if err := db.SetPackageInbounds(s.DB, p.ID, nil, nil); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
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
