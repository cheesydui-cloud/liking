package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"liking/internal/wsproto"
)

// wgDialState remembers a listen port and peer endpoints that the pushed
// conf does not contain. syncconf drops them. The next apply puts them back.
type wgDialState struct {
	ListenPort int               `json:"listen_port,omitempty"`
	Peers      map[string]string `json:"peers,omitempty"`
}

type wgPolicyRule struct {
	Priority int
	From     string
	FwMark   uint64
	Table    string
	AddArgs  []string
}

func (c *Cores) keepWireGuard() {
	c.mu.Lock()
	dir := c.dir
	var cfg wsproto.WireGuardConfig
	_ = json.Unmarshal(c.lastReq["wireguard"], &cfg)
	tunnels := activeWGTunnels(cfg)
	c.mu.Unlock()
	for _, t := range tunnels {
		keepWGPath(dir, t.Iface, t.Conf)
	}
}

func keepWGPath(dir, iface, conf string) {
	iface = strings.TrimSpace(iface)
	conf = strings.TrimSpace(conf)
	if iface == "" || conf == "" {
		return
	}
	kernel := readWGDumpDial(iface)
	st := mergeWGDial(loadWGDial(dir, iface), kernel)
	saveWGDial(dir, iface, st)
	restoreWGDial(iface, conf, st, kernel)
	ensureWGPolicy(iface, conf)
}

func noteWGDial(dir, iface string) {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		return
	}
	kernel := readWGDumpDial(iface)
	if kernel.ListenPort == 0 && len(kernel.Peers) == 0 {
		return
	}
	saveWGDial(dir, iface, mergeWGDial(loadWGDial(dir, iface), kernel))
}

func wgDialPath(dir, iface string) string {
	iface = strings.TrimSpace(iface)
	if iface == "" {
		iface = "likingwg0"
	}
	return filepath.Join(dir, "wg-dial-"+iface+".json")
}

func loadWGDial(dir, iface string) wgDialState {
	if strings.TrimSpace(dir) == "" {
		return wgDialState{}
	}
	st := readWGDialFile(wgDialPath(dir, iface))
	if st.ListenPort == 0 && len(st.Peers) == 0 && (strings.TrimSpace(iface) == "" || strings.TrimSpace(iface) == "likingwg0") {
		st = readWGDialFile(filepath.Join(dir, "wg-dial.json"))
	}
	return st
}

func readWGDialFile(path string) wgDialState {
	body, err := os.ReadFile(path)
	if err != nil {
		return wgDialState{}
	}
	var st wgDialState
	if json.Unmarshal(body, &st) != nil {
		return wgDialState{}
	}
	return st
}

func saveWGDial(dir, iface string, st wgDialState) {
	if strings.TrimSpace(dir) == "" {
		return
	}
	if st.ListenPort == 0 && len(st.Peers) == 0 {
		return
	}
	body, err := json.Marshal(st)
	if err != nil {
		return
	}
	_ = os.WriteFile(wgDialPath(dir, iface), body, 0o600)
}

func mergeWGDial(st wgDialState, kernel wgDumpDial) wgDialState {
	if kernel.ListenPort > 0 {
		st.ListenPort = kernel.ListenPort
	}
	if st.Peers == nil && len(kernel.Peers) > 0 {
		st.Peers = map[string]string{}
	}
	for pub, ep := range kernel.Peers {
		if ep == "" || ep == "(none)" {
			continue
		}
		if st.Peers == nil {
			st.Peers = map[string]string{}
		}
		st.Peers[canonicalWGKey(pub)] = ep
	}
	return st
}

type wgDumpDial struct {
	ListenPort int
	Peers      map[string]string
}

func readWGDumpDial(iface string) wgDumpDial {
	bin := lookBin("wg")
	if bin == "" || strings.TrimSpace(iface) == "" {
		return wgDumpDial{}
	}
	raw, ok := wgShow(bin, iface, "dump")
	if !ok {
		return wgDumpDial{}
	}
	return parseWGDumpDial(raw)
}

func parseWGDumpDial(dump string) wgDumpDial {
	out := wgDumpDial{Peers: map[string]string{}}
	lines := strings.Split(strings.ReplaceAll(dump, "\r\n", "\n"), "\n")
	seenIface := false
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		cols := strings.Split(line, "\t")
		if !seenIface && len(cols) >= 3 && looksLikeWGKey(cols[0]) && !looksLikeWGKey(strings.TrimSpace(cols[2])) {
			if n, err := strconv.Atoi(strings.TrimSpace(cols[2])); err == nil && n > 0 && n <= 65535 {
				out.ListenPort = n
			}
			seenIface = true
			continue
		}
		seenIface = true
		shift := 0
		if len(cols) >= 2 && !looksLikeWGKey(cols[0]) && looksLikeWGKey(cols[1]) {
			shift = 1
		}
		if len(cols) < shift+3 {
			continue
		}
		pub := strings.TrimSpace(cols[shift])
		ep := strings.TrimSpace(cols[shift+2])
		if pub == "" || pub == "(none)" || ep == "" || ep == "(none)" {
			continue
		}
		out.Peers[canonicalWGKey(pub)] = ep
	}
	return out
}

func restoreWGDial(iface, conf string, st wgDialState, kernel wgDumpDial) {
	if wgConfListenPort(conf) == 0 && st.ListenPort > 0 && kernel.ListenPort != st.ListenPort {
		wgSet(iface, "listen-port", strconv.Itoa(st.ListenPort))
	}
	for pub, allowed := range wgPeersWithoutEndpoint(conf) {
		key := canonicalWGKey(pub)
		ep := ""
		if st.Peers != nil {
			ep = st.Peers[key]
		}
		if ep == "" || ep == "(none)" {
			continue
		}
		if kernel.Peers != nil && kernel.Peers[key] != "" && kernel.Peers[key] != "(none)" {
			continue
		}
		args := []string{"peer", pub, "endpoint", ep}
		if strings.Contains(allowed, "0.0.0.0/0") {
			args = append(args, "persistent-keepalive", "25")
		}
		if wgSet(iface, args...) {
			log.Printf("agent: wireguard restored peer endpoint")
		}
	}
}

func wgPeersWithoutEndpoint(conf string) map[string]string {
	out := map[string]string{}
	var pub, allowed string
	hasEndpoint := false
	flush := func() {
		if pub != "" && !hasEndpoint {
			out[pub] = allowed
		}
		pub, allowed = "", ""
		hasEndpoint = false
	}
	inPeer := false
	for _, line := range strings.Split(strings.ReplaceAll(conf, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") {
			if inPeer {
				flush()
			}
			inPeer = strings.EqualFold(trim, "[Peer]")
			continue
		}
		if !inPeer || trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		parts := strings.SplitN(trim, "=", 2)
		if len(parts) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(parts[0]))
		v := strings.TrimSpace(parts[1])
		switch k {
		case "publickey":
			pub = v
		case "allowedips":
			allowed = v
		case "endpoint":
			hasEndpoint = v != ""
		}
	}
	if inPeer {
		flush()
	}
	return out
}

func ensureWGPolicy(iface, conf string) {
	rules := wgPolicyRules(conf)
	if len(rules) == 0 && wgPolicyTable(conf) == 0 {
		return
	}
	show := ipRuleShow()
	for _, r := range rules {
		if ipRuleShowHas(show, r) {
			continue
		}
		if ipRun(append([]string{"rule", "add"}, r.AddArgs...)...) {
			log.Printf("agent: wireguard policy rule %d was missing", r.Priority)
			show = ipRuleShow()
		}
	}
	if table := wgPolicyTable(conf); table > 0 && iface != "" {
		dev := wgTableDefaultDev(table)
		if wgRouteWouldSteal(dev, iface) {
			log.Printf("agent: wireguard %s left table %d on %s", iface, table, dev)
			return
		}
		ipRun("route", "replace", "default", "dev", iface, "table", strconv.Itoa(table))
	}
}

// wgRouteWouldSteal is true when another likingwg interface already owns
// this table. Replacing the default would send that line's clients out the
// wrong home.
func wgRouteWouldSteal(currentDev, iface string) bool {
	currentDev = strings.TrimSpace(currentDev)
	iface = strings.TrimSpace(iface)
	if currentDev == "" || iface == "" || currentDev == iface {
		return false
	}
	return strings.HasPrefix(currentDev, "likingwg") && strings.HasPrefix(iface, "likingwg")
}

func parseWGDefaultDev(show string) string {
	for _, line := range strings.Split(show, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[0] != "default" {
			continue
		}
		for i, f := range fields {
			if f == "dev" && i+1 < len(fields) {
				return fields[i+1]
			}
		}
	}
	return ""
}

func wgTableDefaultDev(table int) string {
	if table <= 0 {
		return ""
	}
	bin := lookBin("ip")
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-4", "route", "show", "table", strconv.Itoa(table)).Output()
	if err != nil {
		return ""
	}
	return parseWGDefaultDev(string(out))
}

func wgPolicyRules(conf string) []wgPolicyRule {
	var out []wgPolicyRule
	for _, cmd := range splitWGCmds(conf) {
		r, ok := parseIPRuleAdd(cmd)
		if ok {
			out = append(out, r)
		}
	}
	return out
}

func wgPolicyTable(conf string) int {
	for _, cmd := range splitWGCmds(conf) {
		if !strings.Contains(cmd, "ip route replace default") {
			continue
		}
		fields := strings.Fields(cmd)
		for i, f := range fields {
			if f == "table" && i+1 < len(fields) {
				n, err := strconv.Atoi(strings.Trim(fields[i+1], ";"))
				if err == nil && n > 0 {
					return n
				}
			}
		}
	}
	return 0
}

func splitWGCmds(conf string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(conf, "\r\n", "\n"), "\n") {
		for _, part := range strings.Split(line, ";") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func parseIPRuleAdd(cmd string) (wgPolicyRule, bool) {
	fields := strings.Fields(cmd)
	if len(fields) < 4 || fields[0] != "ip" || fields[1] != "rule" || fields[2] != "add" {
		return wgPolicyRule{}, false
	}
	r := wgPolicyRule{From: "all", AddArgs: append([]string(nil), fields[3:]...)}
	for i := 3; i < len(fields); i++ {
		switch fields[i] {
		case "from":
			if i+1 < len(fields) {
				r.From = fields[i+1]
			}
		case "fwmark":
			if i+1 < len(fields) {
				r.FwMark = parseFwMark(fields[i+1])
			}
		case "lookup":
			if i+1 < len(fields) {
				r.Table = fields[i+1]
			}
		case "priority":
			if i+1 < len(fields) {
				r.Priority, _ = strconv.Atoi(fields[i+1])
			}
		}
	}
	if r.Table == "" || r.Priority == 0 {
		return wgPolicyRule{}, false
	}
	return r, true
}

func parseFwMark(s string) uint64 {
	s = strings.TrimSpace(s)
	base := 10
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		base = 16
		s = s[2:]
	}
	n, err := strconv.ParseUint(s, base, 32)
	if err != nil {
		return 0
	}
	return n
}

func ipRuleShowHas(show string, r wgPolicyRule) bool {
	for _, line := range strings.Split(show, "\n") {
		line = strings.TrimSpace(line)
		pri, rest, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(pri))
		if err != nil || n != r.Priority {
			continue
		}
		rest = strings.Join(strings.Fields(rest), " ")
		if r.From != "" && r.From != "all" && !strings.Contains(rest, "from "+r.From) {
			continue
		}
		if r.FwMark != 0 {
			hex := fmt.Sprintf("fwmark 0x%x", r.FwMark)
			dec := fmt.Sprintf("fwmark %d", r.FwMark)
			if !strings.Contains(rest, hex) && !strings.Contains(rest, dec) {
				continue
			}
		}
		if !strings.Contains(rest, "lookup "+r.Table) {
			continue
		}
		return true
	}
	return false
}

func ipRuleShow() string {
	bin := lookBin("ip")
	if bin == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, "-4", "rule", "show").Output()
	if err != nil {
		return ""
	}
	return string(out)
}

func ipRun(args ...string) bool {
	bin := lookBin("ip")
	if bin == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, bin, args...).Run(); err != nil {
		log.Printf("agent: ip %s: %v", strings.Join(args, " "), err)
		return false
	}
	return true
}

func wgSet(iface string, args ...string) bool {
	bin := lookBin("wg")
	if bin == "" || strings.TrimSpace(iface) == "" {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	full := append([]string{"set", iface}, args...)
	if err := exec.CommandContext(ctx, bin, full...).Run(); err != nil {
		log.Printf("agent: wg set: %v", err)
		return false
	}
	return true
}
