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

func TestWGPeerMissFallsBackToEmail(t *testing.T) {
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
	u, err := db.CreateUser(d, "miss", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, UUID: "10.7.0.10/32",
		Username: "client-key", Password: "priv", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	// One customer and one busy /32: the key missed, so the kernel counter is
	// the usage. The email sample is the same traffic and must not be added.
	h.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		Samples:    []wsproto.Sample{{Email: email, Up: 99999, Down: 99999}},
		WGPeers:    []wsproto.WGPeerStat{{PublicKey: "other", AllowedIPs: "10.9.9.9/32", Rx: 1000, Tx: 1000}},
	})
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 1000 || u.UsedDown != 1000 {
		t.Fatalf("sole client %+v %+v", u.UsedUp, u.UsedDown)
	}
	if _, _, ok := h.UserLive(u.ID); ok {
		t.Fatal("first sight must not show a rate")
	}
	if h.WGStats(srv.ID) != "ok" {
		t.Fatalf("matched fallback %s", h.WGStats(srv.ID))
	}

	h.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		Samples:    []wsproto.Sample{{Email: email, Up: 7, Down: 8}},
	})
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 1007 || u.UsedDown != 1008 {
		t.Fatalf("empty peer flag dropped email %+v %+v", u.UsedUp, u.UsedDown)
	}
}

func TestWGPeerAddressAndEmailMatch(t *testing.T) {
	if got := wgUUIDCandidates("10.7.0.10"); len(got) != 2 || got[0] != "10.7.0.10" || got[1] != "10.7.0.10/32" {
		t.Fatalf("bare %v", got)
	}
	if got := wgUUIDCandidates("10.7.0.10/32"); len(got) != 2 || got[1] != "10.7.0.10" {
		t.Fatalf("prefix %v", got)
	}
	if wgUUIDCandidates("0.0.0.0/0") != nil || wgUUIDCandidates("192.0.2.1/32") != nil {
		t.Fatal("unbilled address became a candidate")
	}

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
	u, err := db.CreateUser(d, "addr", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, UUID: "10.7.0.10/32",
		Username: "stored-key", Password: "priv", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		Samples:    []wsproto.Sample{{Email: email, Up: 99999, Down: 99999}},
		WGPeers:    []wsproto.WGPeerStat{{PublicKey: "transfer-only", AllowedIPs: "10.7.0.10", Rx: 100, Tx: 40}},
	})
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 100 || u.UsedDown != 40 {
		t.Fatalf("bare address %+v %+v", u.UsedUp, u.UsedDown)
	}
	if _, _, ok := h.UserLive(u.ID); ok {
		t.Fatal("first sight must not show a rate")
	}

	h.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		Samples:    []wsproto.Sample{{Email: email, Up: 99999, Down: 99999}},
		WGPeers:    []wsproto.WGPeerStat{{PublicKey: "nope", Email: email, Rx: 180, Tx: 90}},
	})
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 180 || u.UsedDown != 90 {
		t.Fatalf("email match %+v %+v", u.UsedUp, u.UsedDown)
	}
	up, down, ok := h.UserLive(u.ID)
	if !ok || up != 16 || down != 10 {
		t.Fatalf("live %d %d %v", up, down, ok)
	}
}

func TestWGPeerSoleClientLiveAndNoDoubleCount(t *testing.T) {
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
	u, err := db.CreateUser(d, "solo", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, UUID: "10.7.0.10/32",
		Username: "stored-key", Password: "priv", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	if h.WGStats(srv.ID) != "missing" {
		t.Fatalf("before report %s", h.WGStats(srv.ID))
	}
	peer := func(rx, tx int64) wsproto.Stats {
		return wsproto.Stats{
			HasWGPeers: true,
			Samples:    []wsproto.Sample{{Email: email, Up: 99999, Down: 99999}},
			WGPeers:    []wsproto.WGPeerStat{{PublicKey: "not-the-stored-key", AllowedIPs: "10.7.0.10/32", Rx: rx, Tx: tx}},
		}
	}
	h.consumeStats(srv.ID, peer(1000, 400))
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 1000 || u.UsedDown != 400 {
		t.Fatalf("address match %+v %+v", u.UsedUp, u.UsedDown)
	}
	if _, _, ok := h.UserLive(u.ID); ok {
		t.Fatal("first sight must not show a rate")
	}
	h.consumeStats(srv.ID, peer(11000, 4400))
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 11000 || u.UsedDown != 4400 {
		t.Fatalf("grew %+v %+v", u.UsedUp, u.UsedDown)
	}
	up, down, ok := h.UserLive(u.ID)
	if !ok || up != 2000 || down != 800 {
		t.Fatalf("live %d %d %v", up, down, ok)
	}
}

func TestWGPeerFallbackDoesNotGuess(t *testing.T) {
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
	mk := func(name, addr string) (int64, string) {
		t.Helper()
		u, err := db.CreateUser(d, name, "h", "user", "")
		if err != nil {
			t.Fatal(err)
		}
		email := db.EmailFor(u.ID, in.ID)
		if err := db.UpsertClient(d, &db.Client{
			InboundID: in.ID, UserID: u.ID, Email: email, UUID: addr,
			Username: name + "-key", Password: "priv", Enabled: true,
		}); err != nil {
			t.Fatal(err)
		}
		return u.ID, email
	}
	idA, emailA := mk("a", "10.7.0.10/32")
	idB, _ := mk("b", "10.7.0.11/32")
	h := NewHub(d)
	h.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		Samples:    []wsproto.Sample{{Email: emailA, Up: 30, Down: 40}},
		WGPeers:    []wsproto.WGPeerStat{{PublicKey: "stranger", AllowedIPs: "10.9.9.9/32", Rx: 5000, Tx: 5000}},
	})
	a, _ := db.GetUser(d, idA)
	b, _ := db.GetUser(d, idB)
	if a.UsedUp != 30 || a.UsedDown != 40 || b.UsedUp != 0 || b.UsedDown != 0 {
		t.Fatalf("guessed %+v %+v / %+v %+v", a.UsedUp, a.UsedDown, b.UsedUp, b.UsedDown)
	}
	if h.WGStats(srv.ID) != "unmatched" {
		t.Fatalf("stats %s", h.WGStats(srv.ID))
	}
}

func TestWGPeerFallbackSkipsExitProbeAndTunnel(t *testing.T) {
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
	u, err := db.CreateUser(d, "only", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, UUID: "10.7.0.10/32",
		Username: "stored-key", Password: "priv", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		Samples:    []wsproto.Sample{{Email: email, Up: 12, Down: 13}},
		WGPeers: []wsproto.WGPeerStat{
			{PublicKey: "exit", AllowedIPs: "0.0.0.0/0", Rx: 9_000_000, Tx: 9_000_000},
			{PublicKey: "probe", AllowedIPs: "192.0.2.1/32", Rx: 50, Tx: 50},
			{PublicKey: "home", AllowedIPs: "10.7.0.0/24", Rx: 9_000_000, Tx: 9_000_000},
			{PublicKey: "xfer", AllowedIPs: "", Rx: 9_000_000, Tx: 9_000_000},
		},
	})
	u, _ = db.GetUser(d, u.ID)
	if u.UsedUp != 12 || u.UsedDown != 13 {
		t.Fatalf("unbilled peer billed %+v %+v", u.UsedUp, u.UsedDown)
	}
	if h.WGStats(srv.ID) != "ok" {
		t.Fatalf("idle peers %s", h.WGStats(srv.ID))
	}
}

func TestUserWGStatsHints(t *testing.T) {
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
	vless, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "vless", Profile: "vless", Protocol: "vless",
		Network: "tcp", Security: "none", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	wgUser, err := db.CreateUser(d, "wg", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateUser(d, "vless", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: wgUser.ID, Email: db.EmailFor(wgUser.ID, in.ID),
		UUID: "10.7.0.10/32", Username: "wg-key", Password: "priv", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertClient(d, &db.Client{
		InboundID: vless.ID, UserID: other.ID, Email: db.EmailFor(other.ID, vless.ID),
		UUID: "11111111-1111-1111-1111-111111111111", Username: "v", Password: "p", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	s := &Server{DB: d, Hub: NewHub(d)}
	if got := s.userWGStats(other.ID); got != "" {
		t.Fatalf("vless user %q", got)
	}
	if got := s.userWGStats(wgUser.ID); got != "offline" {
		t.Fatalf("offline %q", got)
	}
	if err := db.MarkServerOnline(d, srv.ID, "0.3.11", "linux", "amd64", "1.1.1.1", nil); err != nil {
		t.Fatal(err)
	}
	if got := s.userWGStats(wgUser.ID); got != "missing" {
		t.Fatalf("missing %q", got)
	}
	s.Hub.consumeStats(srv.ID, wsproto.Stats{
		HasWGPeers: true,
		WGPeers:    []wsproto.WGPeerStat{{PublicKey: "exit", AllowedIPs: "0.0.0.0/0", Rx: 10, Tx: 10}},
	})
	if got := s.userWGStats(wgUser.ID); got != "" {
		t.Fatalf("reporting %q", got)
	}
}
