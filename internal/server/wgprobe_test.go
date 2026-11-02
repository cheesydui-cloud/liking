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

	oldDial := listenDialTimeout
	defer func() { listenDialTimeout = oldDial }()
	listenDialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
		t.Fatal("udp handshake must not fall back")
		return nil, nil
	}
	// The success path above already returned. Re-run it with the dial stub
	// so a later edit cannot skip the handshake and call TCP instead.
	wgProbeExchange = func(addr string, payload []byte, timeout time.Duration) ([]byte, error) {
		reply := make([]byte, 92)
		binary.LittleEndian.PutUint32(reply, 2)
		return reply, nil
	}
	rec = httptest.NewRecorder()
	(&Server{}).probeWireGuardListen(rec, in, "203.0.113.8", 10433)
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.OK {
		t.Fatalf("handshake %+v", out)
	}

	var tcpAddr string
	listenDialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
		tcpAddr = address
		if network != "tcp" || timeout <= 0 {
			t.Fatalf("dial %s %s %s", network, address, timeout)
		}
		return &probeStubConn{}, nil
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
	if !out.OK || out.LatencyMS < 1 || tcpAddr != "203.0.113.8:10433" {
		t.Fatalf("tcp fallback %+v addr %s", out, tcpAddr)
	}

	listenDialTimeout = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return nil, &net.DNSError{IsTimeout: true}
	}
	oldLocal := wgLocalListenProbe
	defer func() { wgLocalListenProbe = oldLocal }()
	wgLocalListenProbe = func(s *Server, serverID int64, port int) (time.Duration, bool) {
		return 0, false
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

	wgLocalListenProbe = func(s *Server, serverID int64, port int) (time.Duration, bool) {
		if serverID != 7 || port != 10433 {
			t.Fatalf("local probe %d %d", serverID, port)
		}
		return 12 * time.Millisecond, true
	}
	in.ServerID = 7
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
	if !out.OK || out.LatencyMS != 12 {
		t.Fatalf("agent confirm %+v", out)
	}
}

type probeStubConn struct{ net.Conn }

func (probeStubConn) Close() error { return nil }
