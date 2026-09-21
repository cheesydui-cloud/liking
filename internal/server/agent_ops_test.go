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

func TestDashboardAlertsOldAgent(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	items, strs := dashboardAlerts(d, []*db.Server{
		{Name: "jp", NeedsReinstall: true},
		{Name: "hk", NeedsUpgrade: true},
	}, []*db.User{
		{Username: "alice", Role: "user", Enabled: true, QuotaRatio: 90},
		{Username: "admin", Role: "admin", QuotaRatio: 100},
	})
	if len(items) != 3 || len(strs) != 3 {
		t.Fatalf("items %+v strs %v", items, strs)
	}
	if items[0].Kind != "warn" || items[0].To != "/servers" || !strings.Contains(items[0].Text, "太旧") {
		t.Fatalf("reinstall %+v", items[0])
	}
	if items[1].Kind != "warn" || !strings.Contains(items[1].Text, "可升级") {
		t.Fatalf("upgrade %+v", items[1])
	}
	if items[2].Kind != "warn" || items[2].To != "/users" || !strings.Contains(items[2].Text, "90%") {
		t.Fatalf("quota %+v", items[2])
	}
}

func TestOldAgentUpgradeFailsFast(t *testing.T) {
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

	hello, _ := json.Marshal(wsproto.Hello{Token: row.Token, AgentVersion: "0.1.9", OS: "linux", Arch: "amd64", Cores: []string{"xray"}})
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
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/upgrade-agent", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("upgrade status %d %s", res.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"code":"agent_too_old"`)) {
		t.Fatalf("upgrade body %s", body)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("upgrade too slow %s", time.Since(start))
	}

	start = time.Now()
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/uninstall-agent", "application/json", bytes.NewReader([]byte(`{"confirm":true}`)))
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("uninstall status %d %s", res.StatusCode, body)
	}
	if !bytes.Contains(body, []byte(`"code":"agent_too_old"`)) {
		t.Fatalf("uninstall body %s", body)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("uninstall too slow %s", time.Since(start))
	}

	res, err = c.Get(ts.URL + "/api/servers")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Servers []struct {
			NeedsReinstall bool `json:"needs_reinstall"`
			NeedsUpgrade   bool `json:"needs_upgrade"`
		} `json:"servers"`
	}
	decodeRes(t, res, &listing)
	if len(listing.Servers) != 1 || !listing.Servers[0].NeedsReinstall || !listing.Servers[0].NeedsUpgrade {
		t.Fatalf("servers %+v", listing.Servers)
	}

	res, err = c.Get(ts.URL + "/api/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	var dash struct {
		AlertItems []struct {
			Text string `json:"text"`
			To   string `json:"to"`
			Kind string `json:"kind"`
		} `json:"alert_items"`
	}
	decodeRes(t, res, &dash)
	found := false
	for _, a := range dash.AlertItems {
		if strings.Contains(a.Text, "太旧") && a.To == "/servers" && a.Kind == "warn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("alerts %+v", dash.AlertItems)
	}
}

func TestPushCoreRequiresCap(t *testing.T) {
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

	hello, _ := json.Marshal(wsproto.Hello{Token: row.Token, AgentVersion: "0.1.46", OS: "linux", Arch: "amd64", Cores: []string{"xray"}})
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
	body, _ := json.Marshal(map[string]string{"core": "singbox"})
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/push-core", "application/json", bytes.NewReader(body))
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

func TestPushCoreOK(t *testing.T) {
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
		Token: row.Token, AgentVersion: "0.1.46", OS: "linux", Arch: "amd64",
		Cores: []string{"xray"}, Caps: []string{wsproto.CapCores},
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
			if env.Type != wsproto.TypeEnsureCore && env.Type != wsproto.TypeRemoveCore {
				continue
			}
			ackType := wsproto.TypeEnsureCoreAck
			cores := []string{"xray", "singbox"}
			if env.Type == wsproto.TypeRemoveCore {
				ackType = wsproto.TypeRemoveCoreAck
				cores = []string{"xray"}
			}
			ack, _ := json.Marshal(wsproto.CoreOpAck{OK: true, Core: "singbox", Cores: cores})
			out, _ := json.Marshal(wsproto.Envelope{Type: ackType, ID: env.ID, Payload: ack})
			_ = ws.Write(ctx, websocket.MessageText, out)
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(row.ID) && s.Hub.HasCap(row.ID, wsproto.CapCores) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Hub.HasCap(row.ID, wsproto.CapCores) {
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

	body, _ := json.Marshal(map[string]string{"core": "sing-box"})
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/push-core", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var pushed struct {
		OK    bool     `json:"ok"`
		Core  string   `json:"core"`
		Cores []string `json:"cores"`
	}
	decodeRes(t, res, &pushed)
	if !pushed.OK || pushed.Core != "singbox" || len(pushed.Cores) != 2 {
		t.Fatalf("push %+v", pushed)
	}

	_, err = db.CreateInbound(d, &db.Inbound{
		ServerID: row.ID, Name: "a", Profile: "anytls", Protocol: "anytls",
		Network: "tcp", Security: "tls", Core: "singbox", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/remove-core", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("节点")) {
		t.Fatalf("blocked remove %d %s", res.StatusCode, raw)
	}
}

func TestPushNftAgentTooOld(t *testing.T) {
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
	row, err := db.CreateServer(d, "land", "1.2.3.4", "tokentokentoken")
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
		Token: row.Token, AgentVersion: "0.2.20", OS: "linux", Arch: "amd64",
		Cores: []string{"xray"}, Caps: []string{wsproto.CapCores},
	})
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

	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/push-nft", "application/json", bytes.NewReader([]byte("{}")))
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
}

func TestPushNftOK(t *testing.T) {
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
	row, err := db.CreateServer(d, "land", "1.2.3.4", "tokentokentoken")
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
		Token: row.Token, AgentVersion: "0.2.21", OS: "linux", Arch: "amd64",
		Cores: []string{"xray"}, Caps: []string{wsproto.CapCores, wsproto.CapNFT},
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
			switch env.Type {
			case wsproto.TypeEnsureNFT:
				ack, _ := json.Marshal(wsproto.EnsureNFTAck{OK: true, Have: true})
				out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeEnsureNFTAck, ID: env.ID, Payload: ack})
				_ = ws.Write(ctx, websocket.MessageText, out)
			case wsproto.TypeApply:
				ack, _ := json.Marshal(wsproto.ApplyAck{OK: true, Rev: "x"})
				out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeApplyAck, ID: env.ID, Payload: ack})
				_ = ws.Write(ctx, websocket.MessageText, out)
			}
		}
	}()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(row.ID) && s.Hub.HasCap(row.ID, wsproto.CapNFT) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Hub.HasCap(row.ID, wsproto.CapNFT) {
		t.Fatal("missing cap")
	}

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/push-nft", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var pushed struct {
		OK          bool   `json:"ok"`
		Have        bool   `json:"have"`
		ApplyError  string `json:"apply_error"`
	}
	decodeRes(t, res, &pushed)
	if !pushed.OK || !pushed.Have {
		t.Fatalf("push %+v", pushed)
	}
}
