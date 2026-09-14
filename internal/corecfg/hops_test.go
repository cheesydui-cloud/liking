package corecfg

import (
	"strings"
	"testing"

	"liking/internal/db"
)

func TestNormalizeHops(t *testing.T) {
	st := Settings{"hops": []any{
		map[string]any{"kind": "panel", "inbound_id": 3},
		map[string]any{"kind": "socks", "uri": "socks5://u:p@10.0.0.2:1080"},
	}}
	if err := NormalizeHops(st); err != nil {
		t.Fatal(err)
	}
	hops := ParseHops(st)
	if len(hops) != 2 || hops[0].InboundID != 3 || hops[1].URI == "" {
		t.Fatalf("%+v", hops)
	}

	tooMany := make([]any, MaxHops)
	for i := range tooMany {
		tooMany[i] = map[string]any{"kind": "panel", "inbound_id": i + 1}
	}
	if err := NormalizeHops(Settings{"hops": tooMany}); err == nil {
		t.Fatal("expected max hops")
	}

	if err := NormalizeHops(Settings{"hops": []any{map[string]any{"kind": "panel"}}}); err == nil {
		t.Fatal("expected missing node")
	}
	if err := NormalizeHops(Settings{"hops": []any{
		map[string]any{"kind": "panel", "inbound_id": 1, "uri": "socks5://x:y@1.1.1.1:1"},
	}}); err == nil {
		t.Fatal("expected xor")
	}
}

func TestApplyHopRelays(t *testing.T) {
	land := &db.Inbound{ID: 9, Profile: ProfileVLESSReality, Port: 443, LineKind: "direct"}
	st := Settings{"hops": []any{map[string]any{"kind": "panel", "inbound_id": 9}}}
	if err := ApplyHopRelays(st, map[int64]*db.Inbound{9: land}, 1, 8); err != nil {
		t.Fatal(err)
	}
	hops := ParseHops(st)
	if len(hops) != 1 || hops[0].RelayUUID == "" {
		t.Fatalf("relay %+v", hops)
	}
	if err := ApplyHopRelays(st, map[int64]*db.Inbound{9: land}, 1, 9); err == nil {
		t.Fatal("expected duplicate landing")
	}
	chain := &db.Inbound{ID: 4, Profile: ProfileVLESSReality, Port: 1, LineKind: "chain"}
	bad := Settings{"hops": []any{map[string]any{"kind": "panel", "inbound_id": 4}}}
	if err := ApplyHopRelays(bad, map[int64]*db.Inbound{4: chain}, 1, 8); err == nil {
		t.Fatal("expected reject chain hop")
	}
}

func TestChainPathTags(t *testing.T) {
	hopLand := &db.Inbound{ID: 2, Name: "h", Profile: ProfileVLESSReality, Port: 443, LineKind: "direct", ServerHost: "10.0.0.2"}
	land := &db.Inbound{ID: 3, Name: "l", Profile: ProfileVLESSReality, Port: 443, LineKind: "direct", ServerHost: "10.0.0.3"}
	exit := land.ID
	entry := &db.Inbound{
		ID: 1, Name: "in", Profile: ProfileVLESSReality, Port: 8443, LineKind: "chain",
		ExitInboundID: &exit,
		Settings:      `{"hops":[{"kind":"panel","inbound_id":2,"relay_uuid":"aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee"}],"relay_uuid":"11111111-1111-4111-8111-111111111111"}`,
	}
	path, err := ChainPath(entry, map[int64]*db.Inbound{2: hopLand, 3: land})
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 2 {
		t.Fatalf("len %d", len(path))
	}
	if path[0].Tag != "ob-1-h0" || path[0].Land == nil || path[0].Land.ID != 2 {
		t.Fatalf("hop0 %+v", path[0])
	}
	if path[1].Tag != "ob-1" || path[1].Land == nil || path[1].Land.ID != 3 {
		t.Fatalf("land %+v", path[1])
	}

	sk := &db.Inbound{
		ID: 5, Name: "sk", Profile: ProfileVLESSReality, Port: 1, LineKind: "chain",
		ExitURI: "socks5://u:p@203.0.113.9:1080", Settings: "{}",
	}
	p2, err := ChainPath(sk, nil)
	if err != nil || len(p2) != 1 || p2[0].Socks == nil || p2[0].Socks.Host != "203.0.113.9" {
		t.Fatalf("sk5 %+v %v", p2, err)
	}
	if p2[0].Share == nil || p2[0].Share.Scheme != "socks5" {
		t.Fatalf("share %+v", p2[0].Share)
	}
}

func TestCountHopsTo(t *testing.T) {
	list := []*db.Inbound{
		{ID: 1, Settings: `{"hops":[{"kind":"panel","inbound_id":9}]}`},
		{ID: 2, Settings: `{"hops":[{"kind":"socks","uri":"socks5://h:1"}]}`},
	}
	if n := CountHopsTo(list, 9); n != 1 {
		t.Fatalf("n=%d", n)
	}
	if n := CountHopsTo(list, 1); n != 0 {
		t.Fatalf("self %d", n)
	}
}

func TestHopInboundIDs(t *testing.T) {
	in := &db.Inbound{Settings: `{"hops":[{"kind":"panel","inbound_id":4},{"kind":"socks","uri":"socks5://x:y@1.1.1.1:1080"}]}`}
	ids := HopInboundIDs(in)
	if len(ids) != 1 || ids[0] != 4 {
		t.Fatalf("%v", ids)
	}
}

func TestNormalizeHopsEmptyDeletes(t *testing.T) {
	st := Settings{"hops": []any{}, "dest": "www.microsoft.com:443"}
	if err := NormalizeHops(st); err != nil {
		t.Fatal(err)
	}
	if _, ok := st["hops"]; ok {
		t.Fatalf("hops should be dropped: %+v", st)
	}
	if !strings.Contains(st.String("dest"), "microsoft") {
		t.Fatalf("%+v", st)
	}
}
