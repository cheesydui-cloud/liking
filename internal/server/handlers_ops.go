package server

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
		rememberLoginPassword(s.DB, u.ID, pw)
		exp := int64(0)
		if item.Days > 0 {
			exp = time.Now().Add(time.Duration(item.Days) * 24 * time.Hour).Unix()
		}
		if item.PackageID != nil {
			if err := db.BindUserPackage(s.DB, u.ID, *item.PackageID, exp); err != nil {
				rec.Error = err.Error()
				out = append(out, rec)
				continue
			}
		} else if exp > 0 {
			u.ExpiresAt = exp
			_ = db.UpdateUser(s.DB, u)
		}
		u, _ = db.GetUser(s.DB, u.ID)
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
	over := s.serverOverMap()
	type node struct {
		Name       string `json:"name"`
		Host       string `json:"host"`
		Port       int    `json:"port"`
		Profile    string `json:"profile"`
		ServerName string `json:"server_name"`
	}
	out := []node{}
	for _, id := range ids {
		in, err := db.GetInbound(s.DB, id)
		if err != nil || in == nil {
			continue
		}
		if !inboundLive(s.DB, in, over) {
			continue
		}
		out = append(out, node{
			Name:       in.Name,
			Host:       in.ServerHost,
			Port:       in.Port,
			Profile:    in.Profile,
			ServerName: in.ServerName,
		})
	}
	announce, _ := db.GetSetting(s.DB, "announce")
	jsonOK(w, map[string]any{"nodes": out, "announce": announce})
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
