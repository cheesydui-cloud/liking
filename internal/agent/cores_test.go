package agent

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func writeFakeMita(t *testing.T, binDir, stateDir string) string {
	t.Helper()
	listener := `#!/usr/bin/env python3
import json, os, signal, socket, sys
cfg = json.load(open(sys.argv[1]))
socks = []
for b in cfg.get("portBindings") or []:
    port = int(b.get("port") or 0)
    proto = str(b.get("protocol") or "TCP").upper()
    if port <= 0:
        continue
    if proto == "UDP":
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        s.bind(("0.0.0.0", port))
    else:
        s = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        s.bind(("0.0.0.0", port))
        s.listen(8)
    socks.append(s)
open(sys.argv[2], "w").write(str(os.getpid()))
signal.pause()
`
	listenPath := filepath.Join(stateDir, "listen.py")
	if err := os.WriteFile(listenPath, []byte(listener), 0o755); err != nil {
		t.Fatal(err)
	}
	script := `#!/usr/bin/env python3
import os, shutil, signal, subprocess, sys, time

STATE = ` + fmt.Sprintf("%q", stateDir) + `
LISTEN_PY = ` + fmt.Sprintf("%q", listenPath) + `
STATUS = os.path.join(STATE, "status")
PIDF = os.path.join(STATE, "listen.pid")
CFG = os.path.join(STATE, "cfg.json")

def read_status():
    try:
        s = open(STATUS).read().strip()
        return s or "IDLE"
    except FileNotFoundError:
        return "IDLE"

def write_status(s):
    os.makedirs(STATE, exist_ok=True)
    open(STATUS, "w").write(s)

def bump_stops():
    n = 0
    path = os.path.join(STATE, "stops")
    try:
        n = int(open(path).read().strip() or "0")
    except FileNotFoundError:
        n = 0
    except ValueError:
        n = 0
    open(path, "w").write(str(n + 1))

def kill_listen():
    pid = 0
    try:
        pid = int(open(PIDF).read().strip() or "0")
    except FileNotFoundError:
        pid = 0
    except ValueError:
        pid = 0
    if pid:
        for sig in (signal.SIGTERM, signal.SIGKILL):
            try:
                os.kill(pid, sig)
            except ProcessLookupError:
                break
            for _ in range(40):
                try:
                    os.kill(pid, 0)
                    time.sleep(0.05)
                except ProcessLookupError:
                    break
    try:
        os.remove(PIDF)
    except FileNotFoundError:
        pass
    write_status("IDLE")

cmd = sys.argv[1] if len(sys.argv) > 1 else ""
if cmd == "stop":
    bump_stops()
    kill_listen()
    sys.exit(0)
if cmd == "apply":
    src = sys.argv[3] if len(sys.argv) > 3 else sys.argv[2]
    shutil.copyfile(src, CFG)
    sys.exit(0)
if cmd == "start":
    if not os.path.isfile(CFG):
        print("no config", file=sys.stderr)
        sys.exit(1)
    kill_listen()
    subprocess.Popen(
        [sys.executable, LISTEN_PY, CFG, PIDF],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        start_new_session=True,
    )
    for _ in range(50):
        if os.path.isfile(PIDF) and os.path.getsize(PIDF) > 0:
            break
        time.sleep(0.05)
    else:
        print("listen did not start", file=sys.stderr)
        sys.exit(1)
    write_status("RUNNING")
    sys.exit(0)
if cmd == "status":
    print('mita server status is "%s"' % read_status())
    sys.exit(0)
print("unknown", cmd, file=sys.stderr)
sys.exit(1)
`
	path := filepath.Join(binDir, "mita")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestApplyMitaReusesOwnPort(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	stateDir := filepath.Join(dir, "mita-state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeMita(t, binDir, stateDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := NewCores(filepath.Join(dir, "data"))
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer c.StopAll()
	t.Cleanup(func() {
		if bin := lookBin("mita"); bin != "" {
			_ = exec.Command(bin, "stop").Run()
		}
	})

	tcpOnly, _ := json.Marshal(map[string]any{
		"portBindings": []any{
			map[string]any{"port": port, "protocol": "TCP"},
		},
		"users": []any{map[string]any{"name": "u3", "password": "p"}},
	})
	if err := c.Apply(wsproto.ApplyConfig{Mita: tcpOnly}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if !listenHeld(tcpOnly) {
		t.Fatal("mita should hold tcp after first apply")
	}

	both, _ := json.Marshal(map[string]any{
		"portBindings": []any{
			map[string]any{"port": port, "protocol": "TCP"},
			map[string]any{"port": port, "protocol": "UDP"},
		},
		"users": []any{map[string]any{"name": "u3", "password": "p"}},
	})
	if err := c.Apply(wsproto.ApplyConfig{Mita: both}); err != nil {
		t.Fatalf("second apply should reuse own port, got %v", err)
	}
	if !listenHeld(both) {
		t.Fatal("mita should hold tcp+udp after second apply")
	}
}

func TestApplyMitaSkipsIdenticalWithoutStop(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	stateDir := filepath.Join(dir, "mita-state")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFakeMita(t, binDir, stateDir)
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	c := NewCores(filepath.Join(dir, "data"))
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		t.Fatal(err)
	}
	defer c.StopAll()
	t.Cleanup(func() {
		if bin := lookBin("mita"); bin != "" {
			_ = exec.Command(bin, "stop").Run()
		}
	})

	cfg, _ := json.Marshal(map[string]any{
		"portBindings": []any{
			map[string]any{"port": port, "protocol": "TCP"},
		},
		"users": []any{map[string]any{"name": "u3", "password": "p"}},
	})
	if err := c.Apply(wsproto.ApplyConfig{Mita: cfg}); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	stops := fakeMitaStops(t, stateDir)
	if stops == 0 {
		t.Fatal("first apply should stop mita at least once")
	}
	if err := c.Apply(wsproto.ApplyConfig{Mita: cfg}); err != nil {
		t.Fatalf("identical apply: %v", err)
	}
	if got := fakeMitaStops(t, stateDir); got != stops {
		t.Fatalf("identical apply restarted mita, stops %d -> %d", stops, got)
	}
}

func fakeMitaStops(t *testing.T, stateDir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(stateDir, "stops"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
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
