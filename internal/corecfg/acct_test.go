package corecfg

import (
	"encoding/json"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestAcctMarkSkipsRelayAndChain(t *testing.T) {
	direct := &db.Inbound{ID: 3, Core: CoreXray, Profile: ProfileVLESSRealityVision, Enabled: true, LineKind: "direct", Port: 443}
	chain := &db.Inbound{ID: 4, Core: CoreXray, Profile: ProfileVLESSRealityVision, Enabled: true, LineKind: "chain", Port: 444}
	socks := &db.Inbound{ID: 5, Core: CoreSingbox, Profile: ProfileSOCKS5, Enabled: true, LineKind: "direct", Port: 1080}
	clients := map[int64][]*db.Client{
		3: {
			{ID: 9, InboundID: 3, UserID: 1, Email: "u1.i3", Enabled: true},
			{ID: 10, InboundID: 3, UserID: 2, Email: "relay.i4", Enabled: true},
		},
		4: {{ID: 11, InboundID: 4, UserID: 1, Email: "u1.i4", Enabled: true}},
		5: {{ID: 12, InboundID: 5, UserID: 3, Email: "", Username: "neo", Password: "pw", Enabled: true}},
	}
	marks := collectAcctMarks([]*db.Inbound{direct, chain, socks}, clients)
	if len(marks) != 2 {
		t.Fatalf("marks %+v", marks)
	}
	got := map[string]uint32{}
	for _, m := range marks {
		got[m.Email] = m.Mark
	}
	if got["u1.i3"] != AcctMark(clients[3][0], 3) || got["neo"] == 0 {
		t.Fatalf("%+v", marks)
	}
	if _, ok := got["relay.i4"]; ok {
		t.Fatal("relay billed")
	}
	if _, ok := got["u1.i4"]; ok {
		t.Fatal("chain billed on entry mark list")
	}
}

func TestSingboxConfigOmitsV2RayAPI(t *testing.T) {
	in := &db.Inbound{ID: 7, Core: CoreSingbox, Profile: ProfileSOCKS5, Enabled: true, Listen: "0.0.0.0", Port: 20000, LineKind: "direct"}
	clients := map[int64][]*db.Client{
		7: {{ID: 1, InboundID: 7, UserID: 3, Email: "u3.i7", Password: "pw", Enabled: true}},
	}
	cfg, err := buildSingbox([]*db.Inbound{in}, clients, nil, nil, 60190, 60191, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "v2ray_api") {
		t.Fatalf("v2ray_api in %s", raw)
	}
	want := AcctMark(clients[7][0], 7)
	found := false
	for _, ob := range cfg["outbounds"].([]any) {
		m := ob.(map[string]any)
		switch n := m["routing_mark"].(type) {
		case uint32:
			found = n == want
		case float64:
			found = uint32(n) == want
		}
		if found {
			break
		}
	}
	if !found {
		t.Fatal("socks user has no marked outbound")
	}
}
