package corecfg

import (
	"encoding/json"
	"testing"

	"liking/internal/db"
)

func TestSpeedMark(t *testing.T) {
	if SpeedMark(0) != 0 || SpeedMark(-1) != 0 {
		t.Fatal("zero")
	}
	if SpeedMark(1) != SpeedMarkBase|1 {
		t.Fatalf("got %x", SpeedMark(1))
	}
}

func TestBuildDirectSpeedLimit(t *testing.T) {
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
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{srv.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	u.SpeedLimit = 50
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.SpeedLimits) != 1 {
		t.Fatalf("limits %+v", b.Apply.SpeedLimits)
	}
	mark := SpeedMark(u.ID)
	if b.Apply.SpeedLimits[0].Mark != mark || b.Apply.SpeedLimits[0].Mbps != 50 {
		t.Fatalf("limit %+v mark %d", b.Apply.SpeedLimits[0], mark)
	}

	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	wantTag := limitTag(mark)
	foundOut := false
	for _, raw := range xray["outbounds"].([]any) {
		ob := raw.(map[string]any)
		if ob["tag"] != wantTag {
			continue
		}
		foundOut = true
		ss, _ := ob["streamSettings"].(map[string]any)
		so, _ := ss["sockopt"].(map[string]any)
		if ob["protocol"] != "freedom" || so["mark"] != float64(mark) {
			t.Fatalf("outbound %+v", ob)
		}
	}
	if !foundOut {
		t.Fatal("missing limit outbound")
	}

	c, err := db.GetClient(d, created.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	inTag := inboundTag(created.ID)
	userRuleAt, directRuleAt := -1, -1
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		if len(tags) != 1 || tags[0] != inTag {
			continue
		}
		if users, ok := m["user"].([]any); ok && len(users) == 1 && users[0] == c.Email {
			if m["outboundTag"] != wantTag {
				t.Fatalf("user rule %+v", m)
			}
			userRuleAt = i
			continue
		}
		if m["outboundTag"] == "direct" {
			directRuleAt = i
		}
	}
	if userRuleAt < 0 || directRuleAt < 0 || userRuleAt >= directRuleAt {
		t.Fatalf("rule order user=%d direct=%d", userRuleAt, directRuleAt)
	}

	rev50 := b.Rev
	u.SpeedLimit = 100
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b2, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b2.Rev == rev50 {
		t.Fatal("rev should change when only mbps changes")
	}
	if len(b2.Apply.SpeedLimits) != 1 || b2.Apply.SpeedLimits[0].Mbps != 100 {
		t.Fatalf("limits %+v", b2.Apply.SpeedLimits)
	}

	u.SpeedLimit = 0
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	b3, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b3.Apply.SpeedLimits) != 0 {
		t.Fatalf("unlimited still has limits %+v", b3.Apply.SpeedLimits)
	}
}

func TestBuildChainDoesNotStealSpeed(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	direct := &db.Inbound{
		ServerID: srv.ID, Name: "v1", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(direct, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, direct); err != nil {
		t.Fatal(err)
	}
	chain := &db.Inbound{
		ServerID: srv.ID, Name: "relay", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain",
		ExitURI: "socks5://u:p@203.0.113.9:1080", Settings: "{}",
	}
	if err := Normalize(chain, nil); err != nil {
		t.Fatal(err)
	}
	createdChain, err := db.CreateInbound(d, chain)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{srv.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	u.SpeedLimit = 50
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	routing, _ := xray["routing"].(map[string]any)
	chainTag := inboundTag(createdChain.ID)
	limitPrefix := limitTag(SpeedMark(u.ID))
	for _, raw := range routing["rules"].([]any) {
		m, _ := raw.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		if len(tags) != 1 || tags[0] != chainTag {
			continue
		}
		ob, _ := m["outboundTag"].(string)
		if ob == limitPrefix || ob == "direct" {
			t.Fatalf("chain inbound stolen: %+v", m)
		}
		if _, ok := m["user"]; ok {
			t.Fatalf("chain inbound has user rule: %+v", m)
		}
	}
}

func TestBuildSingboxSpeedLimit(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := db.CreateCert(d, &db.Certificate{
		Name: "c1", CertPEM: "-----BEGIN CERTIFICATE-----\nA\n-----END CERTIFICATE-----",
		KeyPEM: "-----BEGIN PRIVATE KEY-----\nB\n-----END PRIVATE KEY-----", Domains: "a.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "a1", Profile: ProfileAnyTLS,
		Port: 8443, Enabled: true, LineKind: "direct", Settings: "{}", CertID: &cert.ID,
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{srv.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	u.SpeedLimit = 20
	if err := db.UpdateUser(d, u); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Singbox) == 0 {
		t.Fatal("singbox")
	}
	mark := SpeedMark(u.ID)
	if len(b.Apply.SpeedLimits) != 1 || b.Apply.SpeedLimits[0].Mark != mark || b.Apply.SpeedLimits[0].Mbps != 20 {
		t.Fatalf("limits %+v", b.Apply.SpeedLimits)
	}
	var cfg map[string]any
	if err := json.Unmarshal(b.Apply.Singbox, &cfg); err != nil {
		t.Fatal(err)
	}
	wantTag := limitTag(mark)
	found := false
	for _, raw := range cfg["outbounds"].([]any) {
		ob := raw.(map[string]any)
		if ob["tag"] != wantTag {
			continue
		}
		found = true
		if ob["type"] != "direct" || ob["routing_mark"] != float64(mark) {
			t.Fatalf("outbound %+v", ob)
		}
	}
	if !found {
		t.Fatal("missing marked outbound")
	}
	c, err := db.GetClient(d, created.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := cfg["route"].(map[string]any)
	hit := false
	for _, raw := range route["rules"].([]any) {
		m, _ := raw.(map[string]any)
		users, _ := m["auth_user"].([]any)
		if len(users) == 1 && users[0] == c.Email && m["outbound"] == wantTag {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("missing auth_user rule in %+v", route["rules"])
	}
}
