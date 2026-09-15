package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestAgentHello(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srvRow, err := db.CreateServer(d, "n1", "1.2.3.4", "tokentokentoken")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Router())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")

	hello, _ := json.Marshal(wsproto.Hello{Token: srvRow.Token, AgentVersion: "test", OS: "linux", Arch: "amd64"})
	b, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeHello, ID: "1", Payload: hello})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	_, raw, err := ws.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var env wsproto.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if env.Type != wsproto.TypeHelloAck {
		t.Fatalf("got %s", env.Type)
	}
	var ack wsproto.HelloAck
	_ = json.Unmarshal(env.Payload, &ack)
	if ack.Error != "" || ack.ServerID != srvRow.ID {
		t.Fatalf("%+v", ack)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(srvRow.ID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("not online")
}

func TestNoteLive(t *testing.T) {
	h := NewHub(nil)
	ac := &agentConn{}
	h.noteLive(ac, wsproto.Stats{HasNet: true, NetUp: 1200, NetDown: 3400})
	if ac.upBps != 1200 || ac.downBps != 3400 || ac.lastStatsAt.IsZero() {
		t.Fatalf("nic %+v", ac)
	}
	ac2 := &agentConn{}
	h.noteLive(ac2, wsproto.Stats{Samples: []wsproto.Sample{{Up: 5000, Down: 9000}}})
	ac2.lastStatsAt = time.Now().Add(-2 * time.Second)
	h.noteLive(ac2, wsproto.Stats{Samples: []wsproto.Sample{{Up: 4000, Down: 8000}}})
	if ac2.upBps < 1500 || ac2.downBps < 3000 {
		t.Fatalf("sample bps up=%d down=%d", ac2.upBps, ac2.downBps)
	}
}

func TestUserLiveBps(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := db.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := db.CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "a", Profile: "vless-reality", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.applyStats(srv.ID, []wsproto.Sample{{Email: email, Up: 10000, Down: 20000}})
	up, down, ok := h.UserLive(u.ID)
	if !ok || up != 2000 || down != 4000 {
		t.Fatalf("first tick up=%d down=%d ok=%v", up, down, ok)
	}

	tok2, _ := db.RandomHex(8)
	srv2, err := db.CreateServer(d, "n2", "2.2.2.2", tok2)
	if err != nil {
		t.Fatal(err)
	}
	in2, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv2.ID, Name: "b", Profile: "vless-reality", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	email2 := db.EmailFor(u.ID, in2.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in2.ID, UserID: u.ID, Email: email2, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h.applyStats(srv2.ID, []wsproto.Sample{{Email: email2, Up: 5000, Down: 5000}})
	up, down, ok = h.UserLive(u.ID)
	if !ok || up != 3000 || down != 5000 {
		t.Fatalf("sum up=%d down=%d ok=%v", up, down, ok)
	}

	h.applyStats(srv.ID, nil)
	up, down, ok = h.UserLive(u.ID)
	if !ok || up != 3000 || down != 5000 {
		t.Fatalf("empty tick must keep live up=%d down=%d ok=%v", up, down, ok)
	}

	h.clearUserLive(srv2.ID)
	up, down, ok = h.UserLive(u.ID)
	if !ok || up != 2000 || down != 4000 {
		t.Fatalf("after disconnect server2 up=%d down=%d ok=%v", up, down, ok)
	}

	h.clearUserLive(srv.ID)
	if _, _, ok := h.UserLive(u.ID); ok {
		t.Fatal("expected idle after both servers cleared")
	}
}

func TestUserLiveChainInbound(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := db.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := db.CreateServer(d, "jp", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "tw", Profile: "vless-reality-vision", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 18461, Enabled: true, Settings: "{}", LineKind: "chain",
		ExitURI: "ss://Y2hhY2hhMjAtaWV0Zi1wb2x5MTMwNTpwYXNz@203.0.113.9:51491",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "mmvxv", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.applyStats(srv.ID, []wsproto.Sample{
		{Email: "relay.i4", Up: 99999, Down: 99999},
		{Email: email, Up: 10000, Down: 40000},
	})
	up, down, ok := h.UserLive(u.ID)
	if !ok || up != 2000 || down != 8000 {
		t.Fatalf("chain live up=%d down=%d ok=%v", up, down, ok)
	}
	h.applyStats(srv.ID, []wsproto.Sample{{Email: "relay.i4", Up: 1, Down: 1}})
	up, down, ok = h.UserLive(u.ID)
	if !ok || up != 2000 || down != 8000 {
		t.Fatalf("relay-only tick wiped chain live up=%d down=%d ok=%v", up, down, ok)
	}
}

func TestUserLiveTTL(t *testing.T) {
	old := userLiveTTL
	userLiveTTL = 20 * time.Millisecond
	defer func() { userLiveTTL = old }()

	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := db.RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := db.CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "a", Profile: "vless-reality", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: email, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.applyStats(srv.ID, []wsproto.Sample{{Email: email, Up: 10000, Down: 20000}})
	if _, _, ok := h.UserLive(u.ID); !ok {
		t.Fatal("expected live")
	}
	time.Sleep(40 * time.Millisecond)
	if _, _, ok := h.UserLive(u.ID); ok {
		t.Fatal("expected ttl expiry")
	}
}

func TestAllUserLiveIncludesAdmin(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, _ := db.RandomHex(8)
	srv, err := db.CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "a", Profile: "vless-reality", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := db.CreateUser(d, "root", "h", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := db.CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	emAdmin := db.EmailFor(admin.ID, in.ID)
	emBob := db.EmailFor(bob.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{InboundID: in.ID, UserID: admin.ID, Email: emAdmin, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := db.UpsertClient(d, &db.Client{InboundID: in.ID, UserID: bob.ID, Email: emBob, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.applyStats(srv.ID, []wsproto.Sample{
		{Email: emAdmin, Up: 10000, Down: 20000},
		{Email: emBob, Up: 5000, Down: 5000},
	})
	up, down := h.AllUserLive()
	if up != 3000 || down != 5000 {
		t.Fatalf("all live up=%d down=%d", up, down)
	}
	aUp, aDown, ok := h.UserLive(admin.ID)
	if !ok || aUp != 2000 || aDown != 4000 {
		t.Fatalf("admin live up=%d down=%d ok=%v", aUp, aDown, ok)
	}
}
