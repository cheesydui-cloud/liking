package agent

import (
	"encoding/json"
	"io"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"liking/internal/wsproto"
)

func TestApplyConfigMissingForwardsStaysNil(t *testing.T) {
	var missing wsproto.ApplyConfig
	if err := json.Unmarshal([]byte(`{"rev":"abc"}`), &missing); err != nil {
		t.Fatal(err)
	}
	if missing.Forwards != nil {
		t.Fatal("old panel must not clear relays")
	}
	var empty wsproto.ApplyConfig
	if err := json.Unmarshal([]byte(`{"forwards":[]}`), &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Forwards == nil {
		t.Fatal("empty list must clear")
	}
}

func TestNftForwardScript(t *testing.T) {
	body := nftForwardScript([]udpForward{{Listen: 20000, IP: "203.0.113.11", Port: 20001}})
	for _, want := range []string{
		"table inet liking_forward",
		"udp dport 20000 dnat ip to 203.0.113.11:20001",
		"masquerade",
		"priority -10",
		"ct state established,related ct status dnat accept",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %s\n%s", want, body)
		}
	}
	if nftForwardScript(nil) != "" {
		t.Fatal("empty rules should clear the table")
	}
	if _, _, err := splitForwardRules([]wsproto.ForwardRule{{Port: 1, DestHost: "2001:db8::1", DestPort: 1, Proto: "udp"}}); err == nil {
		t.Fatal("ipv6 udp")
	}
}

func TestUDPForwardNeedsLinux(t *testing.T) {
	prev := forwardGOOS
	forwardGOOS = "windows"
	defer func() { forwardGOOS = prev }()
	f := NewForwarder()
	err := f.Apply([]wsproto.ForwardRule{{Port: 20000, DestHost: "203.0.113.9", DestPort: 20001, Proto: "udp"}})
	if err == nil || !strings.Contains(err.Error(), "Linux") {
		t.Fatalf("err %v", err)
	}
}

func TestTCPRelayCopies(t *testing.T) {
	echo, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer echo.Close()
	go func() {
		for {
			c, err := echo.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 8)
				n, _ := io.ReadFull(c, buf[:4])
				if n > 0 {
					_, _ = c.Write(buf[:n])
				}
			}(c)
		}
	}()
	ep := echo.Addr().(*net.TCPAddr)
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	f := NewForwarder()
	f.listenHost = "127.0.0.1"
	defer f.Close()
	if err := f.Apply([]wsproto.ForwardRule{{
		ID: 1, Port: port, DestHost: "127.0.0.1", DestPort: ep.Port, Proto: "tcp",
	}}); err != nil {
		t.Fatal(err)
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q", buf)
	}
	if err := f.Apply([]wsproto.ForwardRule{}); err != nil {
		t.Fatal(err)
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 300*time.Millisecond); err == nil {
		t.Fatal("listener still open")
	}
}
