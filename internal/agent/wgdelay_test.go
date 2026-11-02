package agent

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func TestWGDelayAcceptThenClose(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	_ = probe.Close()
	c := &Cores{}
	defer c.stopWGDelay()
	if !c.ensureWGDelay(port, 0) {
		t.Fatal("listen")
	}
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 8)
	if _, err = conn.Read(buf); err == nil {
		t.Fatal("delay listener stayed open")
	}
	c.stopWGDelay()
	if _, err = net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond); err == nil {
		t.Fatal("listener still up after stop")
	}
}
