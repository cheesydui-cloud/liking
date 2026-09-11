package corecfg

import (
	"encoding/json"
	"testing"

	"liking/internal/db"
)

func TestBuildXrayAndMita(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "v1", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, in); err != nil {
		t.Fatal(err)
	}
	m := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 8964, Enabled: true, LineKind: "direct", Settings: `{"transport":"TCP"}`,
	}
	if err := Normalize(m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, m); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Xray) == 0 || len(b.Apply.Mita) == 0 {
		t.Fatalf("expected xray+mita, got xray=%d mita=%d singbox=%d", len(b.Apply.Xray), len(b.Apply.Mita), len(b.Apply.Singbox))
	}
	if b.Rev == "" {
		t.Fatal("rev")
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	if _, ok := xray["inbounds"]; !ok {
		t.Fatal("xray inbounds")
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	apiRule := ""
	for _, r := range rules {
		m, _ := r.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		for _, t := range tags {
			if t == "api" {
				apiRule, _ = m["outboundTag"].(string)
			}
		}
	}
	if apiRule != "api" {
		t.Fatalf("api inbound must route to api module, got %q", apiRule)
	}
	for _, o := range xray["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == "api-out" {
			t.Fatal("freedom api-out loops the stats API onto itself")
		}
	}
}
