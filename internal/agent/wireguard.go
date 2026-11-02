package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"liking/internal/wsproto"
)

func (c *Cores) applyWireGuard(raw json.RawMessage) error {
	if !hasCfg(raw) {
		if c.wgIface == "" && !hasCfg(c.lastReq["wireguard"]) {
			return nil
		}
		iface := c.wgIface
		if iface == "" {
			iface = "likingwg0"
		}
		return c.downWireGuard(iface)
	}
	var cfg wsproto.WireGuardConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("wireguard 配置无效")
	}
	if strings.TrimSpace(cfg.Iface) == "" {
		cfg.Iface = "likingwg0"
	}
	if strings.TrimSpace(cfg.Conf) == "" {
		return c.downWireGuard(cfg.Iface)
	}
	if lookBin("wg") == "" || lookBin("wg-quick") == "" {
		return fmt.Errorf("wireguard-tools 未安装")
	}
	if bytesEqual(raw, c.lastReq["wireguard"]) && wgIfaceUp(cfg.Iface) {
		c.rememberWG(cfg)
		return nil
	}
	path := filepath.Join(c.dir, cfg.Iface+".conf")
	if err := os.WriteFile(path, []byte(cfg.Conf), 0o600); err != nil {
		return err
	}
	var prev wsproto.WireGuardConfig
	_ = json.Unmarshal(c.lastReq["wireguard"], &prev)
	up := wgIfaceUp(cfg.Iface)
	changed := wgInterfaceBlock(cfg.Conf) != wgInterfaceBlock(prev.Conf)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	quick := lookBin("wg-quick")
	if !up || changed {
		if up {
			if msg, err := runLimited(ctx, quick, "down", path); err != nil {
				return fmt.Errorf("wireguard 关闭失败: %s", cmdErr(msg, err))
			}
		}
		if msg, err := runLimited(ctx, quick, "up", path); err != nil {
			return fmt.Errorf("wireguard 启动失败: %s", cmdErr(msg, err))
		}
	} else {
		stripPath := filepath.Join(c.dir, cfg.Iface+".sync")
		if err := os.WriteFile(stripPath, []byte(stripWGQuick(cfg.Conf)), 0o600); err != nil {
			return err
		}
		if msg, err := runLimited(ctx, lookBin("wg"), "syncconf", cfg.Iface, stripPath); err != nil {
			return fmt.Errorf("wireguard 更新对端失败: %s", cmdErr(msg, err))
		}
	}
	c.lastReq["wireguard"] = append(json.RawMessage(nil), raw...)
	c.rememberWG(cfg)
	return nil
}

func (c *Cores) downWireGuard(iface string) error {
	if strings.TrimSpace(iface) == "" {
		iface = "likingwg0"
	}
	path := filepath.Join(c.dir, iface+".conf")
	if wgIfaceUp(iface) {
		quick := lookBin("wg-quick")
		if quick == "" {
			return fmt.Errorf("wireguard-tools 未安装")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
		defer cancel()
		if _, err := os.Stat(path); err != nil {
			path = iface
		}
		if msg, err := runLimited(ctx, quick, "down", path); err != nil {
			return fmt.Errorf("wireguard 关闭失败: %s", cmdErr(msg, err))
		}
	}
	c.clearWG(iface)
	return nil
}

func (c *Cores) rememberWG(cfg wsproto.WireGuardConfig) {
	c.wgIface = cfg.Iface
	next := map[string]string{}
	for _, p := range cfg.Peers {
		pub := strings.TrimSpace(p.PublicKey)
		email := strings.TrimSpace(p.Email)
		if pub == "" || email == "" || strings.HasPrefix(email, "relay.") {
			continue
		}
		next[pub] = email
	}
	c.wgPeers = next
}

func (c *Cores) clearWG(iface string) {
	_ = os.Remove(filepath.Join(c.dir, iface+".conf"))
	_ = os.Remove(filepath.Join(c.dir, iface+".sync"))
	delete(c.lastReq, "wireguard")
	c.wgIface = ""
	c.wgPeers = nil
}

func (c *Cores) peekWireGuard() ([]wsproto.Sample, map[string]bytePair, bool) {
	c.mu.Lock()
	iface := c.wgIface
	peers := c.wgPeers
	prev := c.wgLast
	c.mu.Unlock()
	if iface == "" || len(peers) == 0 {
		return nil, nil, false
	}
	bin := lookBin("wg")
	if bin == "" {
		return nil, nil, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "show", iface, "dump").Output()
	if err != nil {
		noteStatsFail("wireguard", err)
		return nil, nil, false
	}
	cum := parseWGDump(string(out), peers)
	deltas, next := mitaDeltas(prev, cum)
	return deltas, next, true
}

func parseWGDump(dump string, peers map[string]string) []wsproto.Sample {
	lines := strings.Split(strings.ReplaceAll(dump, "\r\n", "\n"), "\n")
	var out []wsproto.Sample
	seen := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		seen++
		if seen == 1 {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 7 {
			continue
		}
		email := peers[strings.TrimSpace(cols[0])]
		if email == "" {
			continue
		}
		rx, err1 := strconv.ParseInt(strings.TrimSpace(cols[5]), 10, 64)
		tx, err2 := strconv.ParseInt(strings.TrimSpace(cols[6]), 10, 64)
		if err1 != nil || err2 != nil || rx < 0 || tx < 0 {
			continue
		}
		out = append(out, wsproto.Sample{Email: email, Up: rx, Down: tx})
	}
	return out
}

func stripWGQuick(conf string) string {
	var b strings.Builder
	inIface := false
	for _, line := range strings.Split(strings.ReplaceAll(conf, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "[") {
			inIface = strings.EqualFold(trim, "[Interface]")
			b.WriteString(trim)
			b.WriteByte('\n')
			continue
		}
		if inIface {
			key := strings.ToLower(strings.TrimSpace(strings.SplitN(trim, "=", 2)[0]))
			switch key {
			case "privatekey", "listenport", "fwmark":
			default:
				continue
			}
		}
		b.WriteString(trim)
		b.WriteByte('\n')
	}
	return b.String()
}

func wgInterfaceBlock(conf string) string {
	var b strings.Builder
	inIface := false
	for _, line := range strings.Split(strings.ReplaceAll(conf, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			if inIface {
				break
			}
			inIface = strings.EqualFold(trim, "[Interface]")
			continue
		}
		if inIface {
			b.WriteString(trim)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func wgIfaceUp(iface string) bool {
	bin := lookBin("wg")
	if bin == "" || strings.TrimSpace(iface) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, bin, "show", iface).Run() == nil
}

func runLimited(ctx context.Context, bin string, args ...string) (string, error) {
	if bin == "" {
		return "", fmt.Errorf("wireguard-tools 未安装")
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func cmdErr(msg string, err error) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if msg == "" {
		return err.Error()
	}
	if len(msg) > 300 {
		msg = msg[:300]
	}
	return msg
}

func bytesEqual(a, b json.RawMessage) bool {
	return string(a) == string(b) && len(a) > 0
}
