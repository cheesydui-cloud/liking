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
		c.armWGDelay(cfg.Conf)
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
			c.captureWGHandoff(cfg.Iface)
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
	c.armWGDelay(cfg.Conf)
	return nil
}

func (c *Cores) downWireGuard(iface string) error {
	if strings.TrimSpace(iface) == "" {
		iface = "likingwg0"
	}
	path := filepath.Join(c.dir, iface+".conf")
	if wgIfaceUp(iface) {
		c.captureWGHandoff(iface)
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
	c.stopWGDelay()
	return nil
}

func (c *Cores) rememberWG(cfg wsproto.WireGuardConfig) {
	c.wgIface = cfg.Iface
	next := map[string]string{}
	for _, p := range cfg.Peers {
		pub := canonicalWGKey(p.PublicKey)
		email := strings.TrimSpace(p.Email)
		if pub == "" || email == "" || strings.HasPrefix(email, "relay.") {
			continue
		}
		next[pub] = email
	}
	byIP := map[string]string{}
	for pub, ips := range parseWGConfPeers(cfg.Conf) {
		email := next[canonicalWGKey(pub)]
		if email == "" {
			continue
		}
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip == "" || ip == "0.0.0.0/0" || ip == "::/0" {
				continue
			}
			if prev, ok := byIP[ip]; ok && prev != email {
				byIP[ip] = ""
				continue
			}
			byIP[ip] = email
		}
	}
	c.wgPeers = next
	c.wgPeerIP = byIP
}

func (c *Cores) clearWG(iface string) {
	_ = os.Remove(filepath.Join(c.dir, iface+".conf"))
	_ = os.Remove(filepath.Join(c.dir, iface+".sync"))
	delete(c.lastReq, "wireguard")
	c.wgIface = ""
	c.wgPeers = nil
	c.wgPeerIP = nil
}

// captureWGHandoff saves peer counters before wg-quick down resets them.
// Apply holds c.mu; this uses its own lock so the snapshot cannot deadlock.
func (c *Cores) captureWGHandoff(iface string) {
	peers, ok := readWGDump(iface)
	if !ok || len(peers) == 0 {
		return
	}
	c.wgHandoffMu.Lock()
	c.wgHandoff = peers
	c.wgHandoffMu.Unlock()
}

func (c *Cores) viewWGHandoff() ([]wsproto.WGPeerStat, bool) {
	c.wgHandoffMu.Lock()
	defer c.wgHandoffMu.Unlock()
	if len(c.wgHandoff) == 0 {
		return nil, false
	}
	return append([]wsproto.WGPeerStat(nil), c.wgHandoff...), true
}

func (c *Cores) clearWGHandoff() {
	c.wgHandoffMu.Lock()
	c.wgHandoff = nil
	c.wgHandoffMu.Unlock()
}

func readWGDump(iface string) ([]wsproto.WGPeerStat, bool) {
	peers, ran := readWGCounters(iface)
	return peers, ran && len(peers) > 0
}

// readWGCounters reads kernel peer counters. ran is true when wg answered,
// even if the tunnel has no peers yet. An empty parse is not a peer list:
// the panel must not treat that as "peers reported" and drop email deltas.
func readWGCounters(iface string) (peers []wsproto.WGPeerStat, ran bool) {
	iface = strings.TrimSpace(iface)
	bin := lookBin("wg")
	if bin == "" || iface == "" {
		return nil, false
	}
	if raw, ok := wgShow(bin, iface, "dump"); ok {
		ran = true
		if peers = parseWGPeerDump(raw); len(peers) > 0 {
			return peers, true
		}
	}
	if raw, ok := wgShow(bin, iface, "transfer"); ok {
		ran = true
		if peers = parseWGTransfer(raw); len(peers) > 0 {
			return peers, true
		}
	}
	return nil, ran
}

func wgShow(bin, iface, sub string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "show", iface, sub).Output()
	if err != nil {
		return "", false
	}
	return string(out), true
}

// peekWireGuardPeers reads kernel counters even when the in-memory email map
// is empty. After an agent restart the tunnel can stay up while rememberWG
// has not run; the panel matches these peers by public key and tunnel address.
func (c *Cores) peekWireGuardPeers() ([]wsproto.WGPeerStat, bool) {
	if peers, ok := c.viewWGHandoff(); ok {
		c.pendingClearHandoff = true
		return peers, true
	}
	c.pendingClearHandoff = false
	c.mu.Lock()
	configured := strings.TrimSpace(c.wgIface)
	c.mu.Unlock()
	iface := configured
	if iface == "" {
		iface = "likingwg0"
		if _, err := os.Stat("/sys/class/net/" + iface); err != nil {
			return nil, false
		}
	}
	peers, ran := readWGCounters(iface)
	if len(peers) == 0 {
		if !ran && configured != "" {
			noteStatsFail("wireguard", fmt.Errorf("wg show %s", iface))
		}
		return nil, false
	}
	return peers, true
}

func (c *Cores) wgEmailDeltas(raw []wsproto.WGPeerStat) ([]wsproto.Sample, map[string]bytePair, bool) {
	c.mu.Lock()
	peers := c.wgPeers
	byIP := c.wgPeerIP
	prev := c.wgLast
	c.mu.Unlock()
	if len(peers) == 0 {
		return nil, nil, false
	}
	var cum []wsproto.Sample
	for _, p := range raw {
		email := emailForWGPeer(p.PublicKey, p.AllowedIPs, peers, byIP)
		if email == "" || strings.HasPrefix(email, "relay.") {
			continue
		}
		cum = append(cum, wsproto.Sample{Email: email, Up: p.Rx, Down: p.Tx})
	}
	deltas, next := flowDeltas(prev, cum)
	return deltas, next, true
}

func (c *Cores) peekWireGuard() ([]wsproto.Sample, map[string]bytePair, bool) {
	c.mu.Lock()
	iface := c.wgIface
	peers := c.wgPeers
	byIP := c.wgPeerIP
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
	cum := parseWGDump(string(out), peers, byIP)
	// A wg-quick bounce resets counters. Bill only growth after the new baseline.
	deltas, next := flowDeltas(prev, cum)
	return deltas, next, true
}

func parseWGDump(dump string, peers, byIP map[string]string) []wsproto.Sample {
	var out []wsproto.Sample
	for _, p := range parseWGPeerDump(dump) {
		email := emailForWGPeer(p.PublicKey, p.AllowedIPs, peers, byIP)
		if email == "" || strings.HasPrefix(email, "relay.") {
			continue
		}
		out = append(out, wsproto.Sample{Email: email, Up: p.Rx, Down: p.Tx})
	}
	return out
}

// parseWGPeerDump reads `wg show <iface> dump`. The first line is the
// interface and is not a peer. Peer columns are public key, preshared key,
// endpoint, allowed IPs, handshake, rx, tx, keepalive.
func parseWGPeerDump(dump string) []wsproto.WGPeerStat {
	lines := strings.Split(strings.ReplaceAll(dump, "\r\n", "\n"), "\n")
	var out []wsproto.WGPeerStat
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
		pub := strings.TrimSpace(cols[0])
		if pub == "" || pub == "(none)" {
			continue
		}
		rx, err1 := strconv.ParseInt(strings.TrimSpace(cols[5]), 10, 64)
		tx, err2 := strconv.ParseInt(strings.TrimSpace(cols[6]), 10, 64)
		if err1 != nil || err2 != nil || rx < 0 || tx < 0 {
			continue
		}
		allowed := strings.TrimSpace(cols[3])
		if allowed == "(none)" {
			allowed = ""
		}
		out = append(out, wsproto.WGPeerStat{PublicKey: pub, AllowedIPs: allowed, Rx: rx, Tx: tx})
	}
	return out
}

// parseWGTransfer reads `wg show <iface> transfer`: public key, rx, tx.
// There is no interface line and no allowed-ips. The panel matches the key.
func parseWGTransfer(out string) []wsproto.WGPeerStat {
	var peers []wsproto.WGPeerStat
	for _, line := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if len(cols) < 3 {
			continue
		}
		pub := strings.TrimSpace(cols[0])
		if pub == "" || pub == "(none)" {
			continue
		}
		rx, err1 := strconv.ParseInt(strings.TrimSpace(cols[1]), 10, 64)
		tx, err2 := strconv.ParseInt(strings.TrimSpace(cols[2]), 10, 64)
		if err1 != nil || err2 != nil || rx < 0 || tx < 0 {
			continue
		}
		peers = append(peers, wsproto.WGPeerStat{PublicKey: pub, Rx: rx, Tx: tx})
	}
	return peers
}

func (c *Cores) annotateWGPeers(raw []wsproto.WGPeerStat) []wsproto.WGPeerStat {
	out := append([]wsproto.WGPeerStat(nil), raw...)
	if len(out) == 0 {
		return out
	}
	c.mu.Lock()
	peers := c.wgPeers
	byIP := c.wgPeerIP
	c.mu.Unlock()
	if len(peers) == 0 && len(byIP) == 0 {
		return out
	}
	for i := range out {
		out[i].Email = emailForWGPeer(out[i].PublicKey, out[i].AllowedIPs, peers, byIP)
	}
	return out
}

func emailForWGPeer(pub, allowed string, peers, byIP map[string]string) string {
	if email := peers[canonicalWGKey(pub)]; email != "" {
		return email
	}
	var found string
	for _, ip := range strings.Split(allowed, ",") {
		ip = strings.TrimSpace(ip)
		if ip == "" || ip == "0.0.0.0/0" || ip == "::/0" || ip == "192.0.2.1/32" || ip == "(none)" {
			continue
		}
		email := byIP[ip]
		if email == "" {
			continue
		}
		if found != "" && found != email {
			return ""
		}
		found = email
	}
	return found
}

func canonicalWGKey(s string) string {
	return wsproto.CanonicalWGKey(s)
}

func parseWGConfPeers(conf string) map[string][]string {
	out := map[string][]string{}
	var pub string
	inPeer := false
	for _, line := range strings.Split(strings.ReplaceAll(conf, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if strings.HasPrefix(trim, "[") {
			inPeer = strings.EqualFold(trim, "[Peer]")
			pub = ""
			continue
		}
		if !inPeer {
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		val := strings.TrimSpace(parts[1])
		switch key {
		case "publickey":
			pub = val
			if _, ok := out[pub]; !ok {
				out[pub] = nil
			}
		case "allowedips":
			if pub == "" {
				continue
			}
			for _, ip := range strings.Split(val, ",") {
				ip = strings.TrimSpace(ip)
				if ip != "" {
					out[pub] = append(out[pub], ip)
				}
			}
		}
	}
	return out
}

func wgJSONListenPort(raw json.RawMessage) int {
	if len(raw) == 0 {
		return 0
	}
	var cfg struct {
		Conf string `json:"conf"`
	}
	if json.Unmarshal(raw, &cfg) != nil {
		return 0
	}
	return wgConfListenPort(cfg.Conf)
}

func wgConfListenPort(conf string) int {
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
		if !inIface {
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) != 2 || !strings.EqualFold(strings.TrimSpace(parts[0]), "ListenPort") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || n < 1 || n > 65535 {
			return 0
		}
		return n
	}
	return 0
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
