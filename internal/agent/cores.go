package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"liking/internal/wsproto"
)

type proc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

type Cores struct {
	dir        string
	mu         sync.Mutex
	collectMu  sync.Mutex
	procs      map[string]*proc
	last       map[string]json.RawMessage
	lastReq    map[string]json.RawMessage
	xrayAPI    string
	xrayBin    string
	singboxAPI string
	clashLast  map[string]clashSnap
	mitaLast   map[string]bytePair
	crashes    map[string][]time.Time
	stopWatch  chan struct{}
	stopOnce   sync.Once
}

func NewCores(dir string) *Cores {
	c := &Cores{
		dir: dir, procs: map[string]*proc{}, last: map[string]json.RawMessage{}, lastReq: map[string]json.RawMessage{},
		clashLast: map[string]clashSnap{}, mitaLast: map[string]bytePair{},
		crashes:   map[string][]time.Time{},
		stopWatch: make(chan struct{}),
	}
	go c.watchLoop()
	return c
}

func detectedCores() []string {
	var out []string
	if lookBin("xray") != "" {
		out = append(out, "xray")
	}
	if lookBin("sing-box", "singbox") != "" {
		out = append(out, "singbox")
	}
	if lookBin("mita") != "" {
		out = append(out, "mita")
	}
	return out
}

func normalizeAgentCore(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	switch n {
	case "sing-box", "singbox":
		return "singbox"
	case "mieru", "mita":
		return "mita"
	default:
		return n
	}
}

func knownAgentCore(name string) bool {
	switch normalizeAgentCore(name) {
	case "xray", "singbox", "mita":
		return true
	default:
		return false
	}
}

func (c *Cores) Remove(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	name = normalizeAgentCore(name)
	c.stopLocked(name)
	if name == "mita" {
		if bin := lookBin("mita"); bin != "" {
			_ = exec.Command(bin, "stop").Run()
		}
	}
	delete(c.last, name)
	delete(c.lastReq, name)
	delete(c.crashes, name)
	if name == "xray" {
		c.xrayBin = ""
	}
	_ = os.Remove(filepath.Join(c.dir, name+".json"))
	if name == "mita" {
		_ = os.Remove(filepath.Join(c.dir, "mita.json"))
	}
}

func (c *Cores) Apply(cfg wsproto.ApplyConfig) error {
	ensureApplyBins(cfg)
	applyDisableIPv6(cfg.DisableIPv6)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.xrayAPI = cfg.XrayAPI
	c.singboxAPI = cfg.SingboxAPI

	var errs []string
	take := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	take(c.applyJSON("xray", "xray.json", cfg.Xray, lookBin("xray"), []string{"run", "-c"}))
	take(c.applyJSON("singbox", "singbox.json", cfg.Singbox, lookBin("sing-box", "singbox"), []string{"run", "-c"}))
	take(c.applyMita(cfg.Mita))
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	applySpeedLimits(cfg.SpeedLimits)
	return nil
}

func ensureApplyBins(cfg wsproto.ApplyConfig) {
	type need struct {
		name string
		raw  json.RawMessage
		bins []string
	}
	for _, n := range []need{
		{"xray", cfg.Xray, []string{"xray"}},
		{"singbox", cfg.Singbox, []string{"sing-box", "singbox"}},
		{"mita", cfg.Mita, []string{"mita"}},
	} {
		if !hasCfg(n.raw) {
			continue
		}
		if lookBin(n.bins...) != "" {
			continue
		}
		_, _ = ensureCore(n.name)
	}
}

func hasCfg(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func (c *Cores) applyJSON(name, file string, raw json.RawMessage, bin string, prefix []string) error {
	path := filepath.Join(c.dir, file)
	prev := c.last[name]
	prevReq := c.lastReq[name]
	if !hasCfg(raw) {
		c.stopLocked(name)
		_ = os.Remove(path)
		delete(c.last, name)
		delete(c.lastReq, name)
		return nil
	}
	if bin == "" {
		return fmt.Errorf("%s 未安装", name)
	}
	if name == "xray" {
		c.xrayBin = bin
	}
	if bytes.Equal(raw, prevReq) && c.aliveLocked(name) {
		return nil
	}
	c.stopLocked(name)
	filtered, skipped := dropOccupiedListen(raw)
	if len(skipped) > 0 {
		c.rollbackJSON(name, file, prev, bin, prefix)
		if !hasCfg(prev) {
			delete(c.lastReq, name)
		} else {
			c.lastReq[name] = prevReq
		}
		return fmt.Errorf("已跳过占用端口: %s", joinPorts(skipped))
	}
	if err := os.WriteFile(path, filtered, 0o640); err != nil {
		c.rollbackJSON(name, file, prev, bin, prefix)
		c.lastReq[name] = prevReq
		return err
	}
	if err := testCoreConfig(name, bin, path); err != nil {
		c.rollbackJSON(name, file, prev, bin, prefix)
		if !hasCfg(prev) {
			delete(c.lastReq, name)
		} else {
			c.lastReq[name] = prevReq
		}
		return err
	}
	args := append(append([]string{}, prefix...), path)
	if err := c.startLocked(name, bin, args); err != nil {
		c.rollbackJSON(name, file, prev, bin, prefix)
		if !hasCfg(prev) {
			delete(c.lastReq, name)
		} else {
			c.lastReq[name] = prevReq
		}
		return err
	}
	c.last[name] = append(json.RawMessage(nil), filtered...)
	c.lastReq[name] = append(json.RawMessage(nil), raw...)
	return nil
}

func (c *Cores) rollbackJSON(name, file string, prev json.RawMessage, bin string, prefix []string) {
	path := filepath.Join(c.dir, file)
	c.stopLocked(name)
	if !hasCfg(prev) {
		_ = os.Remove(path)
		delete(c.last, name)
		return
	}
	if err := os.WriteFile(path, prev, 0o640); err != nil {
		log.Printf("agent: rollback write %s: %v", name, err)
		return
	}
	args := append(append([]string{}, prefix...), path)
	if err := c.startLocked(name, bin, args); err != nil {
		log.Printf("agent: rollback start %s: %v", name, err)
		return
	}
	c.last[name] = append(json.RawMessage(nil), prev...)
}

func (c *Cores) applyMita(raw json.RawMessage) error {
	path := filepath.Join(c.dir, "mita.json")
	bin := lookBin("mita")
	if !hasCfg(raw) {
		if bin != "" {
			_ = exec.Command(bin, "stop").Run()
		}
		c.stopLocked("mita")
		_ = os.Remove(path)
		delete(c.last, "mita")
		delete(c.lastReq, "mita")
		return nil
	}
	if bin == "" {
		return fmt.Errorf("mita 未安装")
	}
	// Skip without probing the live port. A raw TCP dial is not a mieru
	// handshake; doing it on every periodic apply just opens junk sessions.
	if bytes.Equal(raw, c.lastReq["mita"]) && mitaIsRunning(bin) {
		return nil
	}

	prev := append(json.RawMessage(nil), c.last["mita"]...)
	prevReq := append(json.RawMessage(nil), c.lastReq["mita"]...)
	restore := func() {
		if !hasCfg(prev) {
			_ = exec.Command(bin, "stop").Run()
			return
		}
		_ = os.WriteFile(path, prev, 0o640)
		_ = mitaApply(bin, path)
		c.last["mita"] = append(json.RawMessage(nil), prev...)
		c.lastReq["mita"] = append(json.RawMessage(nil), prevReq...)
	}

	// Stop first so mita's own sockets are not reported as "occupied".
	_ = exec.Command(bin, "stop").Run()
	if skipped := waitPortsFree(raw, 2*time.Second); len(skipped) > 0 {
		restore()
		return fmt.Errorf("已跳过占用端口: %s", joinPorts(skipped))
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		return err
	}
	if err := mitaApply(bin, path); err != nil {
		restore()
		return err
	}
	if !waitListenHeld(raw, 3*time.Second) {
		restore()
		return fmt.Errorf("mita 已启动但未监听配置端口")
	}
	c.last["mita"] = append(json.RawMessage(nil), raw...)
	c.lastReq["mita"] = append(json.RawMessage(nil), raw...)
	return nil
}

func mitaApply(bin, path string) error {
	var last error
	for i := 0; i < 10; i++ {
		_ = exec.Command(bin, "stop").Run()
		out, err := exec.Command(bin, "apply", "config", path).CombinedOutput()
		if err != nil {
			last = fmt.Errorf("mita apply: %v (%s)", err, strings.TrimSpace(string(out)))
			time.Sleep(300 * time.Millisecond)
			continue
		}
		out, err = exec.Command(bin, "start").CombinedOutput()
		if err != nil {
			last = fmt.Errorf("mita start: %v (%s)", err, strings.TrimSpace(string(out)))
			time.Sleep(300 * time.Millisecond)
			continue
		}
		if !mitaIsRunning(bin) {
			last = fmt.Errorf("mita start 后状态不是 RUNNING（%s）", strings.TrimSpace(string(out)))
			time.Sleep(300 * time.Millisecond)
			continue
		}
		return nil
	}
	return last
}

func mitaIsRunning(bin string) bool {
	out, err := exec.Command(bin, "status").CombinedOutput()
	if err != nil {
		return false
	}
	u := strings.ToUpper(string(out))
	return strings.Contains(u, "RUNNING") && !strings.Contains(u, "IDLE")
}

func waitPortsFree(raw json.RawMessage, d time.Duration) []int {
	deadline := time.Now().Add(d)
	var skipped []int
	for {
		skipped = busyPorts(raw)
		if len(skipped) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return skipped
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func listenHeld(raw json.RawMessage) bool {
	tcp, udp := publicListenSpecs(raw)
	if len(tcp)+len(udp) == 0 {
		return true
	}
	// Prefer connecting to 127.0.0.1: macOS SO_REUSEADDR lets a second bind
	// succeed even when the port is already taken, so a bind probe is not enough.
	for _, a := range tcp {
		_, port, err := net.SplitHostPort(a)
		if err != nil {
			return false
		}
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", port), 250*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
	}
	if len(tcp) > 0 {
		return true
	}
	for _, a := range udp {
		pc, err := net.ListenPacket("udp", a)
		if err == nil {
			_ = pc.Close()
			return false
		}
	}
	return true
}

func waitListenHeld(raw json.RawMessage, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if listenHeld(raw) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(80 * time.Millisecond)
	}
}

func testCoreConfig(name, bin, path string) error {
	if bin == "" || path == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch name {
	case "xray":
		cmd = exec.CommandContext(ctx, bin, "run", "-test", "-c", path)
	case "singbox":
		cmd = exec.CommandContext(ctx, bin, "check", "-c", path)
	default:
		return nil
	}
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	low := strings.ToLower(msg + " " + err.Error())
	if strings.Contains(low, "unknown flag") || strings.Contains(low, "unknown command") || strings.Contains(low, "not found") {
		return nil
	}
	if msg == "" {
		msg = err.Error()
	}
	return fmt.Errorf("%s 配置检查失败: %s", name, msg)
}

func rotateLog(path string) {
	st, err := os.Stat(path)
	if err != nil || st.Size() < 8<<20 {
		return
	}
	_ = os.Rename(path, path+".1")
}

func (c *Cores) startLocked(name, bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GOMEMLIMIT=256MiB")
	logPath := filepath.Join(c.dir, name+".log")
	rotateLog(logPath)
	logf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err == nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		if logf != nil {
			_ = logf.Close()
		}
		return fmt.Errorf("start %s: %w", name, err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		if logf != nil {
			_ = logf.Close()
		}
		close(done)
	}()
	select {
	case <-done:
		return fmt.Errorf("%s 启动后立即退出：%s", name, tailLog(filepath.Join(c.dir, name+".log")))
	case <-time.After(600 * time.Millisecond):
	}
	c.procs[name] = &proc{cmd: cmd, done: done}
	log.Printf("agent: started %s pid=%d", name, cmd.Process.Pid)
	return nil
}

func (c *Cores) aliveLocked(name string) bool {
	p := c.procs[name]
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return false
	}
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

func (c *Cores) stopLocked(name string) {
	p := c.procs[name]
	delete(c.procs, name)
	if p == nil || p.cmd == nil || p.cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGTERM)
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
		select {
		case <-p.done:
		case <-time.After(2 * time.Second):
		}
	}
}

func (c *Cores) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name := range c.procs {
		c.stopLocked(name)
	}
	if bin := lookBin("mita"); bin != "" {
		_ = exec.Command(bin, "stop").Run()
	}
	applySpeedLimits(nil)
}

func (c *Cores) Close() {
	c.stopOnce.Do(func() {
		if c.stopWatch != nil {
			close(c.stopWatch)
		}
	})
	c.StopAll()
}

func (c *Cores) watchLoop() {
	t := time.NewTicker(4 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-c.stopWatch:
			return
		case <-t.C:
			c.watchOnce()
		}
	}
}

func (c *Cores) watchOnce() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for name, raw := range c.last {
		if !hasCfg(raw) {
			continue
		}
		if name == "mita" {
			bin := lookBin("mita")
			if bin == "" || mitaIsRunning(bin) {
				continue
			}
			if c.tooManyCrashes(name, now) {
				continue
			}
			path := filepath.Join(c.dir, "mita.json")
			if err := mitaApply(bin, path); err != nil {
				c.noteCrash(name, now)
				log.Printf("agent watchdog: mita: %v", err)
			}
			continue
		}
		if c.aliveLocked(name) {
			continue
		}
		if c.tooManyCrashes(name, now) {
			continue
		}
		bin := lookBin(name)
		if name == "singbox" {
			bin = lookBin("sing-box", "singbox")
		}
		if name == "xray" && c.xrayBin != "" {
			bin = c.xrayBin
		}
		if bin == "" {
			continue
		}
		path := filepath.Join(c.dir, name+".json")
		prefix := []string{"run", "-c"}
		if err := c.startLocked(name, bin, append(append([]string{}, prefix...), path)); err != nil {
			c.noteCrash(name, now)
			log.Printf("agent watchdog: %s: %v", name, err)
		}
	}
}

func (c *Cores) tooManyCrashes(name string, now time.Time) bool {
	cut := now.Add(-2 * time.Minute)
	keep := c.crashes[name][:0]
	for _, t := range c.crashes[name] {
		if t.After(cut) {
			keep = append(keep, t)
		}
	}
	c.crashes[name] = keep
	return len(keep) >= 5
}

func (c *Cores) noteCrash(name string, now time.Time) {
	c.crashes[name] = append(c.crashes[name], now)
}

func (c *Cores) ListenPorts() []int {
	if !c.mu.TryLock() {
		return nil
	}
	defer c.mu.Unlock()
	seen := map[int]struct{}{}
	var out []int
	add := func(raw json.RawMessage) {
		tcp, udp := publicListenSpecs(raw)
		for _, a := range append(tcp, udp...) {
			_, p, err := net.SplitHostPort(a)
			if err != nil {
				continue
			}
			n, _ := strconv.Atoi(p)
			if n <= 0 {
				continue
			}
			if _, ok := seen[n]; ok {
				continue
			}
			seen[n] = struct{}{}
			out = append(out, n)
		}
	}
	for _, raw := range c.last {
		add(raw)
	}
	return out
}

func (c *Cores) Running() []string {
	if !c.mu.TryLock() {
		return nil
	}
	defer c.mu.Unlock()
	var out []string
	for name, p := range c.procs {
		if p != nil && p.cmd != nil && p.cmd.Process != nil {
			select {
			case <-p.done:
			default:
				out = append(out, name)
			}
		}
	}
	if hasCfg(c.last["mita"]) {
		if bin := lookBin("mita"); bin != "" && mitaIsRunning(bin) {
			found := false
			for _, n := range out {
				if n == "mita" {
					found = true
					break
				}
			}
			if !found {
				out = append(out, "mita")
			}
		}
	}
	return out
}

type xrayStats struct {
	Stat []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	} `json:"stat"`
}

func anyInt(v any) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case json.Number:
		n, _ := t.Int64()
		return n
	case string:
		var n int64
		fmt.Sscan(t, &n)
		return n
	default:
		return 0
	}
}

func lookBin(names ...string) string {
	for _, n := range names {
		if p, err := exec.LookPath(n); err == nil {
			return p
		}
		for _, dir := range []string{"/usr/local/bin", "/usr/bin"} {
			p := filepath.Join(dir, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				return p
			}
		}
	}
	return ""
}

func checkPublicPorts(raw json.RawMessage) error {
	if skipped := busyPorts(raw); len(skipped) > 0 {
		return fmt.Errorf("已跳过占用端口: %s", joinPorts(skipped))
	}
	return nil
}

func joinPorts(ports []int) string {
	parts := make([]string, 0, len(ports))
	seen := map[int]struct{}{}
	for _, p := range ports {
		if p <= 0 {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		parts = append(parts, strconv.Itoa(p))
	}
	return strings.Join(parts, ", ")
}

func busyPorts(raw json.RawMessage) []int {
	tcp, udp := publicListenSpecs(raw)
	var skipped []int
	seen := map[int]struct{}{}
	add := func(addr, network string) {
		_, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return
		}
		port, _ := strconv.Atoi(portStr)
		if port <= 0 {
			return
		}
		var probeErr error
		if network == "udp" {
			var pc net.PacketConn
			pc, probeErr = net.ListenPacket("udp", addr)
			if pc != nil {
				_ = pc.Close()
			}
		} else {
			var ln net.Listener
			ln, probeErr = net.Listen("tcp", addr)
			if ln != nil {
				_ = ln.Close()
			}
		}
		if probeErr != nil {
			if _, ok := seen[port]; ok {
				return
			}
			seen[port] = struct{}{}
			skipped = append(skipped, port)
		}
	}
	for _, a := range tcp {
		add(a, "tcp")
	}
	for _, a := range udp {
		add(a, "udp")
	}
	return skipped
}

func dropOccupiedListen(raw json.RawMessage) (json.RawMessage, []int) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil || top == nil {
		return raw, nil
	}
	var skipped []int
	seen := map[int]struct{}{}
	addSkip := func(port int) {
		if port <= 0 {
			return
		}
		if _, ok := seen[port]; ok {
			return
		}
		seen[port] = struct{}{}
		skipped = append(skipped, port)
	}
	if ins, ok := top["inbounds"].([]any); ok {
		keep := make([]any, 0, len(ins))
		for _, x := range ins {
			m, _ := x.(map[string]any)
			if m == nil {
				keep = append(keep, x)
				continue
			}
			listen := strAny(m["listen"])
			if listen == "" {
				listen = "0.0.0.0"
			}
			if isLoopback(listen) {
				keep = append(keep, x)
				continue
			}
			port := anyPort(m["port"])
			if port == 0 {
				port = anyPort(m["listen_port"])
			}
			if port == 0 {
				keep = append(keep, x)
				continue
			}
			ln, err := net.Listen("tcp", net.JoinHostPort(listen, strconv.Itoa(port)))
			if err != nil {
				addSkip(port)
				continue
			}
			_ = ln.Close()
			keep = append(keep, x)
		}
		top["inbounds"] = keep
	}
	if binds, ok := top["portBindings"].([]any); ok {
		keep := make([]any, 0, len(binds))
		for _, x := range binds {
			m, _ := x.(map[string]any)
			if m == nil {
				keep = append(keep, x)
				continue
			}
			port := anyPort(m["port"])
			if port == 0 {
				keep = append(keep, x)
				continue
			}
			addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
			proto := strings.ToUpper(strAny(m["protocol"]))
			var err error
			if proto == "UDP" {
				var pc net.PacketConn
				pc, err = net.ListenPacket("udp", addr)
				if pc != nil {
					_ = pc.Close()
				}
			} else {
				var ln net.Listener
				ln, err = net.Listen("tcp", addr)
				if ln != nil {
					_ = ln.Close()
				}
			}
			if err != nil {
				addSkip(port)
				continue
			}
			keep = append(keep, x)
		}
		top["portBindings"] = keep
	}
	out, err := json.Marshal(top)
	if err != nil {
		return raw, skipped
	}
	return out, skipped
}

func publicListenSpecs(raw json.RawMessage) (tcp []string, udp []string) {
	var top map[string]any
	if err := json.Unmarshal(raw, &top); err != nil {
		return nil, nil
	}
	if ins, ok := top["inbounds"].([]any); ok {
		for _, x := range ins {
			m, _ := x.(map[string]any)
			if m == nil {
				continue
			}
			listen := strAny(m["listen"])
			if listen == "" {
				listen = "0.0.0.0"
			}
			if isLoopback(listen) {
				continue
			}
			port := anyPort(m["port"])
			if port == 0 {
				port = anyPort(m["listen_port"])
			}
			if port == 0 {
				continue
			}
			tcp = append(tcp, net.JoinHostPort(listen, strconv.Itoa(port)))
		}
	}
	if binds, ok := top["portBindings"].([]any); ok {
		for _, x := range binds {
			m, _ := x.(map[string]any)
			if m == nil {
				continue
			}
			port := anyPort(m["port"])
			if port == 0 {
				continue
			}
			addr := net.JoinHostPort("0.0.0.0", strconv.Itoa(port))
			proto := strings.ToUpper(strAny(m["protocol"]))
			if proto == "UDP" {
				udp = append(udp, addr)
			} else {
				tcp = append(tcp, addr)
			}
		}
	}
	return tcp, udp
}

func isLoopback(listen string) bool {
	h := strings.TrimSpace(listen)
	if i := strings.LastIndex(h, ":"); i > 0 && !strings.Contains(h, "]") {
		// already host only
	}
	ip := net.ParseIP(h)
	if ip != nil && ip.IsLoopback() {
		return true
	}
	return h == "localhost" || h == "127.0.0.1" || h == "::1"
}

func strAny(v any) string {
	s, _ := v.(string)
	return s
}

func anyPort(v any) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	case json.Number:
		n, _ := t.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(t)
		return n
	default:
		return 0
	}
}

func tailLog(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return ""
	}
	const n = 1200
	start := st.Size() - n
	if start < 0 {
		start = 0
	}
	if _, err := f.Seek(start, io.SeekStart); err != nil {
		return ""
	}
	b, _ := io.ReadAll(f)
	s := strings.TrimSpace(string(b))
	if len(s) > 800 {
		s = s[len(s)-800:]
	}
	return s
}
