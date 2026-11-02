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
