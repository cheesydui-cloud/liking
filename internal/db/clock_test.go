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
	feb := time.Date(2026, 2, 28, 12, 0, 0, 0, loc)
	if ResetDayOn(feb, 31) != 28 {
		t.Fatalf("feb clamp day %d", ResetDayOn(feb, 31))
	}
	if ResetDayOn(now, 15) != 15 {
		t.Fatal("same day")
	}
}

func TestAddMonths(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	jan12 := time.Date(2026, 1, 12, 15, 4, 5, 0, loc)
	got := AddMonths(jan12, 3)
	if got.Format("2006-01-02 15:04:05") != "2026-04-12 15:04:05" {
		t.Fatalf("jan12+3 %v", got)
	}
	jan31 := time.Date(2026, 1, 31, 8, 0, 0, 0, loc)
	got = AddMonths(jan31, 1)
	if got.Format("2006-01-02 15:04:05") != "2026-02-28 08:00:00" {
		t.Fatalf("jan31+1 %v", got)
	}
	leap := time.Date(2024, 1, 31, 0, 0, 0, 0, loc)
	got = AddMonths(leap, 1)
	if got.Format("2006-01-02") != "2024-02-29" {
		t.Fatalf("leap %v", got)
	}
	dec := time.Date(2026, 12, 19, 0, 0, 0, 0, loc)
	got = AddMonths(dec, 12)
	if got.Format("2006-01-02") != "2027-12-19" {
		t.Fatalf("year %v", got)
	}
	got = AddMonths(jan12, 0)
	if !got.Equal(jan12) {
		t.Fatalf("zero %v", got)
	}
}
