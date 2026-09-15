package server

import (
	"strings"
	"testing"
	"time"

	"liking/internal/db"
)

func TestDashboardAlertsQuotaAndExpiry(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now().Unix()
	items, _ := dashboardAlerts(d, []*db.Server{
		{Name: "hk", TrafficLimit: 1000, UsedUp: 850, UsedDown: 0},
		{Name: "jp", TrafficLimit: 100, UsedUp: 100, OverQuota: true},
		{Name: "us", ExpiresAt: now - 60},
		{Name: "sg", ExpiresAt: now + 3*86400},
		{Name: "de", Online: 0, LastSeen: now - 120},
		{Name: "new", Online: 0, LastSeen: 0},
	}, []*db.User{
		{Username: "alice", Role: "user", Enabled: true, QuotaRatio: 90},
		{Username: "bob", Role: "user", Enabled: true, QuotaRatio: 100},
		{Username: "carol", Role: "user", Enabled: true, ExpiresAt: now - 10},
		{Username: "dave", Role: "user", Enabled: true, ExpiresAt: now + 2*86400},
		{Username: "erin", Role: "user", Enabled: false, QuotaRatio: 100, ExpiresAt: now - 10},
		{Username: "admin", Role: "admin", Enabled: true, QuotaRatio: 100},
	})
	want := []struct {
		sub  string
		kind string
		to   string
	}{
		{"hk 流量已用 85%", "warn", "/servers"},
		{"jp 已达流量上限", "danger", "/servers"},
		{"us 已到期", "danger", "/servers"},
		{"sg 将于", "warn", "/servers"},
		{"de 离线", "warn", "/servers"},
		{"alice 流量已用 90%", "warn", "/users"},
		{"bob 流量已用尽", "danger", "/users"},
		{"carol 已到期", "danger", "/users"},
		{"dave 将于", "warn", "/users"},
	}
	for _, w := range want {
		if !alertHas(items, w.sub, w.kind, w.to) {
			t.Fatalf("missing %q kind=%s to=%s in %+v", w.sub, w.kind, w.to, textsOf(items))
		}
	}
	if alertHas(items, "new 离线", "", "") {
		t.Fatalf("unseen server should not alert offline: %v", textsOf(items))
	}
	if alertHas(items, "erin", "", "") {
		t.Fatalf("disabled user should not alert: %v", textsOf(items))
	}
	if alertHas(items, "admin", "", "") {
		t.Fatalf("admin should not alert: %v", textsOf(items))
	}
	if items[0].Kind != "danger" {
		t.Fatalf("danger should be first %+v", items[0])
	}
}

func TestDaysUntilAndSoonerExpiry(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 9, 15, 10, 0, 0, 0, loc)
	today := time.Date(2026, 9, 15, 23, 59, 59, 0, loc).Unix()
	if n := daysUntil(now, today); n != 0 {
		t.Fatalf("today %d", n)
	}
	in3 := time.Date(2026, 9, 18, 23, 59, 59, 0, loc).Unix()
	if n := daysUntil(now, in3); n != 3 {
		t.Fatalf("in3 %d", n)
	}
	if soonerExpiry(10, 0) != 10 || soonerExpiry(0, 8) != 8 || soonerExpiry(5, 9) != 5 {
		t.Fatalf("sooner")
	}
	if expireSoonText(0) != "今天到期" || !strings.Contains(expireSoonText(3), "3") {
		t.Fatalf("text")
	}
}

func TestSortAndCapAlertsDangerFirst(t *testing.T) {
	in := []dashAlert{
		{Text: "w1", Kind: "warn"},
		{Text: "d1", Kind: "danger"},
		{Text: "w2", Kind: "warn"},
	}
	out := sortAndCapAlerts(in)
	if len(out) != 3 || out[0].Text != "d1" || out[1].Text != "w1" {
		t.Fatalf("%+v", out)
	}
}

func alertHas(items []dashAlert, sub, kind, to string) bool {
	for _, it := range items {
		if !strings.Contains(it.Text, sub) {
			continue
		}
		if kind != "" && it.Kind != kind {
			continue
		}
		if to != "" && it.To != to {
			continue
		}
		return true
	}
	return false
}

func textsOf(items []dashAlert) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Text)
	}
	return out
}
