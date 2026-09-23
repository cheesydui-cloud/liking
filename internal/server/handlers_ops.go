package server

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/version"
)

func (s *Server) handleBulkUsers(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Users []struct {
			Username  string `json:"username"`
			Password  string `json:"password"`
			Remark    string `json:"remark"`
			PackageID *int64 `json:"package_id"`
			Days      int    `json:"days"`
		} `json:"users"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if len(req.Users) == 0 {
		jsonErr(w, http.StatusBadRequest, "没有用户")
		return
	}
	if len(req.Users) > 200 {
		jsonErr(w, http.StatusBadRequest, "一次最多 200 个")
		return
	}
	admin := userFromCtx(r.Context())
	type row struct {
		Username string `json:"username"`
		Password string `json:"password,omitempty"`
		Error    string `json:"error,omitempty"`
		OK       bool   `json:"ok"`
	}
	out := make([]row, 0, len(req.Users))
	okN := 0
	for _, item := range req.Users {
		name := strings.TrimSpace(item.Username)
		rec := row{Username: name}
		if name == "" {
			rec.Error = "用户名为空"
			out = append(out, rec)
			continue
		}
		pw := strings.TrimSpace(item.Password)
		if pw == "" {
			p, err := randomPassword(10)
			if err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
			pw = p
		} else if len(pw) < 6 {
			rec.Error = "密码至少 6 位"
			out = append(out, rec)
			continue
		}
		hash, err := HashPassword(pw)
		if err != nil {
			rec.Error = err.Error()
			out = append(out, rec)
			continue
		}
		u, err := db.CreateUser(s.DB, name, hash, "user", item.Remark)
		if err != nil {
			rec.Error = "用户名已存在"
			out = append(out, rec)
			continue
		}
		exp := int64(0)
		if item.Days > 0 {
			got, err := expiryFromDays(item.Days)
			if err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
			exp = got
		}
		if item.PackageID != nil {
			if err := db.BindUserPackage(s.DB, u.ID, *item.PackageID, exp); err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
			u.ExpiresAt = exp
			if p, err := db.GetPackage(s.DB, *item.PackageID); err == nil {
				db.CopyPackagePolicy(u, p)
			}
			_ = db.UpdateUser(s.DB, u)
		} else if exp > 0 {
			u.ExpiresAt = exp
			_ = db.UpdateUser(s.DB, u)
		}
		if fresh, err := db.GetUser(s.DB, u.ID); err == nil && fresh != nil {
			u = fresh
		}
		s.provisionAndSyncUser(u)
		rec.OK = true
		rec.Password = pw
		okN++
		out = append(out, rec)
	}
	if admin != nil {
		db.AddAudit(s.DB, &admin.ID, "user.bulk", fmt.Sprintf("%d/%d", okN, len(req.Users)))
	}
	jsonOK(w, map[string]any{"ok": okN, "total": len(req.Users), "users": out})
}

type facingNode struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Profile      string `json:"profile"`
	ServerName   string `json:"server_name"`
	ServerID     int64  `json:"server_id"`
	ServerOnline int    `json:"server_online"`
	LineKind     string `json:"line_kind"`
	UsedUp       int64  `json:"used_up"`
	UsedDown     int64  `json:"used_down"`
	PeriodUp     int64  `json:"period_up"`
	PeriodDown   int64  `json:"period_down"`
	URI          string `json:"uri,omitempty"`
	Starred      bool   `json:"starred,omitempty"`
}

type hiddenNode struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	ServerName string `json:"server_name"`
	Reason     string `json:"reason"`
}

func (s *Server) handleMeNodes(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	ids, err := db.InboundIDsForUser(s.DB, u)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	starred := []int64{}
	if u.Role == "admin" {
		starred = loadStarredIDs(s.DB, u.ID)
	}
	nodes, hidden := s.listFacingNodes(ids, u, true, starred)
	announce, _ := db.GetSetting(s.DB, "announce")
	jsonOK(w, map[string]any{
		"nodes":    nodes,
		"hidden":   hidden,
		"starred":  starred,
		"announce": announce,
	})
}

func (s *Server) handleMeStarred(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	if u.Role != "admin" {
		jsonErr(w, http.StatusForbidden, "没有权限")
		return
	}
	var req struct {
		InboundIDs []int64 `json:"inbound_ids"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	valid := map[int64]struct{}{}
	if list, err := db.ListInbounds(s.DB); err == nil {
		for _, in := range list {
			if in != nil && corecfg.UserFacing(in.Profile) {
				valid[in.ID] = struct{}{}
			}
		}
	}
	keep := make([]int64, 0, len(req.InboundIDs))
	seen := map[int64]struct{}{}
	for _, id := range req.InboundIDs {
		if _, ok := valid[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		keep = append(keep, id)
	}
	raw, err := json.Marshal(keep)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := db.SetSetting(s.DB, starredSettingKey(u.ID), string(raw)); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"starred": keep})
}

func (s *Server) handleUserNodes(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil || id <= 0 {
		jsonErr(w, http.StatusBadRequest, "无效用户")
		return
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	ids, err := db.InboundIDsForUser(s.DB, u)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodes, hidden := s.listFacingNodes(ids, nil, false, nil)
	jsonOK(w, map[string]any{"nodes": nodes, "hidden": hidden})
}

func (s *Server) handlePackageNodes(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil || id <= 0 {
		jsonErr(w, http.StatusBadRequest, "无效套餐")
		return
	}
	p, err := db.GetPackage(s.DB, id)
	if err != nil || p == nil {
		jsonErr(w, http.StatusNotFound, "套餐不存在")
		return
	}
	ids, err := db.PackageInboundIDs(s.DB, p)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	nodes, hidden := s.listFacingNodes(ids, nil, false, nil)
	jsonOK(w, map[string]any{"nodes": nodes, "hidden": hidden})
}

func (s *Server) listFacingNodes(ids []int64, viewer *db.User, withURI bool, starred []int64) ([]facingNode, []hiddenNode) {
	over := s.serverOverMap()
	out := []facingNode{}
	hidden := []hiddenNode{}
	starSet := map[int64]struct{}{}
	for _, id := range starred {
		starSet[id] = struct{}{}
	}
	var totals map[int64]db.TrafficSum
	period := map[int64]db.TrafficSum{}
	if viewer != nil {
		totals, _ = db.UserInboundTrafficTotals(s.DB, viewer.ID)
		from, to := dayRangeDB(s.DB, 14)
		if list, err := db.TrafficByInbound(s.DB, from, to, viewer.ID); err == nil {
			for _, t := range list {
				period[t.ID] = db.TrafficSum{Up: t.Up, Down: t.Down}
			}
		}
	}
	for _, id := range ids {
		in, err := db.GetInbound(s.DB, id)
		if err != nil || in == nil {
			continue
		}
		reason := inboundSkipReason(s.DB, in, over)
		if reason == "skip" {
			continue
		}
		if reason != "" {
			hidden = append(hidden, hiddenNode{
				ID:         in.ID,
				Name:       in.Name,
				ServerName: in.ServerName,
				Reason:     reason,
			})
			continue
		}
		kind := in.LineKind
		if kind == "" {
			kind = "direct"
		}
		n := facingNode{
			ID:           in.ID,
			Name:         in.Name,
			Host:         corecfg.ShareHost(in),
			Port:         in.Port,
			Profile:      in.Profile,
			ServerName:   in.ServerName,
			ServerID:     in.ServerID,
			ServerOnline: in.ServerOnline,
			LineKind:     kind,
		}
		if totals != nil {
			if t, ok := totals[in.ID]; ok {
				n.UsedUp = t.Up
				n.UsedDown = t.Down
			}
		}
		if t, ok := period[in.ID]; ok {
			n.PeriodUp = t.Up
			n.PeriodDown = t.Down
		}
		if _, ok := starSet[in.ID]; ok {
			n.Starred = true
		}
		if withURI && viewer != nil {
			if cl, err := db.GetClient(s.DB, in.ID, viewer.ID); err == nil && cl != nil && cl.Enabled {
				if uri, err := corecfg.ShareURI(in, cl); err == nil {
					n.URI = uri
				}
			}
		}
		out = append(out, n)
	}
	return out, hidden
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListSessionCounts(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"sessions": list})
}

func (s *Server) handleTrafficCSV(w http.ResponseWriter, r *http.Request) {
	n := queryDays(r)
	from, to := dayRangeDB(s.DB, n)
	users, err := db.TrafficByUser(s.DB, from, to)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="liking-traffic-`+from+`-`+to+`.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"user_id", "username", "up", "down", "raw"})
	for _, u := range users {
		_ = cw.Write([]string{
			strconv.FormatInt(u.ID, 10),
			u.Name,
			strconv.FormatInt(u.Up, 10),
			strconv.FormatInt(u.Down, 10),
			strconv.FormatInt(u.Up+u.Down, 10),
		})
	}
	cw.Flush()
}

func (s *Server) handleTrafficMonth(w http.ResponseWriter, r *http.Request) {
	now := db.ClockNow(s.DB)
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	to := now.Format("2006-01-02")
	days, err := db.TrafficSeries(s.DB, from, to, 0, 0)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	users, err := db.TrafficByUser(s.DB, from, to)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	var raw int64
	for _, p := range days {
		raw += p.Up + p.Down
	}
	jsonOK(w, map[string]any{
		"month":    db.ClockMonth(s.DB),
		"from":     from,
		"to":       to,
		"days":     db.FillTrafficDays(from, to, days),
		"users":    users,
		"raw":      raw,
		"version":  version.Version,
		"timezone": db.Timezone(s.DB),
	})
}
