package agent

import (
	"encoding/json"
	"net"
	"testing"
)

func TestPublicListenSpecsSkipsLoopback(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"inbounds": []any{
			map[string]any{"tag": "api", "listen": "127.0.0.1", "port": 10085},
			map[string]any{"tag": "in-1", "listen": "0.0.0.0", "port": 8443},
			map[string]any{"tag": "sb", "listen": "0.0.0.0", "listen_port": 8444},
		},
		"portBindings": []any{
			map[string]any{"port": 9443, "protocol": "TCP"},
		},
	})
	tcp, udp := publicListenSpecs(raw)
	if len(udp) != 0 {
		t.Fatalf("udp %v", udp)
	}
	got := map[string]bool{}
	for _, a := range tcp {
		got[a] = true
	}
	if !got["0.0.0.0:8443"] || !got["0.0.0.0:8444"] || !got["0.0.0.0:9443"] {
		t.Fatalf("tcp %v", tcp)
	}
	if got["127.0.0.1:10085"] {
		t.Fatal("api port should be skipped")
	}
}

func TestCheckPublicPortsBusy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	raw, _ := json.Marshal(map[string]any{
		"inbounds": []any{
			map[string]any{"listen": "127.0.0.1", "port": port},
		},
	})
	if err := checkPublicPorts(raw); err != nil {
		t.Fatal(err)
	}

	hold, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	busyPort := hold.Addr().(*net.TCPAddr).Port
	raw2, _ := json.Marshal(map[string]any{
		"inbounds": []any{
			map[string]any{"listen": "0.0.0.0", "port": busyPort},
		},
	})
	if err := checkPublicPorts(raw2); err == nil {
		t.Fatal("expected busy")
	}
}
