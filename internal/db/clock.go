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
