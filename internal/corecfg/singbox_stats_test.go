package corecfg

import (
	"encoding/json"
	"testing"

	"liking/internal/db"
)

func TestSingboxV2RayStatsUsers(t *testing.T) {
	socks := &db.Inbound{ID: 7, Core: CoreSingbox, Profile: ProfileSOCKS5, Enabled: true, Listen: "0.0.0.0", Port: 20000, LineKind: "direct"}
	anytls := &db.Inbound{ID: 8, Core: CoreSingbox, Profile: ProfileAnyTLS, Enabled: true, Listen: "0.0.0.0", Port: 20001, LineKind: "direct", CertID: ptrInt64(1)}
	certs := map[int64]*db.Certificate{1: {ID: 1, CertPEM: "cert", KeyPEM: "key"}}
	clients := map[int64][]*db.Client{
		7: {
			{InboundID: 7, UserID: 3, Email: "u3.i7", Password: "pw", Enabled: true},
			{InboundID: 7, UserID: 4, Email: "", Username: "neo", Password: "pw", Enabled: true},
			{InboundID: 7, Email: "relay.skip", Password: "pw", Enabled: true},
		},
		8: {
			{InboundID: 8, UserID: 5, Email: "u5.i8", Password: "pw", Enabled: true},
		},
	}
	cfg, err := buildSingbox([]*db.Inbound{socks, anytls}, clients, certs, nil, 60190, 60191, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	exp, _ := cfg["experimental"].(map[string]any)
	v2, _ := exp["v2ray_api"].(map[string]any)
	if v2["listen"] != "127.0.0.1:60191" {
		t.Fatalf("listen %v", v2["listen"])
	}
	stats, _ := v2["stats"].(map[string]any)
	if stats["enabled"] != true {
		t.Fatal("stats not enabled")
	}
	users, _ := stats["users"].([]string)
	if len(users) != 3 || users[0] != "neo" || users[1] != "u3.i7" || users[2] != "u5.i8" {
		t.Fatalf("users %#v", users)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(raw) {
		t.Fatal("invalid json")
	}
}

func ptrInt64(n int64) *int64 { return &n }
