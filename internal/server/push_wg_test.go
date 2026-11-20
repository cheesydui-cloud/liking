package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
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

func TestPushWireGuard(t *testing.T) {
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
	row, err := db.CreateServer(d, "transit", "203.0.113.8", "tokentokentoken")
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
		Token: row.Token, AgentVersion: "0.3.5", OS: "linux", Arch: "amd64",
		Caps: []string{wsproto.CapWG},
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
			case wsproto.TypeEnsureWG:
				ack, _ := json.Marshal(wsproto.EnsureWGAck{OK: true, Have: true})
				out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeEnsureWGAck, ID: env.ID, Payload: ack})
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
		if s.Hub.IsOnline(row.ID) && s.Hub.HasCap(row.ID, wsproto.CapWG) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !s.Hub.HasCap(row.ID, wsproto.CapWG) {
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

	res, err = c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/push-wg", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var pushed struct {
		OK         bool   `json:"ok"`
		Have       bool   `json:"have"`
		ApplyError string `json:"apply_error"`
	}
	decodeRes(t, res, &pushed)
	if !pushed.OK || !pushed.Have || pushed.ApplyError != "" {
		t.Fatalf("push %+v", pushed)
	}
}

func TestRemoveWireGuard(t *testing.T) {
	d, row, s, ts, c := wgPanel(t)
	defer d.Close()
	defer s.Close()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := wgHello(ctx, ts.URL, row.Token, []string{wsproto.CapWG, wsproto.CapWGRemove}, func(env wsproto.Envelope) []byte {
		if env.Type != wsproto.TypeRemoveWG {
			return nil
		}
		ack, _ := json.Marshal(wsproto.RemoveWGAck{OK: true, Have: false, Cores: []string{"xray"}})
		out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeRemoveWGAck, ID: env.ID, Payload: ack})
		return out
	}); err != nil {
		t.Fatal(err)
	}
	waitCap(t, s, row.ID, wsproto.CapWGRemove)

	res, err := c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/remove-wg", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	var removed struct {
		OK    bool     `json:"ok"`
		Have  bool     `json:"have"`
		Cores []string `json:"cores"`
	}
	decodeRes(t, res, &removed)
	if !removed.OK || removed.Have || len(removed.Cores) != 1 || removed.Cores[0] != "xray" {
		t.Fatalf("remove %+v", removed)
	}
	got, err := db.GetServer(d, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Cores != "xray" {
		t.Fatalf("cores %q", got.Cores)
	}
}

func TestRemoveWireGuardBlocked(t *testing.T) {
	d, row, s, ts, c := wgPanel(t)
	defer d.Close()
	defer s.Close()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := wgHello(ctx, ts.URL, row.Token, []string{wsproto.CapWGRemove}, nil); err != nil {
		t.Fatal(err)
	}
	waitCap(t, s, row.ID, wsproto.CapWGRemove)
	if _, err := db.CreateInbound(d, &db.Inbound{
		ServerID: row.ID, Name: "家宽", Profile: "wireguard", Protocol: "wireguard",
		Network: "udp", Security: "none", Core: "wireguard", Listen: "0.0.0.0",
		Port: 51820, Enabled: true, Settings: "{}", LineKind: "direct",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/remove-wg", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("先删节点")) {
		t.Fatalf("blocked %d %s", res.StatusCode, raw)
	}

	home, err := db.CreateServer(d, "home", "203.0.113.9", "hometokenhometoken")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, &db.Inbound{
		ServerID: row.ID, Name: "出口", Profile: "wireguard", Protocol: "wireguard",
		Network: "udp", Security: "none", Core: "wireguard", Listen: "0.0.0.0",
		Port: 51821, Enabled: true, Settings: fmt.Sprintf(`{"exit_server_id":%d}`, home.ID), LineKind: "direct",
	}); err != nil {
		t.Fatal(err)
	}
	if msg := wgUninstallBlocked(d, home.ID); !strings.Contains(msg, "家宽出口") {
		t.Fatalf("exit block %q", msg)
	}
}

func TestRemoveWireGuardAgentTooOld(t *testing.T) {
	d, row, s, ts, c := wgPanel(t)
	defer d.Close()
	defer s.Close()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := wgHello(ctx, ts.URL, row.Token, []string{wsproto.CapWG}, nil); err != nil {
		t.Fatal(err)
	}
	waitCap(t, s, row.ID, wsproto.CapWG)
	res, err := c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/remove-wg", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("agent_too_old")) {
		t.Fatalf("old %d %s", res.StatusCode, raw)
	}
}

func wgPanel(t *testing.T) (*sql.DB, *db.Server, *Server, *httptest.Server, *http.Client) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	hash, err := HashPassword("secret12")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(d, "admin", hash, "admin", ""); err != nil {
		t.Fatal(err)
	}
	row, err := db.CreateServer(d, "transit", "203.0.113.8", "tokentokentoken")
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Router())
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar, Timeout: 5 * time.Second}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	return d, row, s, ts, c
}

func wgHello(ctx context.Context, base, token string, caps []string, reply func(wsproto.Envelope) []byte) error {
	wsURL := "ws" + strings.TrimPrefix(base, "http") + "/v1/agents"
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		return err
	}
	hello, _ := json.Marshal(wsproto.Hello{
		Token: token, AgentVersion: "0.3.44", OS: "linux", Arch: "amd64", Caps: caps,
	})
	b, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeHello, ID: "1", Payload: hello})
	if err := ws.Write(ctx, websocket.MessageText, b); err != nil {
		return err
	}
	if _, _, err := ws.Read(ctx); err != nil {
		return err
	}
	go func() {
		defer ws.Close(websocket.StatusNormalClosure, "")
		for {
			_, raw, err := ws.Read(ctx)
			if err != nil {
				return
			}
			var env wsproto.Envelope
			if json.Unmarshal(raw, &env) != nil || reply == nil {
				continue
			}
			if out := reply(env); len(out) > 0 {
				_ = ws.Write(ctx, websocket.MessageText, out)
			}
		}
	}()
	return nil
}

func waitCap(t *testing.T, s *Server, id int64, cap string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Hub.IsOnline(id) && s.Hub.HasCap(id, cap) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("missing cap")
}
