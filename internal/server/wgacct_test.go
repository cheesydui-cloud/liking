package server

import (
	"encoding/base64"
	"testing"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestWGStepCatchUpResetAndLive(t *testing.T) {
	if g := wgGap(8400, 8400); g != 0 {
		t.Fatalf("same %d", g)
	}
	if g := wgGap(100000, 8400); g != 91600 {
		t.Fatalf("gap %d", g)
	}
	if g := wgGap(1000, 8400); g != 0 {
		t.Fatalf("new session must not subtract another life %d", g)
	}

	next, up, down, live := wgStep(wgCursor{}, 10000, 20000, 91600, 100)
	if live || up != 91600 || down != 100 || next.rx != 10000 || !next.have {
		t.Fatalf("catch-up %+v %d %d %v", next, up, down, live)
	}
	glitch, up, down, live := wgStep(next, 10, 12, 0, 0)
	if up != 0 || down != 0 || live || !glitch.pendingReset || glitch.rx != 10000 {
		t.Fatalf("glitch %+v %d %d %v", glitch, up, down, live)
	}
	back, up, down, live := wgStep(glitch, 11000, 22000, 0, 0)
	if up != 1000 || down != 2000 || !live || back.pendingReset || back.rx != 11000 {
		t.Fatalf("recover %+v %d %d %v", back, up, down, live)
	}
	low, up, down, live := wgStep(back, 8, 9, 0, 0)
	if up != 0 || down != 0 || live || !low.pendingReset {
		t.Fatalf("first low %+v %d %d", low, up, down)
	}
	reset, up, down, live := wgStep(low, 8, 9, 0, 0)
	if up != 0 || down != 0 || live || reset.pendingReset || reset.rx != 8 || reset.tx != 9 {
		t.Fatalf("confirmed reset %+v %d %d %v", reset, up, down, live)
	}
	grew, up, down, live := wgStep(reset, 40, 50, 0, 0)
	if up != 32 || down != 41 || !live {
		t.Fatalf("after reset %+v %d %d %v", grew, up, down, live)
	}
}

func TestWGPeerAccounting(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := db.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := db.CreateServer(d, "entry", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "ces", Profile: "wireguard", Protocol: "wireguard",
		Network: "udp", Security: "none", Core: "wireguard", Listen: "0.0.0.0",
		Port: 10433, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "test", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 3)
	}
	pub := base64.StdEncoding.EncodeToString(raw)
	bare := base64.RawStdEncoding.EncodeToString(raw)
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, UUID: "10.7.0.10/32",
		Username: pub, Password: "priv", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDailyTraffic(d, "2026-11-02", u.ID, in.ID, 8400, 100); err != nil {
		t.Fatal(err)
	}
	if err := db.AddUserTraffic(d, u.ID, 8400, 100); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	peer := func(rx, tx int64) wsproto.Stats {
		return wsproto.Stats{
			HasWGPeers: true,
			Samples:    []wsproto.Sample{{Email: email, Up: 99999, Down: 99999}},
			WGPeers: []wsproto.WGPeerStat{
				{PublicKey: "exit", AllowedIPs: "0.0.0.0/0", Rx: 9_000_000, Tx: 9_000_000},
				{PublicKey: "probe", AllowedIPs: "192.0.2.1/32", Rx: 50, Tx: 50},
				{PublicKey: bare, AllowedIPs: "10.7.0.10/32", Rx: rx, Tx: tx},
			},
		}
	}
	h.consumeStats(srv.ID, peer(8400, 100))
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 8400 || u.UsedDown != 100 {
		t.Fatalf("already billed doubled %+v", u.UsedUp)
	}
	if _, _, ok := h.UserLive(u.ID); ok {
		t.Fatal("equal counter must not invent a rate")
	}

	h.consumeStats(srv.ID, peer(18400, 4100))
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 18400 || u.UsedDown != 4100 {
		t.Fatalf("delta %+v %+v", u.UsedUp, u.UsedDown)
	}
	up, down, ok := h.UserLive(u.ID)
	if !ok || up != 2000 || down != 800 {
		t.Fatalf("live %d %d %v", up, down, ok)
	}

	tok2, _ := db.RandomHex(8)
	home, err := db.CreateServer(d, "home", "2.2.2.2", tok2)
	if err != nil {
		t.Fatal(err)
	}
	before := u.UsedUp
	h.consumeStats(home.ID, peer(28400, 5100))
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != before {
		t.Fatalf("home billed the client %d", u.UsedUp)
	}

	h2 := NewHub(d)
	h2.applyStats(srv.ID, []wsproto.Sample{{Email: email, Up: 30, Down: 40}})
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != before+30 || u.UsedDown != 4100+40 {
		t.Fatalf("old email path %+v", u.UsedUp)
	}
}
