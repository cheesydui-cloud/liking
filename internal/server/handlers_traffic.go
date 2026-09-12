package server

import (
	"database/sql"
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
	return dayRangeAt(time.Now(), n)
}

func dayRangeAt(now time.Time, n int) (from, to string) {
	if n < 1 {
		n = 14
	}
	if n > 90 {
		n = 90
	}
	to = now.Format("2006-01-02")
	from = now.AddDate(0, 0, -(n - 1)).Format("2006-01-02")
	return from, to
}

func dayRangeDB(d *sql.DB, n int) (from, to string) {
	return dayRangeAt(db.ClockNow(d), n)
}

func (s *Server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	n := queryDays(r)
	from, to := dayRangeDB(s.DB, n)
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
	var billed, rawUsed int64
	for _, u := range all {
		if u != nil && u.Role != "admin" {
			billed += u.BilledBytes
			rawUsed += u.UsedUp + u.UsedDown
		}
	}
	var nicUp, nicDown int64
	srvs, _ := db.ListServers(s.DB)
	for _, x := range srvs {
		if up, down, ok := s.Hub.Live(x.ID); ok {
			nicUp += up
			nicDown += down
		}
	}
	now := db.ClockNow(s.DB)
	monthFrom := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location()).Format("2006-01-02")
	monthDays, _ := db.TrafficSeries(s.DB, monthFrom, to, 0, 0)
	var monthRaw int64
	for _, p := range monthDays {
		monthRaw += p.Up + p.Down
	}
	jsonOK(w, map[string]any{
		"from":         from,
		"to":           to,
		"days":         db.FillTrafficDays(from, to, days),
		"users":        users,
		"inbounds":     ins,
		"billed_bytes": billed,
		"raw_bytes":    rawUsed,
		"nic_up_bps":   nicUp,
		"nic_down_bps": nicDown,
		"month":        db.ClockMonth(s.DB),
		"month_bytes":  monthRaw,
		"timezone":     db.Timezone(s.DB),
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
	from, to := dayRangeDB(s.DB, n)
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
