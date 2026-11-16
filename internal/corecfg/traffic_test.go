package corecfg

import (
	"encoding/json"
	"strings"
	"testing"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestForwardProtoAndRewrite(t *testing.T) {
	if ForwardProto(ProfileHysteria2, "") != "udp" || ForwardProto(ProfileWireGuard, "") != "udp" {
		t.Fatal("udp profiles")
	}
	if ForwardProto(ProfileSOCKS5, "") != "tcp+udp" {
		t.Fatal("socks")
	}
	if ForwardProto(ProfileMieru, "UDP") != "udp" || ForwardProto(ProfileMieru, "TCP") != "tcp" || ForwardProto(ProfileMieru, "BOTH") != "tcp+udp" {
		t.Fatal("mieru")
	}
	if ForwardProto(ProfileVLESSRealityVision, "tcp") != "tcp" {
		t.Fatal("vless")
	}
	if err := ValidDialHost("203.0.113.21"); err != nil {
		t.Fatal(err)
	}
	if err := ValidDialHost("203.0.113.21:1"); err == nil {
		t.Fatal("port should be rejected")
	}

	landing := &db.Inbound{Name: "家宽", Port: 18443, ServerHost: "203.0.113.12", ConnectIP: "203.0.113.12", ServerID: 3}
	fw := &db.TrafficForward{Name: "家宽", EntryPort: 21000, EntryServerID: 1}
	cp := RewriteForwardInbound(landing, fw, "203.0.113.10")
	if cp.Name != "家宽 · 入口" || cp.Port != 21000 || cp.ServerHost != "203.0.113.10" || cp.ConnectIP != "" || cp.ServerID != 1 {
		t.Fatalf("rewrite %+v", cp)
	}
	if landing.Port != 18443 || landing.ServerID != 3 {
		t.Fatal("landing mutated")
	}
}

func TestTrafficRulesForServer(t *testing.T) {
	fw := &db.TrafficForward{
		ID: 7, Enabled: true, EntryServerID: 1, EntryPort: 21000,
		LandingInboundID: 9, LandingDialHost: "203.0.113.22",
		Hops: []db.TrafficHop{{ServerID: 2, ListenPort: 21001, DialHost: "203.0.113.21"}},
	}
	servers := map[int64]*db.Server{
		1: {ID: 1, PublicHost: "203.0.113.10", Name: "上海火山"},
		2: {ID: 2, PublicHost: "203.0.113.11", Name: "沪日 IX"},
		3: {ID: 3, PublicHost: "203.0.113.12", Name: "美国家宽"},
	}
	landing := &db.Inbound{ID: 9, ServerID: 3, Port: 18443, Profile: ProfileSOCKS5, Name: "家宽"}
	entry := TrafficRulesForServer(fw, 1, servers, landing)
	if len(entry) != 1 || entry[0].Port != 21000 || entry[0].DestHost != "203.0.113.21" || entry[0].DestPort != 21001 || entry[0].Proto != "tcp+udp" {
		t.Fatalf("entry %+v", entry)
	}
	hop := TrafficRulesForServer(fw, 2, servers, landing)
	if len(hop) != 1 || hop[0].DestHost != "203.0.113.22" || hop[0].DestPort != 18443 || hop[0].Proto != "tcp+udp" {
		t.Fatalf("hop %+v", hop)
	}
	if TrafficRulesForServer(fw, 3, servers, landing) != nil {
		t.Fatal("landing machine should not forward")
	}
	hy := *landing
	hy.Profile = ProfileHysteria2
	if got := TrafficRulesForServer(fw, 1, servers, &hy); len(got) != 1 || got[0].Proto != "udp" {
		t.Fatalf("hy2 %+v", got)
	}
	off := *fw
	off.Enabled = false
	if TrafficRulesForServer(&off, 1, servers, landing) != nil {
		t.Fatal("disabled")
	}
}

func TestPrepareAndBuildForwards(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	entry, err := db.CreateServer(d, "上海火山", "203.0.113.10", "tok-entry")
	if err != nil {
		t.Fatal(err)
	}
	hop, err := db.CreateServer(d, "沪日 IX", "203.0.113.11", "tok-hop")
	if err != nil {
		t.Fatal(err)
	}
	landSrv, err := db.CreateServer(d, "美国家宽", "203.0.113.12", "tok-land")
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: landSrv.ID, Name: "家宽", Profile: ProfileSOCKS5, Protocol: "socks",
		Network: "tcp", Core: "singbox", Listen: "0.0.0.0", Port: 18443, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	bare, err := Build(d, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if bare.Apply.Forwards == nil {
		t.Fatal("missing forwards field")
	}
	raw, err := json.Marshal(bare.Apply)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Forwards []wsproto.ForwardRule `json:"forwards"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Forwards == nil || len(decoded.Forwards) != 0 {
		t.Fatalf("empty forwards %+v", decoded.Forwards)
	}

	fw := &db.TrafficForward{
		Enabled:          true,
		EntryServerID:    entry.ID,
		LandingInboundID: in.ID,
		LandingDialHost:  "203.0.113.22",
		Hops:             []db.TrafficHop{{ServerID: hop.ID, DialHost: "203.0.113.21"}},
	}
	if err := PrepareTrafficForward(d, fw); err != nil {
		t.Fatal(err)
	}
	if fw.EntryPort < 10000 || fw.Hops[0].ListenPort < 10000 || fw.Name == "" {
		t.Fatalf("prepared %+v", fw)
	}
	if err := db.InsertTrafficForward(d, fw); err != nil {
		t.Fatal(err)
	}
	built, err := Build(d, entry.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Apply.Forwards) != 1 || built.Apply.Forwards[0].DestHost != "203.0.113.21" {
		t.Fatalf("entry build %+v", built.Apply.Forwards)
	}
	hopBuilt, err := Build(d, hop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hopBuilt.Apply.Forwards) != 1 || hopBuilt.Apply.Forwards[0].DestHost != "203.0.113.22" || hopBuilt.Apply.Forwards[0].DestPort != 18443 {
		t.Fatalf("hop build %+v", hopBuilt.Apply.Forwards)
	}
	landBuilt, err := Build(d, landSrv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if landBuilt.Apply.Forwards == nil || len(landBuilt.Apply.Forwards) != 0 {
		t.Fatalf("landing build %+v", landBuilt.Apply.Forwards)
	}
	dup := &db.TrafficForward{EntryServerID: landSrv.ID, LandingInboundID: in.ID}
	if err := PrepareTrafficForward(d, dup); err == nil || !strings.Contains(err.Error(), "同一台") {
		t.Fatalf("duplicate machine: %v", err)
	}
}
