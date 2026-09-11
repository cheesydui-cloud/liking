package agent

import (
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
	dir     string
	mu      sync.Mutex
	procs   map[string]*proc
	last    map[string]json.RawMessage
	xrayAPI string
	xrayBin string
}

func NewCores(dir string) *Cores {
	return &Cores{dir: dir, procs: map[string]*proc{}, last: map[string]json.RawMessage{}}
}

func (c *Cores) Apply(cfg wsproto.ApplyConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.xrayAPI = cfg.XrayAPI

	var errs []string
	take := func(err error) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	take(c.applyJSON("xray", "xray.json", cfg.Xray, lookBin("xray"), []string{"run", "-c"}))
	take(c.applyJSON("singbox", "singbox.json", cfg.Singbox, lookBin("sing-box", "singbox"), []string{"run", "-c"}))
	take(c.applyMita(cfg.Mita))
	if len(errs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(errs, "; "))
}

func hasCfg(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func (c *Cores) applyJSON(name, file string, raw json.RawMessage, bin string, prefix []string) error {
	path := filepath.Join(c.dir, file)
	prev := c.last[name]
	if !hasCfg(raw) {
		c.stopLocked(name)
		_ = os.Remove(path)
		delete(c.last, name)
		return nil
	}
	if bin == "" {
		return fmt.Errorf("%s 未安装：请将二进制放到 PATH（/usr/local/bin）", name)
	}
	if name == "xray" {
		c.xrayBin = bin
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		return err
	}
	c.stopLocked(name)
	if err := checkPublicPorts(raw); err != nil {
		c.rollbackJSON(name, file, prev, bin, prefix)
		return err
	}
	args := append(append([]string{}, prefix...), path)
	if err := c.startLocked(name, bin, args); err != nil {
		c.rollbackJSON(name, file, prev, bin, prefix)
		return err
	}
	c.last[name] = append(json.RawMessage(nil), raw...)
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
	if !hasCfg(raw) {
		c.stopLocked("mita")
		_ = os.Remove(path)
		delete(c.last, "mita")
		return nil
	}
	bin := lookBin("mita")
	if bin == "" {
		return fmt.Errorf("mita 未安装：Mieru 入站需要 mita")
	}
	if err := checkPublicPorts(raw); err != nil {
		return err
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		return err
	}
	cmd := exec.Command(bin, "apply", "config", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mita apply: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command(bin, "start").Run()
	c.last["mita"] = append(json.RawMessage(nil), raw...)
	return nil
}

func (c *Cores) startLocked(name, bin string, args []string) error {
	cmd := exec.Command(bin, args...)
	logf, err := os.OpenFile(filepath.Join(c.dir, name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
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
		<-p.done
	}
}

func (c *Cores) StopAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for name := range c.procs {
		c.stopLocked(name)
	}
}

func (c *Cores) Running() []string {
	c.mu.Lock()
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
	return out
}

func (c *Cores) Collect() []wsproto.Sample {
	c.mu.Lock()
	bin, api := c.xrayBin, c.xrayAPI
	c.mu.Unlock()
	if bin == "" || api == "" {
		return nil
	}
	cmd := exec.Command(bin, "api", "statsquery", "--server="+api, "-reset")
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseXrayStats(out)
}

type xrayStats struct {
	Stat []struct {
		Name  string `json:"name"`
		Value any    `json:"value"`
	} `json:"stat"`
}

func parseXrayStats(raw []byte) []wsproto.Sample {
	var st xrayStats
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil
	}
	type acc struct{ up, down int64 }
	m := map[string]*acc{}
	for _, row := range st.Stat {
		parts := strings.Split(row.Name, ">>>")
		if len(parts) != 4 || parts[0] != "user" || parts[2] != "traffic" {
			continue
		}
		email := parts[1]
		a := m[email]
		if a == nil {
			a = &acc{}
			m[email] = a
		}
		v := anyInt(row.Value)
		if parts[3] == "uplink" {
			a.up = v
		} else if parts[3] == "downlink" {
			a.down = v
		}
	}
	var out []wsproto.Sample
	for email, a := range m {
		if a.up == 0 && a.down == 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: email, Up: a.up, Down: a.down})
	}
	return out
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
	tcp, udp := publicListenSpecs(raw)
	for _, a := range tcp {
		ln, err := net.Listen("tcp", a)
		if err != nil {
			host, port, _ := net.SplitHostPort(a)
			if host == "0.0.0.0" || host == "::" {
				return fmt.Errorf("端口 %s 已被占用", port)
			}
			return fmt.Errorf("端口 %s 已被占用", a)
		}
		_ = ln.Close()
	}
	for _, a := range udp {
		pc, err := net.ListenPacket("udp", a)
		if err != nil {
			_, port, _ := net.SplitHostPort(a)
			return fmt.Errorf("UDP 端口 %s 已被占用", port)
		}
		_ = pc.Close()
	}
	return nil
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
