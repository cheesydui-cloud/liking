package db

import (
	"database/sql"
	"sync"
	"time"
)

const DefaultTimezone = "Asia/Shanghai"

var locCache sync.Map // string -> *time.Location

func Timezone(d *sql.DB) string {
	tz, _ := GetSetting(d, "timezone")
	tz = trimTZ(tz)
	if tz == "" {
		return DefaultTimezone
	}
	if _, err := loadLoc(tz); err != nil {
		return DefaultTimezone
	}
	return tz
}

func Location(d *sql.DB) *time.Location {
	loc, err := loadLoc(Timezone(d))
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}

func ClockNow(d *sql.DB) time.Time {
	return time.Now().In(Location(d))
}

func ClockDay(d *sql.DB) string {
	return ClockNow(d).Format("2006-01-02")
}

func ClockHour(d *sql.DB) string {
	return HourKey(ClockNow(d))
}

func hourFloor(t time.Time) time.Time {
	t = t.In(t.Location())
	return time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, t.Location())
}

func HourKey(t time.Time) string {
	return hourFloor(t).Format("2006-01-02T15")
}

func ClockMonth(d *sql.DB) string {
	return ClockNow(d).Format("2006-01")
}

func loadLoc(name string) (*time.Location, error) {
	if v, ok := locCache.Load(name); ok {
		return v.(*time.Location), nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, err
	}
	locCache.Store(name, loc)
	return loc, nil
}

func trimTZ(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			out = append(out, c)
		}
	}
	return string(out)
}

// MonthResetStart is midnight of the current billing cycle when reset is on
// calendar day `day` (1–31) in now's location. Day 31 in a short month clamps.
func MonthResetStart(now time.Time, day int) time.Time {
	if day < 1 || day > 31 {
		return time.Time{}
	}
	loc := now.Location()
	y, m, _ := now.Date()
	start := clampMonthDay(y, m, day, loc)
	if now.Before(start) {
		prev := time.Date(y, m, 1, 0, 0, 0, 0, loc).AddDate(0, -1, 0)
		start = clampMonthDay(prev.Year(), prev.Month(), day, loc)
	}
	return start
}

func clampMonthDay(y int, m time.Month, day int, loc *time.Location) time.Time {
	last := time.Date(y, m+1, 0, 0, 0, 0, 0, loc).Day()
	if day > last {
		day = last
	}
	return time.Date(y, m, day, 0, 0, 0, 0, loc)
}

// ResetDayOn is the calendar day in now's month that a 1–31 reset actually
// fires. Day 31 in February becomes the last day of that month.
func ResetDayOn(now time.Time, day int) int {
	if day < 1 || day > 31 {
		return 0
	}
	return clampMonthDay(now.Year(), now.Month(), day, now.Location()).Day()
}

func ServerCycleStartDay(s *Server, now time.Time) string {
	if s == nil || s.TrafficResetDay < 1 {
		return ""
	}
	t := MonthResetStart(now, s.TrafficResetDay)
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02")
}
