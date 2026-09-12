package server

import (
	"net/http"
	"strconv"
	"time"

	"liking/internal/db"
)

func queryDays(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("days"))
	if n < 1 {
		n = 14
	}
	if n > 90 {
		n = 90
	}
	return n
}

func dayRange(n int) (from, to string) {
	if n < 1 {
		n = 14
	}
	if n > 90 {
		n = 90
	}
	now := time.Now()
	to = now.Format("2006-01-02")
	from = now.AddDate(0, 0, -(n - 1)).Format("2006-01-02")
	return from, to
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	n := queryDays(r)
	from, to := dayRange(n)
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
	ins, err := db.TrafficByInbound(s.DB, from, to, 0)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	all, _ := db.ListUsers(s.DB)
	var billed int64
	for _, u := range all {
		if u != nil && u.Role != "admin" {
			billed += u.BilledBytes
		}
	}
	jsonOK(w, map[string]any{
		"from":         from,
		"to":           to,
		"days":         db.FillTrafficDays(from, to, days),
		"users":        users,
		"inbounds":     ins,
		"billed_bytes": billed,
	})
}

func (s *Server) handleUserTraffic(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil || id <= 0 {
		jsonErr(w, http.StatusBadRequest, "无效用户")
		return
	}
	if _, err := db.GetUser(s.DB, id); err != nil {
		jsonErr(w, http.StatusNotFound, "用户不存在")
		return
	}
	s.writeUserTraffic(w, r, id)
}

func (s *Server) handleMeTraffic(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	s.writeUserTraffic(w, r, u.ID)
}

func (s *Server) writeUserTraffic(w http.ResponseWriter, r *http.Request, userID int64) {
	n := queryDays(r)
	from, to := dayRange(n)
	days, err := db.TrafficSeries(s.DB, from, to, userID, 0)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	ins, err := db.TrafficByInbound(s.DB, from, to, userID)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{
		"from":     from,
		"to":       to,
		"days":     db.FillTrafficDays(from, to, days),
		"inbounds": ins,
	})
}
