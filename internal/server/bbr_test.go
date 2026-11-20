package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestTuneBBRAgentTooOld(t *testing.T) {
	d, row, s, ts, c := wgPanel(t)
	defer d.Close()
	defer s.Close()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := wgHello(ctx, ts.URL, row.Token, []string{wsproto.CapTime}, nil); err != nil {
		t.Fatal(err)
	}
	waitCap(t, s, row.ID, wsproto.CapTime)
	res, err := c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/tune-bbr", "application/json", bytes.NewReader([]byte(`{"profile":"bbr"}`)))
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !bytes.Contains(raw, []byte("agent_too_old")) {
		t.Fatalf("old %d %s", res.StatusCode, raw)
	}
}

func TestTuneBBRFillsCard(t *testing.T) {
	d, row, s, ts, c := wgPanel(t)
	defer d.Close()
	defer s.Close()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := wgHello(ctx, ts.URL, row.Token, []string{wsproto.CapBBR}, func(env wsproto.Envelope) []byte {
		if env.Type != wsproto.TypeTuneBBR {
			return nil
		}
		p, _ := json.Marshal(wsproto.TuneBBRAck{OK: true, Congestion: "bbr", Qdisc: "fq", Changed: true})
		out, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeTuneBBRAck, ID: env.ID, Payload: p})
		return out
	}); err != nil {
		t.Fatal(err)
	}
	waitCap(t, s, row.ID, wsproto.CapBBR)
	res, err := c.Post(ts.URL+"/api/servers/"+strconv.FormatInt(row.ID, 10)+"/tune-bbr", "application/json", bytes.NewReader([]byte(`{"profile":"bbr"}`)))
	if err != nil {
		t.Fatal(err)
	}
	var ack struct {
		OK         bool   `json:"ok"`
		Congestion string `json:"congestion"`
		Qdisc      string `json:"qdisc"`
		Changed    bool   `json:"changed"`
	}
	decodeRes(t, res, &ack)
	if !ack.OK || !ack.Changed || ack.Congestion != "bbr" || ack.Qdisc != "fq" {
		t.Fatalf("ack %+v", ack)
	}
	got := serverCard(t, c, ts.URL, row.ID)
	if !got.CanTuneBBR || !got.HasTCP || got.Congestion != "bbr" || got.Qdisc != "fq" {
		t.Fatalf("card %+v", got)
	}
}

func TestStatsFillBBRCard(t *testing.T) {
	d, row, s, ts, c := wgPanel(t)
	defer d.Close()
	defer s.Close()
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/agents"
	ws, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close(websocket.StatusNormalClosure, "")
	hello, _ := json.Marshal(wsproto.Hello{
		Token: row.Token, AgentVersion: "0.3.46", OS: "linux", Arch: "amd64", Caps: []string{wsproto.CapBBR},
	})
	hb, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeHello, ID: "stats", Payload: hello})
	if err := ws.Write(ctx, websocket.MessageText, hb); err != nil {
		t.Fatal(err)
	}
	if _, _, err := ws.Read(ctx); err != nil {
		t.Fatal(err)
	}
	waitCap(t, s, row.ID, wsproto.CapBBR)
	st, _ := json.Marshal(wsproto.Stats{HasTCP: true, Congestion: "bbr", Qdisc: "fq"})
	sb, _ := json.Marshal(wsproto.Envelope{Type: wsproto.TypeStats, Payload: st})
	if err := ws.Write(ctx, websocket.MessageText, sb); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		if cc, qd, ok := s.Hub.TCP(row.ID); ok && cc == "bbr" && qd == "fq" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("stats did not fill tcp")
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := serverCard(t, c, ts.URL, row.ID)
	if got.Online != 1 || !got.HasTCP || got.Congestion != "bbr" || got.Qdisc != "fq" {
		t.Fatalf("stats card %+v", got)
	}
}

func serverCard(t *testing.T, c *http.Client, base string, id int64) db.Server {
	t.Helper()
	res, err := c.Get(base + "/api/servers")
	if err != nil {
		t.Fatal(err)
	}
	var body struct {
		Servers []db.Server `json:"servers"`
	}
	decodeRes(t, res, &body)
	for _, x := range body.Servers {
		if x.ID == id {
			return x
		}
	}
	t.Fatalf("server %d missing", id)
	return db.Server{}
}
