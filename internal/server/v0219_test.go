package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
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

func v0219login(t *testing.T, ts *httptest.Server, user, pass string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	return c
}

func TestPackagePolicyCopiesOnCreateNotOnEdit(t *testing.T) {
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
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	c := v0219login(t, ts, "admin", "secret12")

	sbody, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "10.0.0.1"})
	res, err := c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(sbody))
	if err != nil {
		t.Fatal(err)
	}
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)
	inBody, _ := json.Marshal(map[string]any{
		"server_id": createdSrv.Server.ID, "name": "vless-1", "profile": "vless-reality-vision", "port": 8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	pkgBody, _ := json.Marshal(map[string]any{
		"name":                 "vip",
		"traffic_bytes":        0,
		"cycle_days":           30,
		"direction":            "oneway",
		"server_ids":           []int64{createdSrv.Server.ID},
		"speed_limit":          20,
		"sub_rule_preset":      "minimal",
		"site_filter_mode":     "deny",
		"site_deny_categories": []string{"google"},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Package struct {
			ID                 int64    `json:"id"`
			SpeedLimit         int64    `json:"speed_limit"`
			SubRulePreset      string   `json:"sub_rule_preset"`
			SiteFilterMode     string   `json:"site_filter_mode"`
			SiteDenyCategories []string `json:"site_deny_categories"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg)
	if pkg.Package.SpeedLimit != 20 || pkg.Package.SubRulePreset != "minimal" || pkg.Package.SiteFilterMode != "deny" || len(pkg.Package.SiteDenyCategories) != 1 {
		t.Fatalf("pkg %+v", pkg.Package)
	}

	uBody, _ := json.Marshal(map[string]any{
		"username": "alice", "password": "alice12", "package_id": pkg.Package.ID, "days": 30,
	})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		User struct {
			ID                 int64    `json:"id"`
			SpeedLimit         int64    `json:"speed_limit"`
			SubRulePreset      string   `json:"sub_rule_preset"`
			SiteFilterMode     string   `json:"site_filter_mode"`
			SiteDenyCategories []string `json:"site_deny_categories"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if created.User.SpeedLimit != 20 || created.User.SubRulePreset != "minimal" || created.User.SiteFilterMode != "deny" {
		t.Fatalf("copied %+v", created.User)
	}

	editPkg, _ := json.Marshal(map[string]any{
		"name": "vip", "speed_limit": 80, "site_filter_mode": "allow", "site_deny_categories": []string{"tiktok"},
	})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/packages/"+strconv.FormatInt(pkg.Package.ID, 10), bytes.NewReader(editPkg))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Users []struct {
			ID         int64  `json:"id"`
			SpeedLimit int64  `json:"speed_limit"`
			SiteFilter string `json:"site_filter_mode"`
		} `json:"users"`
	}
	decodeRes(t, res, &listing)
	var alice *struct {
		ID         int64  `json:"id"`
		SpeedLimit int64  `json:"speed_limit"`
		SiteFilter string `json:"site_filter_mode"`
	}
	for i := range listing.Users {
		if listing.Users[i].ID == created.User.ID {
			alice = &listing.Users[i]
			break
		}
	}
	if alice == nil || alice.SpeedLimit != 20 || alice.SiteFilter != "deny" {
		t.Fatalf("edit package rewrote user %+v", alice)
	}

	pkg2Body, _ := json.Marshal(map[string]any{
		"name": "lite", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway",
		"server_ids": []int64{createdSrv.Server.ID}, "speed_limit": 30,
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkg2Body))
	if err != nil {
		t.Fatal(err)
	}
	var pkg2 struct {
		Package struct {
			ID int64 `json:"id"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg2)

	bind, _ := json.Marshal(map[string]any{"package_id": pkg2.Package.ID})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(bind))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var rebound struct {
		User struct {
			SpeedLimit     int64  `json:"speed_limit"`
			SiteFilterMode string `json:"site_filter_mode"`
		} `json:"user"`
	}
	decodeRes(t, res, &rebound)
	if rebound.User.SpeedLimit != 30 {
		t.Fatalf("rebind speed %+v", rebound.User)
	}

	keepSpeed, _ := json.Marshal(map[string]any{"package_id": pkg.Package.ID, "speed_limit": 5})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(keepSpeed))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var kept struct {
		User struct {
			SpeedLimit int64 `json:"speed_limit"`
		} `json:"user"`
	}
	decodeRes(t, res, &kept)
	if kept.User.SpeedLimit != 5 {
		t.Fatalf("form speed should win %+v", kept.User)
	}

	bad, _ := json.Marshal(map[string]any{
		"name": "bad", "server_ids": []int64{createdSrv.Server.ID}, "speed_limit": 10001,
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == 200 {
		t.Fatal("speed 10001")
	}
	io.ReadAll(res.Body)
	res.Body.Close()
}

func TestRotateMeSubAndBatchUsers(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	hash, err := HashPassword("secret12")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := db.CreateUser(d, "admin", hash, "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	c := v0219login(t, ts, "admin", "secret12")

	sbody, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "10.0.0.1"})
	res, err := c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(sbody))
	if err != nil {
		t.Fatal(err)
	}
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)
	inBody, _ := json.Marshal(map[string]any{
		"server_id": createdSrv.Server.ID, "name": "vless-1", "profile": "vless-reality-vision", "port": 8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	pkgBody, _ := json.Marshal(map[string]any{
		"name": "std", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway",
		"server_ids": []int64{createdSrv.Server.ID},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Package struct {
			ID int64 `json:"id"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg)

	makeUser := func(name string) (id int64, token string) {
		t.Helper()
		uBody, _ := json.Marshal(map[string]any{
			"username": name, "password": name + "pass12", "package_id": pkg.Package.ID, "days": 1,
		})
		res, err := c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
		if err != nil {
			t.Fatal(err)
		}
		var created struct {
			User struct {
				ID       int64  `json:"id"`
				SubToken string `json:"sub_token"`
			} `json:"user"`
		}
		decodeRes(t, res, &created)
		return created.User.ID, created.User.SubToken
	}
	aliceID, aliceTok := makeUser("alice")
	bobID, _ := makeUser("bob")
	if err := db.AddUserTraffic(d, aliceID, 10, 20); err != nil {
		t.Fatal(err)
	}

	uc := v0219login(t, ts, "alice", "alicepass12")
	res, err = uc.Post(ts.URL+"/api/me/rotate-sub", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var rotated struct {
		SubToken string `json:"sub_token"`
	}
	decodeRes(t, res, &rotated)
	if rotated.SubToken == "" || rotated.SubToken == aliceTok {
		t.Fatalf("rotate %q old %q", rotated.SubToken, aliceTok)
	}
	old, err := http.Get(ts.URL + "/api/sub/" + aliceTok + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	io.ReadAll(old.Body)
	old.Body.Close()
	if old.StatusCode == 200 {
		t.Fatal("old token still works")
	}
	fresh, err := http.Get(ts.URL + "/api/sub/" + rotated.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, fresh, nil)

	batch, _ := json.Marshal(map[string]any{
		"ids": []int64{aliceID, bobID, admin.ID}, "action": "reset_traffic",
	})
	res, err = c.Post(ts.URL+"/api/users/batch", "application/json", bytes.NewReader(batch))
	if err != nil {
		t.Fatal(err)
	}
	var bout struct {
		OK    int `json:"ok"`
		Total int `json:"total"`
		Users []struct {
			ID    int64  `json:"id"`
			OK    bool   `json:"ok"`
			Error string `json:"error"`
		} `json:"users"`
	}
	decodeRes(t, res, &bout)
	if bout.OK != 2 || bout.Total != 3 {
		t.Fatalf("batch %+v", bout)
	}
	alice, err := db.GetUser(d, aliceID)
	if err != nil {
		t.Fatal(err)
	}
	if alice.UsedUp != 0 || alice.UsedDown != 0 {
		t.Fatalf("traffic %+v", alice)
	}

	dis, _ := json.Marshal(map[string]any{"ids": []int64{bobID}, "action": "disable"})
	res, err = c.Post(ts.URL+"/api/users/batch", "application/json", bytes.NewReader(dis))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	bob, err := db.GetUser(d, bobID)
	if err != nil {
		t.Fatal(err)
	}
	if bob.Enabled {
		t.Fatal("bob still enabled")
	}

	ext, _ := json.Marshal(map[string]any{"ids": []int64{aliceID}, "action": "extend", "days": 30})
	res, err = c.Post(ts.URL+"/api/users/batch", "application/json", bytes.NewReader(ext))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	alice, _ = db.GetUser(d, aliceID)
	if alice.ExpiresAt < time.Now().Add(20*24*time.Hour).Unix() {
		t.Fatalf("extend %d", alice.ExpiresAt)
	}
}

func TestListenProbeDialsShareHost(t *testing.T) {
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
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	c := v0219login(t, ts, "admin", "secret12")

	sbody, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "203.0.113.9"})
	res, err := c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(sbody))
	if err != nil {
		t.Fatal(err)
	}
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)
	inBody, _ := json.Marshal(map[string]any{
		"server_id": createdSrv.Server.ID, "name": "vless-1", "profile": "vless-reality-vision", "port": 8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdIn struct {
		Inbound struct {
			ID   int64 `json:"id"`
			Port int   `json:"port"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &createdIn)

	old := listenDialTimeout
	defer func() { listenDialTimeout = old }()
	var gotAddr string
	listenDialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
		gotAddr = address
		if network != "tcp" {
			t.Fatalf("net %s", network)
		}
		a, b := net.Pipe()
		_ = b.Close()
		return a, nil
	}
	res, err = c.Post(ts.URL+"/api/inbounds/"+strconv.FormatInt(createdIn.Inbound.ID, 10)+"/listen-probe", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		OK        bool   `json:"ok"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		LatencyMS int64  `json:"latency_ms"`
	}
	decodeRes(t, res, &out)
	if !out.OK || out.Host != "203.0.113.9" || out.Port != createdIn.Inbound.Port || out.LatencyMS < 1 {
		t.Fatalf("%+v addr %s", out, gotAddr)
	}
	if !strings.Contains(gotAddr, "203.0.113.9") {
		t.Fatalf("dialed %s", gotAddr)
	}

	listenDialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}
	res, err = c.Post(ts.URL+"/api/inbounds/"+strconv.FormatInt(createdIn.Inbound.ID, 10)+"/listen-probe", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var fail struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	decodeRes(t, res, &fail)
	if fail.OK || fail.Error == "" {
		t.Fatalf("fail %+v", fail)
	}
}

func TestKickOldAgentFailsFast(t *testing.T) {
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

	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: row.ID, Name: "a", Profile: "vless-reality", Protocol: "vless",
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
	if err := db.UpsertClient(d, &db.Client{
		InboundID: in.ID, UserID: u.ID, Email: db.EmailFor(u.ID, in.ID), Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	hello, _ := json.Marshal(wsproto.Hello{Token: row.Token, AgentVersion: "0.2.18", OS: "linux", Arch: "amd64", Cores: []string{"xray"}, Caps: []string{wsproto.CapCores}})
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

	c := v0219login(t, ts, "admin", "secret12")
	start := time.Now()
	res, err := c.Post(ts.URL+"/api/users/"+strconv.FormatInt(u.ID, 10)+"/kick", "application/json", bytes.NewReader([]byte("{}")))
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
