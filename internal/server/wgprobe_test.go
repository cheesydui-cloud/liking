package server

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func TestWireGuardListenProbeUsesHandshake(t *testing.T) {
	in := &db.Inbound{
		Name: "ces", Profile: corecfg.ProfileWireGuard, Port: 10433, LineKind: "direct",
		ServerHost: "203.0.113.8", Settings: "{}",
	}
	if err := corecfg.Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	st := corecfg.ParseSettings(in.Settings)
	old := wgProbeExchange
	defer func() { wgProbeExchange = old }()
	var gotAddr string
	var gotLen int
	wgProbeExchange = func(addr string, payload []byte, timeout time.Duration) ([]byte, error) {
		gotAddr = addr
		gotLen = len(payload)
		if len(payload) != 148 || binary.LittleEndian.Uint32(payload[:4]) != 1 {
			t.Fatalf("payload %d", len(payload))
		}
		if timeout <= 0 {
			t.Fatal("timeout")
		}
		reply := make([]byte, 92)
		binary.LittleEndian.PutUint32(reply, 2)
		return reply, nil
	}
	rec := httptest.NewRecorder()
	(&Server{}).probeWireGuardListen(rec, in, corecfg.ShareHost(in), in.Port)
	var out struct {
		OK        bool   `json:"ok"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		LatencyMS int64  `json:"latency_ms"`
		Error     string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK || out.Host != "203.0.113.8" || out.Port != 10433 || out.LatencyMS < 1 || gotLen != 148 {
		t.Fatalf("%+v addr %s", out, gotAddr)
	}
	if gotAddr != "203.0.113.8:10433" {
		t.Fatalf("dialed %s", gotAddr)
	}
	if st.String("probe_private_key") == "" {
		t.Fatal("probe key")
	}

	wgProbeExchange = func(addr string, payload []byte, timeout time.Duration) ([]byte, error) {
		return nil, &net.DNSError{IsTimeout: true}
	}
	rec = httptest.NewRecorder()
	(&Server{}).probeWireGuardListen(rec, in, "203.0.113.8", 10433)
	out = struct {
		OK        bool   `json:"ok"`
		Host      string `json:"host"`
		Port      int    `json:"port"`
		LatencyMS int64  `json:"latency_ms"`
		Error     string `json:"error"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.OK || out.Error != "超时" {
		t.Fatalf("timeout %+v", out)
	}
}
