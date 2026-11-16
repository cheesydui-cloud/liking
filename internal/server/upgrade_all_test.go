package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"liking/internal/db"
	"liking/internal/version"
	"liking/internal/wsproto"
)

func TestUpgradeAllSkipsAndUpgradesTogether(t *testing.T) {
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
	old, err := db.CreateServer(d, "too-old", "203.0.113.1", "token-too-old-1")
	if err != nil {
		t.Fatal(err)
	}
	current, err := db.CreateServer(d, "current", "203.0.113.2", "token-current-1")
	if err != nil {
		t.Fatal(err)
	}
	ready, err := db.CreateServer(d, "ready", "203.0.113.3", "token-ready-001")
	if err != nil {
		t.Fatal(err)
	}
	arm, err := db.CreateServer(d, "arm-missing", "203.0.113.4", "token-arm-00001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateServer(d, "offline", "203.0.113.5", "token-offline-1"); err != nil {
		t.Fatal(err)
	}

	bin := filepath.Join(t.TempDir(), "liking-agent-linux-amd64")
	if err := os.WriteFile(bin, []byte("agent "+version.VersionMarker), 0o644); err != nil {
		t.Fatal(err)
	}
	prevLocate := locateAgentBinary
	locateAgentBinary = func(goos, arch string) string {
		if goos == "linux" && arch == "amd64" {
			return bin
		}
		return ""
	}
	t.Cleanup(func() { locateAgentBinary = prevLocate })

	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ts := httptest.NewServer(s.Router())
	defer ts.Close()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents"

	startFakeAgent(t, s, wsURL, old.Token, "0.1.9", "linux", "amd64")
	startFakeAgent(t, s, wsURL, current.Token, version.Version, "linux", "amd64")
	startFakeAgent(t, s, wsURL, ready.Token, "0.3.39", "linux", "amd64")
	startFakeAgent(t, s, wsURL, arm.Token, "0.3.39", "linux", "arm64")

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 15 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Post(ts.URL+"/api/servers/upgrade-agents", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Upgraded int `json:"upgraded"`
		Failed   int `json:"failed"`
		Skipped  int `json:"skipped"`
		Results  []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error"`
			Code   string `json:"code"`
		} `json:"results"`
	}
	decodeRes(t, res, &out)
	if out.Upgraded != 1 || out.Failed != 1 || out.Skipped != 3 {
		t.Fatalf("counts upgraded=%d failed=%d skipped=%d %+v", out.Upgraded, out.Failed, out.Skipped, out.Results)
	}
	byName := map[string]struct {
		Status string
		Error  string
		Code   string
	}{}
	for _, row := range out.Results {
		byName[row.Name] = struct {
			Status string
			Error  string
			Code   string
		}{row.Status, row.Error, row.Code}
	}
	if byName["ready"].Status != "upgraded" {
		t.Fatalf("ready %+v", byName["ready"])
	}
	if byName["arm-missing"].Status != "failed" || !strings.Contains(byName["arm-missing"].Error, "没有对应架构") {
		t.Fatalf("arm %+v", byName["arm-missing"])
	}
	if byName["too-old"].Status != "skipped" || byName["too-old"].Code != "agent_too_old" {
		t.Fatalf("too-old %+v", byName["too-old"])
	}
	if byName["current"].Status != "skipped" || byName["current"].Code != "current" {
		t.Fatalf("current %+v", byName["current"])
	}
	if byName["offline"].Status != "skipped" || byName["offline"].Code != "offline" {
		t.Fatalf("offline %+v", byName["offline"])
	}
	rows, err := d.Query(`SELECT detail FROM audit WHERE action='agent.upgrade'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var detail string
		if err := rows.Scan(&detail); err != nil {
			t.Fatal(err)
		}
		n++
		if !strings.Contains(detail, "ready -> "+version.Version) {
			t.Fatalf("audit %q", detail)
		}
	}
	if n != 1 {
		t.Fatalf("audit rows %d", n)
	}
}

func startFakeAgent(t *testing.T, s *Server, wsURL, token, ver, osName, arch string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "") })
	hello, _ := json.Marshal(wsproto.Hello{Token: token, AgentVersion: ver, OS: osName, Arch: arch})
	b, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeHello, ID: "1", Payload: hello})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			_, data, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var env wsproto.Envelope
			if json.Unmarshal(data, &env) != nil || env.Type != wsproto.TypeUpgrade {
				continue
			}
			p, _ := json.Marshal(wsproto.UpgradeAck{OK: true, Version: version.Version})
			ack, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeUpgradeAck, ID: env.ID, Payload: p})
			_ = ws.Write(ctx, websocket.MessageText, ack)
		}
	}()
	row, err := db.GetServerByToken(s.DB, token)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		cur, err := db.GetServer(s.DB, row.ID)
		if err == nil && cur != nil && s.Hub.IsOnline(row.ID) && cur.AgentVer == ver {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("agent not online")
}
