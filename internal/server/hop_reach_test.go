package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestPlanChainReachTwoSegments(t *testing.T) {
	hop := &db.Inbound{ID: 2, ServerID: 20, Name: "沪日", Profile: corecfg.ProfileVLESSReality, Port: 443, ServerHost: "203.0.113.20", ServerName: "IX"}
	land := &db.Inbound{ID: 3, ServerID: 30, Name: "家宽", Profile: corecfg.ProfileSOCKS5, Port: 1080, ServerHost: "203.0.113.30", ServerName: "US"}
	exit := land.ID
	entry := &db.Inbound{
		ID: 1, ServerID: 10, Name: "入口", Profile: corecfg.ProfileVLESSReality, Port: 8443, LineKind: "chain",
		ServerName: "上海", ExitInboundID: &exit,
		Settings: `{"hops":[{"kind":"panel","inbound_id":2}]}`,
	}
	segs := planChainReach(entry, map[int64]*db.Inbound{2: hop, 3: land}, nil)
	if len(segs) != 2 {
		t.Fatalf("len %d", len(segs))
	}
	if segs[0].FromID != 10 || segs[0].FromName != "上海" || segs[0].Host != "203.0.113.20" || segs[0].Port != 443 || segs[0].Status != "" {
		t.Fatalf("seg0 %+v", segs[0])
	}
	if segs[1].FromID != 20 || segs[1].Host != "203.0.113.30" || segs[1].Port != 1080 || segs[1].Note != reachUDPUntested || segs[1].Status != "" {
		t.Fatalf("seg1 %+v", segs[1])
	}
}

func TestPlanChainReachSkipsAfterExternal(t *testing.T) {
	land := &db.Inbound{ID: 3, ServerID: 30, Name: "家宽", Profile: corecfg.ProfileVLESSReality, Port: 443, ServerHost: "203.0.113.30", ServerName: "US"}
	exit := land.ID
	entry := &db.Inbound{
		ServerID: 10, Name: "入口", ServerName: "上海", LineKind: "chain", ExitInboundID: &exit,
		Settings: `{"hops":[{"kind":"socks","uri":"socks5://203.0.113.9:1080"}]}`,
	}
	segs := planChainReach(entry, map[int64]*db.Inbound{3: land}, nil)
	if len(segs) != 2 {
		t.Fatalf("len %d", len(segs))
	}
	if segs[0].FromID != 10 || segs[0].Host != "203.0.113.9" || segs[0].Port != 1080 || segs[0].Status != "" {
		t.Fatalf("seg0 %+v", segs[0])
	}
	if segs[1].Status != reachSkip || segs[1].Error != reachExternal || segs[1].FromID != 0 {
		t.Fatalf("seg1 %+v", segs[1])
	}
	if segs[1].Host == "203.0.113.30" && segs[1].Status != reachSkip {
		t.Fatal("external hop must not dial the next segment")
	}
}

func TestPlanTrafficReachUsesDialHost(t *testing.T) {
	fw := &db.TrafficForward{
		EntryServerID: 1, EntryPort: 21000, EntryServerName: "上海",
		LandingDialHost: "203.0.113.50",
		Hops:            []db.TrafficHop{{ServerID: 2, ListenPort: 22000, DialHost: "203.0.113.40", ServerName: "IX"}},
	}
	landing := &db.Inbound{ID: 9, ServerID: 3, Name: "家宽", Profile: corecfg.ProfileVLESSReality, Port: 443, ServerHost: "203.0.113.12"}
	servers := map[int64]*db.Server{
		1: {ID: 1, Name: "上海", PublicHost: "203.0.113.10"},
		2: {ID: 2, Name: "IX", PublicHost: "203.0.113.11"},
		3: {ID: 3, Name: "家宽机", PublicHost: "203.0.113.12"},
	}
	segs := planTrafficReach(fw, servers, landing)
	if len(segs) != 2 {
		t.Fatalf("len %d", len(segs))
	}
	if segs[0].FromID != 1 || segs[0].Host != "203.0.113.40" || segs[0].Port != 22000 || segs[0].Host == "203.0.113.11" {
		t.Fatalf("hop dial %+v", segs[0])
	}
	if segs[1].FromID != 2 || segs[1].Host != "203.0.113.50" || segs[1].Port != 443 {
		t.Fatalf("landing dial %+v", segs[1])
	}
}

func TestPlanTrafficReachUDPDoesNotDial(t *testing.T) {
	fw := &db.TrafficForward{
		EntryServerID: 1, EntryPort: 21000, EntryServerName: "上海",
		Hops: []db.TrafficHop{{ServerID: 2, ListenPort: 22000, DialHost: "203.0.113.40", ServerName: "IX"}},
	}
	landing := &db.Inbound{ID: 9, ServerID: 3, Name: "家宽", Profile: corecfg.ProfileHysteria2, Port: 443, ServerHost: "203.0.113.12"}
	servers := map[int64]*db.Server{
		1: {ID: 1, Name: "上海", PublicHost: "203.0.113.10"},
		2: {ID: 2, Name: "IX", PublicHost: "203.0.113.11"},
		3: {ID: 3, Name: "家宽机", PublicHost: "203.0.113.12"},
	}
	segs := planTrafficReach(fw, servers, landing)
	if len(segs) != 2 {
		t.Fatalf("len %d", len(segs))
	}
	for i, seg := range segs {
		if seg.Status != reachUDP || seg.Note != reachUDPNote {
			t.Fatalf("seg%d %+v", i, seg)
		}
	}
	(&Server{}).fillReach(segs)
	for i, seg := range segs {
		if seg.Status != reachUDP {
			t.Fatalf("udp dialed seg%d %+v", i, seg)
		}
	}
}

func TestPlanPortReach(t *testing.T) {
	in := &db.Inbound{
		ServerID: 10, ServerName: "上海", Profile: corecfg.ProfilePortForward,
		Settings: `{"dest_host":"203.0.113.8","dest_port":443,"network":"tcp,udp"}`,
	}
	segs := planPortReach(in)
	if len(segs) != 1 || segs[0].FromID != 10 || segs[0].Host != "203.0.113.8" || segs[0].Port != 443 || segs[0].Note != reachUDPUntested || segs[0].Status != "" {
		t.Fatalf("tcp+udp %+v", segs)
	}
	in.Settings = `{"dest_host":"203.0.113.8","dest_port":443,"network":"udp"}`
	segs = planPortReach(in)
	if len(segs) != 1 || segs[0].Status != reachUDP || segs[0].Note != reachUDPNote {
		t.Fatalf("udp %+v", segs)
	}
	in.Settings = `{"network":"tcp"}`
	segs = planPortReach(in)
	if len(segs) != 1 || segs[0].Status != reachSkip || segs[0].Error != reachNoDest {
		t.Fatalf("missing %+v", segs)
	}
}

func TestHopProbeHTTP(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	hash, err := HashPassword("secret12")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(d, "admin", hash, "admin", ""); err != nil {
		t.Fatal(err)
	}
	entry, err := db.CreateServer(d, "上海", "203.0.113.10", "tokentokentoken1")
	if err != nil {
		t.Fatal(err)
	}
	hopSrv, err := db.CreateServer(d, "IX", "203.0.113.20", "tokentokentoken2")
	if err != nil {
		t.Fatal(err)
	}
	landSrv, err := db.CreateServer(d, "家宽", "203.0.113.30", "tokentokentoken3")
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

	var probes atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents"
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	hello, _ := json.Marshal(wsproto.Hello{
		Token: entry.Token, AgentVersion: "0.3.41", OS: "linux", Arch: "amd64",
		Cores: []string{"xray"}, Caps: []string{wsproto.CapProbe},
	})
	b, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeHello, ID: "1", Payload: hello})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, raw, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var env wsproto.Envelope
			if json.Unmarshal(raw, &env) != nil || env.Type != wsproto.TypeProbe {
				continue
			}
			probes.Add(1)
			ack, _ := json.Marshal(wsproto.ProbeAck{OK: true, LatencyMS: 17})
			out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeProbeAck, ID: env.ID, Payload: ack})
			_ = ws.Write(ctx, websocket.MessageText, out)
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(entry.ID) && s.Hub.HasCap(entry.ID, wsproto.CapProbe) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Hub.HasCap(entry.ID, wsproto.CapProbe) {
		t.Fatal("missing cap")
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 8 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	pipe := &db.Inbound{
		ServerID: entry.ID, Name: "pipe", Profile: corecfg.ProfilePortForward, Protocol: "dokodemo-door",
		Network: "tcp", Security: "none", Core: "xray", Listen: "0.0.0.0",
		Port: 10443, Enabled: true, LineKind: "direct",
		Settings: `{"dest_host":"203.0.113.8","dest_port":443,"network":"tcp"}`,
	}
	created, err := db.CreateInbound(d, pipe)
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Post(ts.URL+"/api/inbounds/"+strconv.FormatInt(created.ID, 10)+"/hop-probe", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var portOut struct {
		Hops []reachSeg `json:"hops"`
	}
	decodeRes(t, res, &portOut)
	if len(portOut.Hops) != 1 || portOut.Hops[0].Status != reachUp || portOut.Hops[0].Host != "203.0.113.8" || portOut.Hops[0].LatencyMS != 17 {
		t.Fatalf("port %+v", portOut.Hops)
	}

	before := probes.Load()
	udpBody, _ := json.Marshal(map[string]any{"host": "203.0.113.8", "port": 443, "udp": true})
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(entry.ID, 10)+"/reach", "application/json", bytes.NewReader(udpBody))
	if err != nil {
		t.Fatal(err)
	}
	var udpOut struct {
		Hops []reachSeg `json:"hops"`
	}
	decodeRes(t, res, &udpOut)
	if len(udpOut.Hops) != 1 || udpOut.Hops[0].Status != reachUDP || probes.Load() != before {
		t.Fatalf("udp reach %+v probes %d", udpOut.Hops, probes.Load())
	}

	hopIn, err := db.CreateInbound(d, &db.Inbound{
		ServerID: hopSrv.ID, Name: "ix", Profile: corecfg.ProfileVLESSReality, Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	landIn, err := db.CreateInbound(d, &db.Inbound{
		ServerID: landSrv.ID, Name: "home", Profile: corecfg.ProfileVLESSReality, Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 8443, Enabled: true, LineKind: "direct", Settings: "{}",
	})
	if err != nil {
		t.Fatal(err)
	}
	chain, err := db.CreateInbound(d, &db.Inbound{
		ServerID: entry.ID, Name: "chain", Profile: corecfg.ProfileVLESSReality, Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 18443, Enabled: true, LineKind: "chain", ExitInboundID: &landIn.ID,
		Settings: `{"hops":[{"kind":"panel","inbound_id":` + strconv.FormatInt(hopIn.ID, 10) + `}]}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Post(ts.URL+"/api/inbounds/"+strconv.FormatInt(chain.ID, 10)+"/hop-probe", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var chainOut struct {
		Hops []reachSeg `json:"hops"`
	}
	decodeRes(t, res, &chainOut)
	if len(chainOut.Hops) != 2 {
		t.Fatalf("chain len %+v", chainOut.Hops)
	}
	if chainOut.Hops[0].Status != reachUp || chainOut.Hops[0].Host != "203.0.113.20" || chainOut.Hops[0].Port != 443 {
		t.Fatalf("chain0 %+v", chainOut.Hops[0])
	}
	if chainOut.Hops[1].Status != reachSkip || chainOut.Hops[1].Error != reachOffline {
		t.Fatalf("chain1 %+v", chainOut.Hops[1])
	}

	res, err = c.Post(ts.URL+"/api/traffic-forwards/99/hop-probe", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("missing forward %d", res.StatusCode)
	}
	res.Body.Close()
}
