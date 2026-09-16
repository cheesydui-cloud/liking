package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestParseDestAddr(t *testing.T) {
	host, port, ok := parseDestAddr("azure.microsoft.com")
	if !ok || host != "azure.microsoft.com" || port != 443 {
		t.Fatalf("%s %d %v", host, port, ok)
	}
	host, port, ok = parseDestAddr("j.6sc.co:443")
	if !ok || host != "j.6sc.co" || port != 443 {
		t.Fatalf("%s %d %v", host, port, ok)
	}
	host, port, ok = parseDestAddr("https://go.microsoft.com/path")
	if !ok || host != "go.microsoft.com" || port != 443 {
		t.Fatalf("%s %d %v", host, port, ok)
	}
	if _, _, ok := parseDestAddr(""); ok {
		t.Fatal("empty")
	}
	if _, _, ok := parseDestAddr(":::1"); ok {
		t.Fatal("bad")
	}
}

func TestParseURIAndChainVLESSExit(t *testing.T) {
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
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Router())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	body, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "10.0.0.1"})
	res, err = c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &created)

	uri := "vless://11111111-1111-4111-8111-111111111111@203.0.113.10:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=abc&sid=abcd&type=tcp&flow=xtls-rprx-vision#ext"
	parseBody, _ := json.Marshal(map[string]string{"uri": uri})
	res, err = c.Post(ts.URL+"/api/parse-uri", "application/json", bytes.NewReader(parseBody))
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		OK     bool `json:"ok"`
		Target struct {
			Scheme string `json:"scheme"`
			Host   string `json:"host"`
			Port   int    `json:"port"`
			Name   string `json:"name"`
			Label  string `json:"label"`
		} `json:"target"`
	}
	decodeRes(t, res, &parsed)
	if !parsed.OK || parsed.Target.Scheme != "vless" || parsed.Target.Host != "203.0.113.10" || parsed.Target.Port != 443 || parsed.Target.Name != "ext" {
		t.Fatalf("parse %+v", parsed)
	}

	inBody, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "entry-ext",
		"profile":   "vless-reality-vision",
		"port":      0,
		"line_kind": "chain",
		"exit_uri":  uri,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdIn struct {
		Inbound struct {
			ID      int64  `json:"id"`
			ExitURI string `json:"exit_uri"`
			Port    int    `json:"port"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &createdIn)
	if createdIn.Inbound.ID == 0 || createdIn.Inbound.Port < 1 || !strings.Contains(createdIn.Inbound.ExitURI, "vless://") {
		t.Fatalf("inbound %+v", createdIn.Inbound)
	}

	bad, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "bad",
		"profile":   "vless-reality-vision",
		"line_kind": "chain",
		"exit_uri":  "vmess://abc",
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("vmess")) {
		t.Fatalf("vmess %d %s", res.StatusCode, raw)
	}
}

func TestProbeOldAgentFailsFast(t *testing.T) {
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
	row, err := db.CreateServer(d, "jp", "1.2.3.4", "tokentokentoken")
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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	hello, _ := json.Marshal(wsproto.Hello{Token: row.Token, AgentVersion: "0.1.52", OS: "linux", Arch: "amd64", Cores: []string{"xray"}, Caps: []string{wsproto.CapCores}})
	b, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeHello, ID: "1", Payload: hello})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(row.ID) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Hub.IsOnline(row.ID) {
		t.Fatal("not online")
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 3 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	start := time.Now()
	body, _ := json.Marshal(map[string]any{"server_id": row.ID, "host": "203.0.113.10", "port": 443})
	res, err = c.Post(ts.URL+"/api/probe", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d %s", res.StatusCode, raw)
	}
	if !bytes.Contains(raw, []byte(`"code":"agent_too_old"`)) {
		t.Fatalf("body %s", raw)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("too slow %s", time.Since(start))
	}
}

func TestProbeOK(t *testing.T) {
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
	row, err := db.CreateServer(d, "hk", "1.2.3.4", "tokentokentoken")
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
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	hello, _ := json.Marshal(wsproto.Hello{
		Token: row.Token, AgentVersion: "0.1.53", OS: "linux", Arch: "amd64",
		Cores: []string{"xray"}, Caps: []string{wsproto.CapCores, wsproto.CapProbe},
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
			if err := json.Unmarshal(raw, &env); err != nil {
				continue
			}
			if env.Type != wsproto.TypeProbe {
				continue
			}
			ack, _ := json.Marshal(wsproto.ProbeAck{OK: true, LatencyMS: 42})
			out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeProbeAck, ID: env.ID, Payload: ack})
			_ = ws.Write(ctx, websocket.MessageText, out)
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(row.ID) && s.Hub.HasCap(row.ID, wsproto.CapProbe) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Hub.HasCap(row.ID, wsproto.CapProbe) {
		t.Fatal("missing cap")
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 3 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	body, _ := json.Marshal(map[string]any{"server_id": row.ID, "host": "203.0.113.10", "port": 443})
	res, err = c.Post(ts.URL+"/api/probe", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		OK        bool   `json:"ok"`
		LatencyMS int64  `json:"latency_ms"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
	}
	decodeRes(t, res, &out)
	if !out.OK || out.LatencyMS != 42 || out.Host != "203.0.113.10" || out.Port != 443 {
		t.Fatalf("probe %+v", out)
	}

	uriBody, _ := json.Marshal(map[string]any{
		"server_id": row.ID,
		"uri":       "vless://11111111-1111-4111-8111-111111111111@9.9.9.9:8443?security=reality&pbk=x&type=tcp",
	})
	res, err = c.Post(ts.URL+"/api/probe", "application/json", bytes.NewReader(uriBody))
	if err != nil {
		t.Fatal(err)
	}
	var outURI struct {
		OK   bool   `json:"ok"`
		Host string `json:"host"`
		Port int    `json:"port"`
	}
	decodeRes(t, res, &outURI)
	if !outURI.OK || outURI.Host != "9.9.9.9" || outURI.Port != 8443 {
		t.Fatalf("uri probe %+v", outURI)
	}

	destBody, _ := json.Marshal(map[string]any{
		"dests": []string{"azure.microsoft.com", "j.6sc.co:443", "azure.microsoft.com"},
	})
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/dest-probe", "application/json", bytes.NewReader(destBody))
	if err != nil {
		t.Fatal(err)
	}
	var destOut struct {
		Results []struct {
			Dest      string `json:"dest"`
			Host      string `json:"host"`
			Port      int    `json:"port"`
			OK        bool   `json:"ok"`
			LatencyMS int64  `json:"latency_ms"`
		} `json:"results"`
	}
	decodeRes(t, res, &destOut)
	if len(destOut.Results) != 2 {
		t.Fatalf("dedupe %+v", destOut.Results)
	}
	if destOut.Results[0].Dest != "azure.microsoft.com" || destOut.Results[0].Host != "azure.microsoft.com" || destOut.Results[0].Port != 443 || !destOut.Results[0].OK || destOut.Results[0].LatencyMS != 42 {
		t.Fatalf("dest0 %+v", destOut.Results[0])
	}
	if destOut.Results[1].Dest != "j.6sc.co:443" || destOut.Results[1].Port != 443 || !destOut.Results[1].OK {
		t.Fatalf("dest1 %+v", destOut.Results[1])
	}

	in := &db.Inbound{
		ServerID: row.ID, Name: "pipe", Profile: "port-forward", Protocol: "dokodemo-door",
		Network: "tcp", Security: "none", Core: "xray", Listen: "0.0.0.0",
		Port: 10443, Enabled: true, LineKind: "direct",
		Settings: `{"dest_host":"8.8.8.8","dest_port":443,"network":"tcp"}`,
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Post(ts.URL+"/api/inbounds/"+strconv.FormatInt(created.ID, 10)+"/probe", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var out2 struct {
		OK        bool   `json:"ok"`
		LatencyMS int64  `json:"latency_ms"`
		Host      string `json:"host"`
	}
	decodeRes(t, res, &out2)
	if !out2.OK || out2.LatencyMS != 42 || out2.Host != "8.8.8.8" {
		t.Fatalf("inbound probe %+v", out2)
	}
}
