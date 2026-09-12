package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"liking/internal/wsproto"
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

func TestApplyMissingMitaStillStartsXray(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "xray")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+"/usr/bin:/bin")

	c := NewCores(filepath.Join(dir, "data"))
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer c.StopAll()

	xrayCfg, _ := json.Marshal(map[string]any{
		"inbounds": []any{
			map[string]any{"listen": "127.0.0.1", "port": 10085},
		},
	})
	mitaCfg, _ := json.Marshal(map[string]any{
		"portBindings": []any{
			map[string]any{"port": 8444, "protocol": "TCP"},
		},
	})
	old := ensureCore
	ensureCore = func(string) (string, error) { return "", fmt.Errorf("no mita") }
	defer func() { ensureCore = old }()
	if err := c.Apply(wsproto.ApplyConfig{Xray: xrayCfg, Mita: mitaCfg}); err == nil {
		t.Fatal("expected mita error")
	} else if !strings.Contains(err.Error(), "mita") {
		t.Fatalf("err %v", err)
	}
	run := c.Running()
	if len(run) != 1 || run[0] != "xray" {
		t.Fatalf("running %v", run)
	}
	pid := c.procs["xray"].cmd.Process.Pid
	if err := c.Apply(wsproto.ApplyConfig{Xray: xrayCfg, Mita: mitaCfg}); err == nil {
		t.Fatal("expected mita error on second apply")
	}
	if !c.aliveLocked("xray") {
		t.Fatal("xray should stay up")
	}
	if c.procs["xray"].cmd.Process.Pid != pid {
		t.Fatalf("xray restarted pid %d -> %d", pid, c.procs["xray"].cmd.Process.Pid)
	}
}

func TestDropOccupiedListenKeepsFreePort(t *testing.T) {
	hold, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	busy := hold.Addr().(*net.TCPAddr).Port
	freeLn, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	free := freeLn.Addr().(*net.TCPAddr).Port
	_ = freeLn.Close()

	raw, _ := json.Marshal(map[string]any{
		"inbounds": []any{
			map[string]any{"tag": "api", "listen": "127.0.0.1", "port": 10085},
			map[string]any{"tag": "busy", "listen": "0.0.0.0", "port": busy},
			map[string]any{"tag": "ok", "listen": "0.0.0.0", "port": free},
		},
	})
	filtered, skipped := dropOccupiedListen(raw)
	if len(skipped) != 1 || skipped[0] != busy {
		t.Fatalf("skipped %v", skipped)
	}
	var top map[string]any
	if err := json.Unmarshal(filtered, &top); err != nil {
		t.Fatal(err)
	}
	ins := top["inbounds"].([]any)
	tags := map[string]bool{}
	for _, x := range ins {
		tags[x.(map[string]any)["tag"].(string)] = true
	}
	if !tags["api"] || !tags["ok"] || tags["busy"] {
		t.Fatalf("tags %v", tags)
	}
}

func TestCollectTimesOut(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "xray")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	c := NewCores(filepath.Join(dir, "data"))
	c.xrayBin = fake
	c.xrayAPI = "127.0.0.1:9"
	start := time.Now()
	_ = c.Collect()
	if time.Since(start) > 6*time.Second {
		t.Fatalf("collect took %s", time.Since(start))
	}
}
