package agent

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"liking/internal/wsproto"
)

type Cores struct {
	dir     string
	mu      sync.Mutex
	procs   map[string]*exec.Cmd
	xrayAPI string
	xrayBin string
}

func NewCores(dir string) *Cores {
	return &Cores{dir: dir, procs: map[string]*exec.Cmd{}}
}

func (c *Cores) Apply(cfg wsproto.ApplyConfig) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.xrayAPI = cfg.XrayAPI

	if err := c.applyJSON("xray", "xray.json", cfg.Xray, lookBin("xray"), []string{"run", "-c"}); err != nil {
		return err
	}
	if err := c.applyJSON("singbox", "singbox.json", cfg.Singbox, lookBin("sing-box", "singbox"), []string{"run", "-c"}); err != nil {
		return err
	}
	if err := c.applyMita(cfg.Mita); err != nil {
		return err
	}
	return nil
}

func (c *Cores) applyJSON(name, file string, raw json.RawMessage, bin string, prefix []string) error {
	path := filepath.Join(c.dir, file)
	if len(raw) == 0 || string(raw) == "null" {
		c.stopLocked(name)
		_ = os.Remove(path)
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
	args := append(append([]string{}, prefix...), path)
	return c.restartLocked(name, bin, args)
}

func (c *Cores) applyMita(raw json.RawMessage) error {
	path := filepath.Join(c.dir, "mita.json")
	if len(raw) == 0 || string(raw) == "null" {
		c.stopLocked("mita")
		_ = os.Remove(path)
		return nil
	}
	bin := lookBin("mita")
	if bin == "" {
		return fmt.Errorf("mita 未安装：Mieru 入站需要 mita")
	}
	if err := os.WriteFile(path, raw, 0o640); err != nil {
		return err
	}
	cmd := exec.Command(bin, "apply", "config", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("mita apply: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	_ = exec.Command(bin, "start").Run()
	return nil
}

func (c *Cores) restartLocked(name, bin string, args []string) error {
	c.stopLocked(name)
	cmd := exec.Command(bin, args...)
	logf, err := os.OpenFile(filepath.Join(c.dir, name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err == nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start %s: %w", name, err)
	}
	c.procs[name] = cmd
	go func() {
		_ = cmd.Wait()
		if logf != nil {
			_ = logf.Close()
		}
	}()
	log.Printf("agent: started %s pid=%d", name, cmd.Process.Pid)
	return nil
}

func (c *Cores) stopLocked(name string) {
	cmd := c.procs[name]
	if cmd == nil || cmd.Process == nil {
		delete(c.procs, name)
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGTERM)
	delete(c.procs, name)
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
	for name, cmd := range c.procs {
		if cmd != nil && cmd.Process != nil {
			out = append(out, name)
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
		// user>>>email>>>traffic>>>uplink
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
