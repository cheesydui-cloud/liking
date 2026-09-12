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
		{Username: "alice", Role: "user", QuotaRatio: 90},
		{Username: "admin", Role: "admin", QuotaRatio: 100},
	})
	if len(items) != 3 || len(strs) != 3 {
		t.Fatalf("items %+v strs %v", items, strs)
	}
	if items[0].Kind != "warn" || items[0].To != "/nodes" || !strings.Contains(items[0].Text, "太旧") {
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
		if strings.Contains(a.Text, "太旧") && a.To == "/nodes" && a.Kind == "warn" {
			found = true
		}
	}
	if !found {
		t.Fatalf("alerts %+v", dash.AlertItems)
	}
}
