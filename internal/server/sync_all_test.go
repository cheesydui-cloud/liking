package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestSyncAllSkipsOfflineAndReportsFailures(t *testing.T) {
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
	ok1, err := db.CreateServer(d, "ok-1", "203.0.113.1", "token-sync-ok-1")
	if err != nil {
		t.Fatal(err)
	}
	ok2, err := db.CreateServer(d, "ok-2", "203.0.113.2", "token-sync-ok-2")
	if err != nil {
		t.Fatal(err)
	}
	bad, err := db.CreateServer(d, "reject", "203.0.113.3", "token-sync-bad1")
	if err != nil {
		t.Fatal(err)
	}
	off, err := db.CreateServer(d, "offline", "203.0.113.4", "token-sync-off1")
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

	startSyncAgent(t, s, wsURL, ok1.Token, true)
	startSyncAgent(t, s, wsURL, ok2.Token, true)
	startSyncAgent(t, s, wsURL, bad.Token, false)

	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 20 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Post(ts.URL+"/api/servers/sync-all", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Synced  int `json:"synced"`
		Failed  int `json:"failed"`
		Skipped int `json:"skipped"`
		Results []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
			Error  string `json:"error"`
			Code   string `json:"code"`
		} `json:"results"`
	}
	decodeRes(t, res, &out)
	if out.Synced != 2 || out.Failed != 1 || out.Skipped != 1 {
		t.Fatalf("counts synced=%d failed=%d skipped=%d %+v", out.Synced, out.Failed, out.Skipped, out.Results)
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
	if byName["ok-1"].Status != "synced" || byName["ok-2"].Status != "synced" {
		t.Fatalf("online %+v %+v", byName["ok-1"], byName["ok-2"])
	}
	if byName["reject"].Status != "failed" || !strings.Contains(byName["reject"].Error, "拒绝下发") {
		t.Fatalf("reject %+v", byName["reject"])
	}
	if byName["offline"].Status != "skipped" || byName["offline"].Code != "offline" {
		t.Fatalf("offline %+v", byName["offline"])
	}
	cur, err := db.GetServer(d, off.ID)
	if err != nil {
		t.Fatal(err)
	}
	if cur.ConfigRev != "" {
		t.Fatalf("offline rev %q", cur.ConfigRev)
	}
}

func startSyncAgent(t *testing.T, s *Server, wsURL, token string, accept bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "") })
	hello, _ := json.Marshal(wsproto.Hello{Token: token, AgentVersion: "0.3.48", OS: "linux", Arch: "amd64"})
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
			if json.Unmarshal(data, &env) != nil || env.Type != wsproto.TypeApply {
				continue
			}
			ack := wsproto.ApplyAck{OK: accept, Rev: "test"}
			if !accept {
				ack.Error = "拒绝下发"
			}
			p, _ := json.Marshal(ack)
			out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeApplyAck, ID: env.ID, Payload: p})
			_ = ws.Write(ctx, websocket.MessageText, out)
		}
	}()
	row, err := db.GetServerByToken(s.DB, token)
	if err != nil || row == nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(row.ID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("agent not online")
}
