package db

import (
	"testing"
	"time"
)

func TestMonthResetStart(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, loc)
	start := MonthResetStart(now, 1)
	if start.Format("2006-01-02") != "2026-09-01" {
		t.Fatalf("day1 %v", start)
	}
	start = MonthResetStart(now, 20)
	if start.Format("2006-01-02") != "2026-08-20" {
		t.Fatalf("day20 %v", start)
	}
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, loc)
	start = MonthResetStart(mar1, 31)
	if start.Format("2006-01-02") != "2026-02-28" {
		t.Fatalf("feb clamp %v", start)
	}
	if !MonthResetStart(now, 0).IsZero() {
		t.Fatal("day 0")
	}
	s := &Server{ID: 1, TrafficResetDay: 1}
	if ServerCycleStartDay(s, now) != "2026-09-01" {
		t.Fatalf("cycle %s", ServerCycleStartDay(s, now))
	}
	if ServerCycleStartDay(&Server{ID: 2}, now) != "" {
		t.Fatal("no reset day")
	}
}
