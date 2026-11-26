package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"database/sql"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestTimeoutIsNotRetried(t *testing.T) {
	if transientApplyErr(errors.New("等待 Agent 回复超时")) {
		t.Fatal("ack timeout must not be retried")
	}
	if !transientApplyErr(errors.New("连接在回复前断开")) {
		t.Fatal("disconnect should still retry")
	}
	if !transientApplyErr(errors.New("server 3 not connected")) {
		t.Fatal("not connected should still retry")
	}
}

func TestSaveDoesNotWaitForSilentAgent(t *testing.T) {
	d, srv, ts, c := applyWaitPanel(t)
	row, err := db.CreateServer(d, "silent", "203.0.113.80", "token-silent-apply")
	if err != nil {
		t.Fatal(err)
	}
	startSilentAgent(t, srv, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/agents", row.Token)

	body, _ := json.Marshal(map[string]any{
		"server_id": row.ID,
		"name":      "vless-silent",
		"profile":   "vless-reality-vision",
		"port":      8443,
	})
	start := time.Now()
	res, err := c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(body))
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed > 6*time.Second {
		t.Fatalf("save blocked for %s", elapsed)
	}
	var out struct {
		Apply      string `json:"apply"`
		ApplyError string `json:"apply_error"`
	}
	decodeRes(t, res, &out)
	if out.Apply != "queued" || out.ApplyError != "" {
		t.Fatalf("apply=%q err=%q after %s", out.Apply, out.ApplyError, elapsed)
	}
}

func TestSaveReportsFastApplyError(t *testing.T) {
	d, srv, ts, c := applyWaitPanel(t)
	row, err := db.CreateServer(d, "reject", "203.0.113.81", "token-reject-apply")
	if err != nil {
		t.Fatal(err)
	}
	startSyncAgent(t, srv, "ws"+strings.TrimPrefix(ts.URL, "http")+"/v1/agents", row.Token, false)

	body, _ := json.Marshal(map[string]any{
		"server_id": row.ID,
		"name":      "vless-reject",
		"profile":   "vless-reality-vision",
		"port":      8444,
	})
	start := time.Now()
	res, err := c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatalf("fast reject took %s", time.Since(start))
	}
	var out struct {
		Apply      string `json:"apply"`
		ApplyError string `json:"apply_error"`
	}
	decodeRes(t, res, &out)
	if out.Apply == "queued" || !strings.Contains(out.ApplyError, "拒绝下发") {
		t.Fatalf("apply=%q err=%q", out.Apply, out.ApplyError)
	}
}

func TestOfflineSaveHidesStaleApplyError(t *testing.T) {
	d, _, ts, c := applyWaitPanel(t)
	row, err := db.CreateServer(d, "offline", "203.0.113.82", "token-offline-apply")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetServerApplyError(d, row.ID, errors.New("上次失败")); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE servers SET online=0 WHERE id=?`, row.ID); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{
		"server_id": row.ID,
		"name":      "vless-off",
		"profile":   "vless-reality-vision",
		"port":      8445,
	})
	start := time.Now()
	res, err := c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 6*time.Second {
		t.Fatalf("offline save took %s", time.Since(start))
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d %s", res.StatusCode, raw)
	}
	var out struct {
		Apply      string `json:"apply"`
		ApplyError string `json:"apply_error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Apply == "queued" || out.ApplyError != "" || strings.Contains(string(raw), "上次失败") {
		t.Fatalf("stale error leaked %s", raw)
	}
}

func applyWaitPanel(t *testing.T) (*sql.DB, *Server, *httptest.Server, *http.Client) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
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
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 8 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	return d, srv, ts, c
}

func startSilentAgent(t *testing.T, s *Server, wsURL, token string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close(websocket.StatusNormalClosure, "") })
	hello, _ := json.Marshal(wsproto.Hello{Token: token, AgentVersion: "0.3.55", OS: "linux", Arch: "amd64"})
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
	t.Fatal("silent agent not online")
}
