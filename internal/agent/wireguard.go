package agent

import (
	"context"
	"encoding/base64"
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
		if c.wgIface == "" && len(c.wgIfaces) == 0 && !hasCfg(c.lastReq["wireguard"]) {
			return nil
		}
		return c.downAllWireGuard()
	}
	var cfg wsproto.WireGuardConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf("wireguard 配置无效")
	}
	tunnels := activeWGTunnels(cfg)
	if len(tunnels) == 0 {
		return c.downAllWireGuard()
	}
	if lookBin("wg") == "" || lookBin("wg-quick") == "" {
		return fmt.Errorf("wireguard-tools 未安装")
	}
	if bytesEqual(raw, c.lastReq["wireguard"]) && wgTunnelsUp(tunnels) {
		c.rememberWG(cfg)
		c.armWGDelays(tunnelConfs(tunnels))
		for _, t := range tunnels {
			keepWGPath(c.dir, t.Iface, t.Conf)
		}
		return c.downLeftoverWG(tunnelIfaceSet(tunnels))
	}
	var prev wsproto.WireGuardConfig
	_ = json.Unmarshal(c.lastReq["wireguard"], &prev)
	prevBy := map[string]string{}
	for _, t := range activeWGTunnels(prev) {
		prevBy[t.Iface] = t.Conf
	}
	if strings.TrimSpace(prev.Conf) != "" {
		iface := strings.TrimSpace(prev.Iface)
		if iface == "" {
			iface = "likingwg0"
		}
		if prevBy[iface] == "" {
			prevBy[iface] = prev.Conf
		}
	}
	var errs []string
	for _, t := range tunnels {
		if err := c.applyOneWG(t, prevBy[t.Iface]); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if err := c.downLeftoverWG(tunnelIfaceSet(tunnels)); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) > 0 {
		return fmt.Errorf("%s", strings.Join(errs, "; "))
	}
	c.lastReq["wireguard"] = append(json.RawMessage(nil), raw...)
	c.rememberWG(cfg)
	c.armWGDelays(tunnelConfs(tunnels))
	for _, t := range tunnels {
		keepWGPath(c.dir, t.Iface, t.Conf)
	}
	return nil
}

func (c *Cores) applyOneWG(t wsproto.WireGuardTunnel, prevConf string) error {
	noteWGDial(c.dir, t.Iface)
	path := filepath.Join(c.dir, t.Iface+".conf")
	// Read the running file before replacing it. After an agent restart
	// lastReq is empty; comparing only that would bounce a healthy tunnel
	// and zero the kernel counters the panel still needs to catch up.
	disk, _ := os.ReadFile(path)
	up := wgIfaceUp(t.Iface)
	bounce := !up || wgNeedsBounce(t.Conf, prevConf, string(disk))
	if err := os.WriteFile(path, []byte(t.Conf), 0o600); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	quick := lookBin("wg-quick")
	if bounce {
		if up {
			c.captureWGHandoff(t.Iface)
			if msg, err := runLimited(ctx, quick, "down", path); err != nil {
				return fmt.Errorf("wireguard 关闭失败: %s", cmdErr(msg, err))
			}
		}
		if msg, err := runLimited(ctx, quick, "up", path); err != nil {
			return fmt.Errorf("wireguard 启动失败: %s", cmdErr(msg, err))
		}
		return nil
	}
	stripPath := filepath.Join(c.dir, t.Iface+".sync")
	if err := os.WriteFile(stripPath, []byte(stripWGQuick(t.Conf)), 0o600); err != nil {
		return err
	}
	if msg, err := runLimited(ctx, lookBin("wg"), "syncconf", t.Iface, stripPath); err != nil {
		return fmt.Errorf("wireguard 更新对端失败: %s", cmdErr(msg, err))
	}
	return nil
}

func (c *Cores) downAllWireGuard() error {
	ifaces := c.knownWGIfaces()
	if len(ifaces) == 0 {
		ifaces = []string{"likingwg0"}
	}
	var first error
	for _, iface := range ifaces {
		if err := c.downOneWG(iface); err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		return first
	}
	c.clearWGState()
	c.stopWGDelay()
	return nil
}

func (c *Cores) downLeftoverWG(keep map[string]struct{}) error {
	var first error
	for _, iface := range c.knownWGIfaces() {
		if _, ok := keep[iface]; ok {
			continue
		}
		if err := c.downOneWG(iface); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (c *Cores) downOneWG(iface string) error {
	iface = strings.TrimSpace(iface)
	if !likingWGIface(iface) {
		return nil
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
	_ = os.Remove(path)
	_ = os.Remove(filepath.Join(c.dir, iface+".sync"))
	return nil
}

func (c *Cores) rememberWG(cfg wsproto.WireGuardConfig) {
	tunnels := activeWGTunnels(cfg)
	if len(tunnels) == 0 && strings.TrimSpace(cfg.Conf) != "" {
		iface := strings.TrimSpace(cfg.Iface)
		if iface == "" {
			iface = "likingwg0"
		}
		tunnels = []wsproto.WireGuardTunnel{{Iface: iface, Conf: cfg.Conf, Peers: cfg.Peers}}
	}
	next := map[string]string{}
	byIP := map[string]string{}
	var ifaces []string
	for _, t := range tunnels {
		ifaces = append(ifaces, t.Iface)
		for _, p := range t.Peers {
			pub := canonicalWGKey(p.PublicKey)
			email := strings.TrimSpace(p.Email)
			if pub == "" || email == "" || strings.HasPrefix(email, "relay.") {
				continue
			}
			next[pub] = email
		}
		for pub, ips := range parseWGConfPeers(t.Conf) {
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
	}
	c.wgIfaces = ifaces
	if len(ifaces) > 0 {
		c.wgIface = ifaces[0]
	}
	c.wgPeers = next
	c.wgPeerIP = byIP
	saveWGLedger(c.dir, next, byIP)
}

func (c *Cores) clearWGState() {
	delete(c.lastReq, "wireguard")
	c.wgIface = ""
	c.wgIfaces = nil
	c.wgPeers = nil
	c.wgPeerIP = nil
	saveWGLedger(c.dir, nil, nil)
}

func activeWGTunnels(cfg wsproto.WireGuardConfig) []wsproto.WireGuardTunnel {
	var src []wsproto.WireGuardTunnel
	if len(cfg.Tunnels) > 0 {
		src = cfg.Tunnels
	} else if strings.TrimSpace(cfg.Iface) != "" || strings.TrimSpace(cfg.Conf) != "" || len(cfg.Peers) > 0 {
		iface := strings.TrimSpace(cfg.Iface)
		if iface == "" {
			iface = "likingwg0"
		}
		src = []wsproto.WireGuardTunnel{{Iface: iface, Conf: cfg.Conf, Peers: cfg.Peers}}
	}
	var out []wsproto.WireGuardTunnel
	for _, t := range src {
		iface := strings.TrimSpace(t.Iface)
		if iface == "" {
			iface = "likingwg0"
		}
		if !likingWGIface(iface) || strings.TrimSpace(t.Conf) == "" {
			continue
		}
		t.Iface = iface
		out = append(out, t)
	}
	return out
}

func tunnelConfs(tunnels []wsproto.WireGuardTunnel) []string {
	out := make([]string, 0, len(tunnels))
	for _, t := range tunnels {
		out = append(out, t.Conf)
	}
	return out
}

func tunnelIfaceSet(tunnels []wsproto.WireGuardTunnel) map[string]struct{} {
	out := map[string]struct{}{}
	for _, t := range tunnels {
		out[t.Iface] = struct{}{}
	}
	return out
}

func wgTunnelsUp(tunnels []wsproto.WireGuardTunnel) bool {
	for _, t := range tunnels {
		if !wgIfaceUp(t.Iface) {
			return false
		}
	}
	return len(tunnels) > 0
}

func likingWGIface(s string) bool {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, "likingwg")
	if !ok || rest == "" || (len(rest) > 1 && rest[0] == '0') {
		return false
	}
	for _, r := range rest {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (c *Cores) knownWGIfaces() []string {
	seen := map[string]struct{}{}
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if !likingWGIface(s) {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	add(c.wgIface)
	for _, iface := range c.wgIfaces {
		add(iface)
	}
	var cfg wsproto.WireGuardConfig
	_ = json.Unmarshal(c.lastReq["wireguard"], &cfg)
	add(cfg.Iface)
	for _, t := range cfg.Tunnels {
		add(t.Iface)
	}
	for _, iface := range likingWGConfs(c.dir) {
		add(iface)
	}
	return out
}

func likingWGConfs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "likingwg") || !strings.HasSuffix(name, ".conf") {
			continue
		}
		iface := strings.TrimSuffix(name, ".conf")
		if likingWGIface(iface) {
			out = append(out, iface)
		}
	}
	return out
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
		c.pendingWGByPort = nil
		return peers, true
	}
	c.pendingClearHandoff = false
	c.pendingWGByPort = nil
	c.mu.Lock()
	ifaces := append([]string(nil), c.wgIfaces...)
	if len(ifaces) == 0 && strings.TrimSpace(c.wgIface) != "" {
		ifaces = []string{strings.TrimSpace(c.wgIface)}
	}
	ports := wgIfacePortsLocked(c)
	dir := c.dir
	c.mu.Unlock()
	if len(ifaces) == 0 {
		ifaces = likingWGConfs(dir)
	}
	if len(ifaces) == 0 {
		if _, err := os.Stat("/sys/class/net/likingwg0"); err != nil {
			return nil, false
		}
		ifaces = []string{"likingwg0"}
	}
	var all []wsproto.WGPeerStat
	byPort := map[int][]wsproto.WGPeerStat{}
	ranAny := false
	for _, iface := range ifaces {
		peers, ran := readWGCounters(iface)
		if ran {
			ranAny = true
		}
		if len(peers) == 0 {
			if !ran {
				noteStatsFail("wireguard", fmt.Errorf("wg show %s", iface))
			}
			continue
		}
		all = append(all, peers...)
		if port := ports[iface]; port > 0 {
			byPort[port] = append(byPort[port], peers...)
		}
	}
	if len(byPort) > 0 {
		c.pendingWGByPort = byPort
	}
	if len(all) == 0 {
		if !ranAny && len(ifaces) > 0 {
			noteStatsFail("wireguard", fmt.Errorf("wg show %s", ifaces[0]))
		}
		return nil, false
	}
	return all, true
}

func wgIfacePortsLocked(c *Cores) map[string]int {
	out := map[string]int{}
	var cfg wsproto.WireGuardConfig
	_ = json.Unmarshal(c.lastReq["wireguard"], &cfg)
	for _, t := range activeWGTunnels(cfg) {
		if p := wgConfListenPort(t.Conf); p > 0 {
			out[t.Iface] = p
		}
	}
	if len(out) == 0 {
		iface := strings.TrimSpace(cfg.Iface)
		if iface == "" {
			iface = "likingwg0"
		}
		if p := wgConfListenPort(cfg.Conf); p > 0 {
			out[iface] = p
		}
	}
	return out
}

func (c *Cores) wgEmailDeltas(raw []wsproto.WGPeerStat) ([]wsproto.Sample, map[string]bytePair, bool) {
	c.mu.Lock()
	prev := c.wgLast
	c.mu.Unlock()
	peers, byIP := c.wgIdentityMaps()
	if len(peers) == 0 && len(byIP) == 0 {
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

// parseWGPeerDump reads `wg show <iface> dump` and `wg show all dump`.
// An interface line is not a peer. `all dump` prefixes every line with the
// interface name, which shifts rx/tx by one column. A line that is not a
// peer is skipped, including a transfer line that has no allowed-ips.
func parseWGPeerDump(dump string) []wsproto.WGPeerStat {
	lines := strings.Split(strings.ReplaceAll(dump, "\r\n", "\n"), "\n")
	var out []wsproto.WGPeerStat
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		shift := 0
		if len(cols) >= 2 && !looksLikeWGKey(cols[0]) && looksLikeWGKey(cols[1]) {
			shift = 1
		}
		if len(cols) < shift+7 {
			continue
		}
		pub := strings.TrimSpace(cols[shift])
		if pub == "" || pub == "(none)" || !wgDumpPeerKey(cols, shift) {
			continue
		}
		rx, err1 := strconv.ParseInt(strings.TrimSpace(cols[shift+5]), 10, 64)
		tx, err2 := strconv.ParseInt(strings.TrimSpace(cols[shift+6]), 10, 64)
		if err1 != nil || err2 != nil || rx < 0 || tx < 0 {
			continue
		}
		allowed := strings.TrimSpace(cols[shift+3])
		if allowed == "(none)" {
			allowed = ""
		}
		out = append(out, wsproto.WGPeerStat{PublicKey: pub, AllowedIPs: allowed, Rx: rx, Tx: tx})
	}
	return out
}

// wgDumpPeerKey accepts the short names used in tests and the base64 keys
// `wg show` prints. An interface line has the private key in column 0 and
// fewer than 7 peer columns, so it never reaches this check.
func wgDumpPeerKey(cols []string, shift int) bool {
	pub := strings.TrimSpace(cols[shift])
	if pub == "" || pub == "(none)" {
		return false
	}
	if looksLikeWGKey(pub) {
		return true
	}
	// Unprefixed dump lines are peers once they have the peer column count.
	// A name-prefixed line is a peer only when column 1 is a real key.
	return shift == 0
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
	peers, byIP := c.wgIdentityMaps()
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

// wgNeedsBounce is true when the tunnel must be torn down. An empty previous
// config is not enough: the on-disk conf is what wg-quick last applied, and
// a peer-only change can syncconf without zeroing counters.
func wgNeedsBounce(newConf, prevConf, diskConf string) bool {
	base := prevConf
	if strings.TrimSpace(wgInterfaceBlock(base)) == "" {
		base = diskConf
	}
	if strings.TrimSpace(wgInterfaceBlock(base)) == "" {
		return true
	}
	return wgInterfaceBlock(newConf) != wgInterfaceBlock(base)
}

func looksLikeWGKey(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "(none)" {
		return false
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return true
	}
	raw := strings.TrimRight(s, "=")
	b, err := base64.RawStdEncoding.DecodeString(raw)
	return err == nil && len(b) == 32
}

type wgLedger struct {
	Peers map[string]string `json:"peers"`
	IPs   map[string]string `json:"ips"`
}

func wgLedgerPath(dir string) string {
	return filepath.Join(dir, "wg-peers.json")
}

func saveWGLedger(dir string, peers, byIP map[string]string) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	path := wgLedgerPath(dir)
	if len(peers) == 0 && len(byIP) == 0 {
		_ = os.Remove(path)
		return
	}
	body, err := json.Marshal(wgLedger{Peers: peers, IPs: byIP})
	if err != nil {
		return
	}
	_ = os.WriteFile(path, body, 0o600)
}

func loadWGLedger(dir string) (map[string]string, map[string]string) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	body, err := os.ReadFile(wgLedgerPath(dir))
	if err != nil {
		return nil, nil
	}
	var led wgLedger
	if json.Unmarshal(body, &led) != nil {
		return nil, nil
	}
	return led.Peers, led.IPs
}

// wgIdentityMaps names a kernel peer after an agent restart. Memory is filled
// only by a successful apply. The ledger and the conf comment survive that.
func (c *Cores) wgIdentityMaps() (map[string]string, map[string]string) {
	c.mu.Lock()
	peers, byIP := c.wgPeers, c.wgPeerIP
	ifaces := append([]string(nil), c.wgIfaces...)
	if len(ifaces) == 0 && strings.TrimSpace(c.wgIface) != "" {
		ifaces = []string{strings.TrimSpace(c.wgIface)}
	}
	dir := c.dir
	c.mu.Unlock()
	if len(peers) > 0 || len(byIP) > 0 {
		return peers, byIP
	}
	if p, ip := loadWGLedger(dir); len(p) > 0 || len(ip) > 0 {
		return p, ip
	}
	if len(ifaces) == 0 {
		ifaces = likingWGConfs(dir)
	}
	if len(ifaces) == 0 {
		ifaces = []string{"likingwg0"}
	}
	mergedP := map[string]string{}
	mergedIP := map[string]string{}
	for _, iface := range ifaces {
		body, err := os.ReadFile(filepath.Join(dir, iface+".conf"))
		if err != nil {
			continue
		}
		p, ip := identityFromWGConf(string(body))
		for k, v := range p {
			mergedP[k] = v
		}
		for k, v := range ip {
			if prev, ok := mergedIP[k]; ok && prev != v {
				mergedIP[k] = ""
				continue
			}
			mergedIP[k] = v
		}
	}
	if len(mergedP) == 0 && len(mergedIP) == 0 {
		return nil, nil
	}
	return mergedP, mergedIP
}

func identityFromWGConf(conf string) (map[string]string, map[string]string) {
	peers := map[string]string{}
	byIP := map[string]string{}
	var email, pub string
	var ips []string
	flush := func() {
		email = strings.TrimSpace(email)
		pub = strings.TrimSpace(pub)
		if pub == "" || email == "" || strings.HasPrefix(email, "relay.") {
			email, pub, ips = "", "", nil
			return
		}
		key := canonicalWGKey(pub)
		peers[key] = email
		for _, ip := range ips {
			ip = strings.TrimSpace(ip)
			if ip == "" || ip == "0.0.0.0/0" || ip == "::/0" || ip == "192.0.2.1/32" {
				continue
			}
			if prev, ok := byIP[ip]; ok && prev != email {
				byIP[ip] = ""
				continue
			}
			byIP[ip] = email
		}
		email, pub, ips = "", "", nil
	}
	for _, line := range strings.Split(strings.ReplaceAll(conf, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "[") {
			flush()
			continue
		}
		if rest, ok := strings.CutPrefix(trim, "#"); ok {
			rest = strings.TrimSpace(rest)
			if v, ok := strings.CutPrefix(rest, "liking-email="); ok {
				email = strings.TrimSpace(v)
			}
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) != 2 {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(parts[0])) {
		case "publickey":
			pub = strings.TrimSpace(parts[1])
		case "allowedips":
			for _, ip := range strings.Split(parts[1], ",") {
				ip = strings.TrimSpace(ip)
				if ip != "" {
					ips = append(ips, ip)
				}
			}
		}
	}
	flush()
	if len(peers) == 0 && len(byIP) == 0 {
		return nil, nil
	}
	return peers, byIP
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
	ports := wgJSONListenPorts(raw)
	if len(ports) == 0 {
		return 0
	}
	return ports[0]
}

func wgJSONListenPorts(raw json.RawMessage) []int {
	if len(raw) == 0 {
		return nil
	}
	var cfg wsproto.WireGuardConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return nil
	}
	seen := map[int]struct{}{}
	var out []int
	add := func(conf string) {
		p := wgConfListenPort(conf)
		if p < 1 {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	if tunnels := activeWGTunnels(cfg); len(tunnels) > 0 {
		for _, t := range tunnels {
			add(t.Conf)
		}
		return out
	}
	add(cfg.Conf)
	return out
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
