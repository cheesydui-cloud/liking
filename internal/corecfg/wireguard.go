package corecfg

import (
	"crypto/ecdh"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"liking/internal/db"
	"liking/internal/wsproto"
)

const (
	WGIface            = "likingwg0"
	WGDefaultNetwork   = "10.7.0.0/24"
	WGDefaultAddress   = "10.7.0.1/24"
	WGDefaultExit      = "10.7.0.2/32"
	WGDefaultMTU       = 1280
	WGDefaultKeepalive = 25
	WGDefaultFwMark    = 51820
	WGDefaultTable     = 100
	// WGExitListenPort is the home machine's fixed UDP port. The entry dials
	// this port. The home must not pick a random port on bounce, or the
	// entry's endpoint stops answering. 51820 is not the entry's public port.
	WGExitListenPort = 51820
	WGClientDNS      = "1.1.1.1"
	WGProbeAddress   = "192.0.2.1/32"
	wgClientStart    = 10
)

func GenerateWireGuardKeyPair() (priv, pub string, err error) {
	raw := make([]byte, 32)
	if _, err = rand.Read(raw); err != nil {
		return "", "", err
	}
	return wgKeypair(raw)
}

func wireGuardPublic(priv string) (string, error) {
	_, pub, err := wgKeypair(mustWGRaw(priv))
	return pub, err
}

func wgKeypair(raw []byte) (priv, pub string, err error) {
	if len(raw) != 32 {
		return "", "", fmt.Errorf("WireGuard 私钥无效")
	}
	clamped := clampWG(raw)
	key, err := ecdh.X25519().NewPrivateKey(clamped)
	if err != nil {
		return "", "", fmt.Errorf("WireGuard 私钥无效")
	}
	return base64.StdEncoding.EncodeToString(clamped),
		base64.StdEncoding.EncodeToString(key.PublicKey().Bytes()), nil
}

func mustWGRaw(priv string) []byte {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(priv))
	if err != nil || len(b) != 32 {
		return nil
	}
	return b
}

func clampWG(b []byte) []byte {
	out := make([]byte, 32)
	copy(out, b)
	out[0] &= 248
	out[31] &= 127
	out[31] |= 64
	return out
}

func normalizeWireGuard(st Settings) error {
	network := strings.TrimSpace(st.String("network"))
	if network == "" {
		network = WGDefaultNetwork
	}
	address := strings.TrimSpace(st.String("address"))
	if address == "" {
		address = WGDefaultAddress
	}
	exitAddr := strings.TrimSpace(st.String("exit_address"))
	if exitAddr == "" {
		exitAddr = WGDefaultExit
	}
	netIP, err := parseWGCIDR(network, "隧道网段")
	if err != nil {
		return err
	}
	prefix, bits := netIP.Mask.Size()
	if bits != 32 || prefix < 16 || prefix > 30 {
		return fmt.Errorf("隧道网段要在 /16 到 /30 之间")
	}
	hubIP, err := parseWGCIDR(address, "中转机地址")
	if err != nil {
		return err
	}
	exitIP, err := parseWGCIDR(exitAddr, "家宽地址")
	if err != nil {
		return err
	}
	if !netIP.Contains(hubIP.IP) || !samePrefix(netIP, hubIP) {
		return fmt.Errorf("中转机地址的掩码要覆盖隧道网段，例如 10.7.0.1/24")
	}
	if !netIP.Contains(exitIP.IP) {
		return fmt.Errorf("家宽地址不在隧道网段里")
	}
	if hubIP.IP.Equal(exitIP.IP) {
		return fmt.Errorf("中转机地址和家宽地址不能相同")
	}
	ones, _ := exitIP.Mask.Size()
	if ones != 32 {
		return fmt.Errorf("家宽地址要写成 /32，例如 10.7.0.2/32")
	}
	st["network"] = network
	st["address"] = address
	st["exit_address"] = exitAddr
	st["mtu"] = clampInt(st.Int("mtu", WGDefaultMTU), 1280, 1500, WGDefaultMTU)
	st["keepalive"] = clampInt(st.Int("keepalive", WGDefaultKeepalive), 0, 65535, WGDefaultKeepalive)
	st["fwmark"] = clampInt(st.Int("fwmark", WGDefaultFwMark), 1, 0xffffffff, WGDefaultFwMark)
	st["route_table"] = clampInt(st.Int("route_table", WGDefaultTable), 1, 0xffffffff, WGDefaultTable)
	if exitServer := st.Int("exit_server_id", 0); exitServer > 0 {
		st["exit_server_id"] = exitServer
	} else {
		delete(st, "exit_server_id")
	}
	if err := fillWGKey(st, "private_key", "public_key"); err != nil {
		return err
	}
	if err := fillWGKey(st, "exit_private_key", "exit_public_key"); err != nil {
		return err
	}
	if strings.TrimSpace(st.String("probe_address")) == "" {
		st["probe_address"] = WGProbeAddress
	}
	if err := fillWGKey(st, "probe_private_key", "probe_public_key"); err != nil {
		return err
	}
	return nil
}

func clampInt(n, min, max, def int) int {
	if n < min || n > max {
		return def
	}
	return n
}

func fillWGKey(st Settings, privKey, pubKey string) error {
	priv := strings.TrimSpace(st.String(privKey))
	if priv == "" {
		priv, pub, err := GenerateWireGuardKeyPair()
		if err != nil {
			return err
		}
		st[privKey] = priv
		st[pubKey] = pub
		return nil
	}
	stored, pub, err := wgKeypair(mustWGRaw(priv))
	if err != nil {
		return err
	}
	st[privKey] = stored
	st[pubKey] = pub
	return nil
}

func parseWGCIDR(s, label string) (*net.IPNet, error) {
	ip, n, err := net.ParseCIDR(strings.TrimSpace(s))
	if err != nil || ip.To4() == nil || n == nil {
		return nil, fmt.Errorf("%s要写成 IPv4 网段，例如 10.7.0.1/24", label)
	}
	n.IP = ip.To4()
	return n, nil
}

func samePrefix(network, addr *net.IPNet) bool {
	a, _ := network.Mask.Size()
	b, _ := addr.Mask.Size()
	return a == b
}

// AssignFreshWireGuardNet gives a new node the next free /24 when the
// submitted network is empty or already used. An edit keeps its network.
// A free network the operator typed is left alone, including its addresses.
func AssignFreshWireGuardNet(in *db.Inbound, all []*db.Inbound) error {
	if in == nil || in.ID != 0 || in.Profile != ProfileWireGuard {
		return nil
	}
	st := ParseSettings(in.Settings)
	var used []string
	for _, other := range all {
		if other == nil || other.Profile != ProfileWireGuard || other.ID == in.ID {
			continue
		}
		n := strings.TrimSpace(ParseSettings(other.Settings).String("network"))
		if n == "" {
			n = WGDefaultNetwork
		}
		used = append(used, n)
	}
	current := strings.TrimSpace(st.String("network"))
	if current == "" {
		current = WGDefaultNetwork
	}
	cur, err := parseWGCIDR(current, "隧道网段")
	if err != nil {
		return err
	}
	if !wgOverlapsAny(cur, usedNets(used)) {
		return nil
	}
	network, address, exit, err := NextWireGuardNet(used)
	if err != nil {
		return err
	}
	st["network"] = network
	st["address"] = address
	st["exit_address"] = exit
	raw, err := st.Marshal()
	if err != nil {
		return err
	}
	in.Settings = raw
	return nil
}

// NextWireGuardNet returns the first free 10.7.0.0/24, 10.8.0.0/24, …
// The hub is .1 with the same prefix. The home address is .2/32.
func NextWireGuardNet(used []string) (network, address, exit string, err error) {
	taken := usedNets(used)
	for oct := 7; oct <= 255; oct++ {
		base := net.IPv4(10, byte(oct), 0, 0).To4()
		cand := &net.IPNet{IP: base, Mask: net.CIDRMask(24, 32)}
		if wgOverlapsAny(cand, taken) {
			continue
		}
		return base.String() + "/24", addWGIP(base, 1).String() + "/24", addWGIP(base, 2).String() + "/32", nil
	}
	return "", "", "", fmt.Errorf("没有空的隧道网段")
}

func usedNets(used []string) []*net.IPNet {
	var nets []*net.IPNet
	for _, s := range used {
		n, err := parseWGCIDR(s, "隧道网段")
		if err != nil {
			continue
		}
		nets = append(nets, n)
	}
	return nets
}

func wgOverlapsAny(n *net.IPNet, used []*net.IPNet) bool {
	if n == nil {
		return false
	}
	for _, other := range used {
		if other == nil {
			continue
		}
		if n.Contains(other.IP) || other.Contains(n.IP) {
			return true
		}
	}
	return false
}

func NewWireGuardClient(u *db.User, in *db.Inbound, existing []*db.Client) (*db.Client, error) {
	if u == nil || in == nil || in.Profile != ProfileWireGuard {
		return nil, fmt.Errorf("不是 WireGuard 节点")
	}
	st := ParseSettings(in.Settings)
	addr, err := allocateWGAddr(st, existing)
	if err != nil {
		return nil, err
	}
	priv, pub, err := GenerateWireGuardKeyPair()
	if err != nil {
		return nil, err
	}
	return &db.Client{
		InboundID: in.ID,
		UserID:    u.ID,
		Email:     db.EmailFor(u.ID, in.ID),
		UUID:      addr,
		Password:  priv,
		Username:  pub,
		Enabled:   true,
	}, nil
}

func allocateWGAddr(st Settings, existing []*db.Client) (string, error) {
	network, err := parseWGCIDR(nz(st.String("network"), WGDefaultNetwork), "隧道网段")
	if err != nil {
		return "", err
	}
	reserved := map[string]bool{}
	if hub, err := parseWGCIDR(nz(st.String("address"), WGDefaultAddress), "中转机地址"); err == nil {
		reserved[hub.IP.String()] = true
	}
	if exit, err := parseWGCIDR(nz(st.String("exit_address"), WGDefaultExit), "家宽地址"); err == nil {
		reserved[exit.IP.String()] = true
	}
	used := map[string]bool{}
	for _, c := range existing {
		if c == nil {
			continue
		}
		if ip := wgHost(c.UUID); ip != "" {
			used[ip] = true
		}
	}
	ones, bits := network.Mask.Size()
	width := bits - ones
	if width < 2 || width > 16 {
		return "", fmt.Errorf("隧道网段要在 /16 到 /30 之间")
	}
	total := 1 << uint(width)
	base := network.IP.Mask(network.Mask).To4()
	start := wgClientStart
	if start >= total-1 {
		start = 1
	}
	for step := 0; step < total; step++ {
		off := (start + step) % total
		if off == 0 || off == total-1 {
			continue
		}
		ip := addWGIP(base, off)
		if ip == nil || !network.Contains(ip) {
			continue
		}
		s := ip.String()
		if reserved[s] || used[s] {
			continue
		}
		return s + "/32", nil
	}
	return "", fmt.Errorf("隧道网段里没有空地址")
}

func addWGIP(base net.IP, off int) net.IP {
	b := base.To4()
	if b == nil {
		return nil
	}
	n := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	n += uint32(off)
	return net.IPv4(byte(n>>24), byte(n>>16), byte(n>>8), byte(n)).To4()
}

func wgHost(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if ip, _, err := net.ParseCIDR(s); err == nil && ip.To4() != nil {
		return ip.To4().String()
	}
	if ip := net.ParseIP(s); ip != nil && ip.To4() != nil {
		return ip.To4().String()
	}
	return ""
}

type wgPeer struct {
	PublicKey  string
	AllowedIPs string
	Email      string
	Exit       bool
}

func WGExitServerID(in *db.Inbound) int64 {
	if in == nil || in.Profile != ProfileWireGuard {
		return 0
	}
	id := ParseSettings(in.Settings).Int("exit_server_id", 0)
	if id <= 0 {
		return 0
	}
	return int64(id)
}

func CheckWireGuardExit(in *db.Inbound, all []*db.Inbound) error {
	if in == nil || in.Profile != ProfileWireGuard {
		return nil
	}
	if in.Enabled && in.ServerID != 0 {
		for _, other := range all {
			if other == nil || other.ID == in.ID || !other.Enabled || other.Profile != ProfileWireGuard {
				continue
			}
			if WGExitServerID(other) == in.ServerID {
				return fmt.Errorf("这台机器已是家宽出口，不能再在上面建 WireGuard 节点")
			}
		}
	}
	if err := checkWGNetworks(sameServerWG(in, all)); err != nil {
		return err
	}
	id := WGExitServerID(in)
	if id == 0 {
		return nil
	}
	if id == in.ServerID {
		return fmt.Errorf("家宽要选另一台实例")
	}
	for _, other := range all {
		if other == nil || other.ID == in.ID || !other.Enabled || other.Profile != ProfileWireGuard {
			continue
		}
		if other.ServerID == id {
			return fmt.Errorf("家宽实例上还有一条 WireGuard。先删掉那条，出口由这条入口推过去")
		}
		if WGExitServerID(other) == id {
			return fmt.Errorf("这台家宽已经是另一条 WireGuard 的出口")
		}
	}
	return nil
}

func sameServerWG(in *db.Inbound, all []*db.Inbound) []*db.Inbound {
	if in == nil {
		return nil
	}
	out := []*db.Inbound{in}
	for _, other := range all {
		if other == nil || other.ID == in.ID || other.ServerID != in.ServerID {
			continue
		}
		out = append(out, other)
	}
	return out
}

func persistWGProbe(d *sql.DB, in *db.Inbound) error {
	if d == nil || in == nil || in.ID == 0 || in.Profile != ProfileWireGuard {
		return nil
	}
	st := ParseSettings(in.Settings)
	if st.String("probe_private_key") != "" && st.String("probe_public_key") != "" && st.String("probe_address") != "" {
		return nil
	}
	if strings.TrimSpace(st.String("probe_address")) == "" {
		st["probe_address"] = WGProbeAddress
	}
	if err := fillWGKey(st, "probe_private_key", "probe_public_key"); err != nil {
		return err
	}
	raw, err := st.Marshal()
	if err != nil {
		return err
	}
	in.Settings = raw
	return db.UpdateInbound(d, in)
}

func exitHostFor(d *sql.DB, in *db.Inbound) (string, error) {
	if WGExitServerID(in) == 0 {
		return "", nil
	}
	if d == nil {
		return "", fmt.Errorf("家宽没有公开地址，入口无法连过去")
	}
	srv, err := db.GetServer(d, WGExitServerID(in))
	if err != nil {
		return "", fmt.Errorf("家宽实例: %w", err)
	}
	host := strings.TrimSpace(srv.PublicHost)
	if host == "" {
		host = strings.TrimSpace(srv.ConnectIP)
	}
	if host == "" {
		return "", fmt.Errorf("家宽没有公开地址，入口无法连过去")
	}
	return host, nil
}

func buildWireGuard(d *sql.DB, serverID int64, ins []*db.Inbound, all []*db.Inbound, clients map[int64][]*db.Client) (*wsproto.WireGuardConfig, error) {
	var found []*db.Inbound
	var enabled []*db.Inbound
	for _, in := range ins {
		if in == nil || in.Profile != ProfileWireGuard {
			continue
		}
		found = append(found, in)
		if in.Enabled {
			enabled = append(enabled, in)
		}
	}
	var exitOf *db.Inbound
	if serverID != 0 {
		for _, in := range all {
			if in == nil || !in.Enabled || in.Profile != ProfileWireGuard || WGExitServerID(in) != serverID {
				continue
			}
			if exitOf != nil {
				return nil, fmt.Errorf("这台家宽被多条 WireGuard 选为出口")
			}
			exitOf = in
		}
	}
	if len(enabled) > 0 {
		if exitOf != nil {
			return nil, fmt.Errorf("这台机器已是家宽出口，不能再在上面建 WireGuard 节点")
		}
		if err := checkWGNetworks(enabled); err != nil {
			return nil, err
		}
		if err := assignWGSlots(d, enabled); err != nil {
			return nil, err
		}
		sort.Slice(enabled, func(i, j int) bool {
			if enabled[i].ID != enabled[j].ID {
				return enabled[i].ID < enabled[j].ID
			}
			return enabled[i].Port < enabled[j].Port
		})
		seenPort := map[int]bool{}
		var tunnels []wsproto.WireGuardTunnel
		for _, in := range enabled {
			if seenPort[in.Port] {
				return nil, fmt.Errorf("这台机器上的 WireGuard 端口不能相同")
			}
			seenPort[in.Port] = true
			host, err := exitHostFor(d, in)
			if err != nil {
				return nil, err
			}
			conf, peers, err := HubConfig(in, clients[in.ID], host)
			if err != nil {
				return nil, err
			}
			iface := strings.TrimSpace(ParseSettings(in.Settings).String("iface"))
			if !validWGIfaceName(iface) {
				iface = WGIface
			}
			tunnels = append(tunnels, wsproto.WireGuardTunnel{
				Iface: iface,
				Conf:  conf,
				Peers: wgClientPeers(peers),
			})
		}
		return packWG(tunnels), nil
	}
	if exitOf != nil {
		conf, err := HomeConfig(exitOf)
		if err != nil {
			return nil, err
		}
		return packWG([]wsproto.WireGuardTunnel{{Iface: WGIface, Conf: conf}}), nil
	}
	if len(found) > 0 {
		return &wsproto.WireGuardConfig{Iface: WGIface}, nil
	}
	return nil, nil
}

// assignWGSlots gives each enabled node a stable interface, fwmark, and route
// table. A node that already has iface keeps it, so the first tunnel stays
// likingwg0 / 51820 / 100 and its policy rules do not move. Tables 253–255
// are reserved by Linux.
func assignWGSlots(d *sql.DB, enabled []*db.Inbound) error {
	if len(enabled) > 20 {
		return fmt.Errorf("一台入口最多 20 条 WireGuard")
	}
	sorted := append([]*db.Inbound(nil), enabled...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].ID != sorted[j].ID {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Port < sorted[j].Port
	})
	usedIface := map[string]bool{}
	usedMark := map[int]bool{}
	usedTable := map[int]bool{}
	type slotRow struct {
		in      *db.Inbound
		st      Settings
		slotted bool
	}
	rows := make([]slotRow, 0, len(sorted))
	for _, in := range sorted {
		st := ParseSettings(in.Settings)
		iface := strings.TrimSpace(st.String("iface"))
		mark := st.Int("fwmark", WGDefaultFwMark)
		table := st.Int("route_table", WGDefaultTable)
		// iface is the claim. Normalize writes the same default mark and table
		// onto every new node, so those numbers are not a claim by themselves.
		if validWGIfaceName(iface) && !usedIface[iface] && mark >= 1 && !usedMark[mark] && table >= 1 && table <= 252 && !usedTable[table] {
			usedIface[iface] = true
			usedMark[mark] = true
			usedTable[table] = true
			rows = append(rows, slotRow{in: in, st: st, slotted: true})
			continue
		}
		rows = append(rows, slotRow{in: in, st: st})
	}
	next := 0
	alloc := func() (string, int, int, error) {
		for next < 64 {
			iface := fmt.Sprintf("likingwg%d", next)
			mark := WGDefaultFwMark + next
			table := WGDefaultTable + next
			next++
			if usedIface[iface] || usedMark[mark] || usedTable[table] || table > 252 {
				continue
			}
			usedIface[iface] = true
			usedMark[mark] = true
			usedTable[table] = true
			return iface, mark, table, nil
		}
		return "", 0, 0, fmt.Errorf("这台入口没有空的 WireGuard 接口")
	}
	for i := range rows {
		if rows[i].slotted {
			continue
		}
		iface, mark, table, err := alloc()
		if err != nil {
			return err
		}
		rows[i].st["iface"] = iface
		rows[i].st["fwmark"] = mark
		rows[i].st["route_table"] = table
		raw, err := rows[i].st.Marshal()
		if err != nil {
			return err
		}
		if raw == rows[i].in.Settings {
			continue
		}
		rows[i].in.Settings = raw
		if d != nil && rows[i].in.ID != 0 {
			if err := db.UpdateInbound(d, rows[i].in); err != nil {
				return err
			}
		}
	}
	return nil
}

func validWGIfaceName(s string) bool {
	s = strings.TrimSpace(s)
	rest, ok := strings.CutPrefix(s, "likingwg")
	if !ok || rest == "" || (len(rest) > 1 && rest[0] == '0') {
		return false
	}
	n, err := strconv.Atoi(rest)
	return err == nil && n >= 0 && n < 64 && fmt.Sprintf("likingwg%d", n) == s
}

func checkWGNetworks(ins []*db.Inbound) error {
	var nets []*net.IPNet
	for _, in := range ins {
		if in == nil || !in.Enabled || in.Profile != ProfileWireGuard {
			continue
		}
		n, err := wgNetOf(in)
		if err != nil {
			return err
		}
		if wgOverlapsAny(n, nets) {
			return fmt.Errorf("这台机器上的隧道网段不能重叠")
		}
		nets = append(nets, n)
	}
	return nil
}

func wgNetOf(in *db.Inbound) (*net.IPNet, error) {
	raw := strings.TrimSpace(ParseSettings(in.Settings).String("network"))
	if raw == "" {
		raw = WGDefaultNetwork
	}
	return parseWGCIDR(raw, "隧道网段")
}

func packWG(tunnels []wsproto.WireGuardTunnel) *wsproto.WireGuardConfig {
	if len(tunnels) == 0 {
		return &wsproto.WireGuardConfig{Iface: WGIface}
	}
	iface := strings.TrimSpace(tunnels[0].Iface)
	if iface == "" {
		iface = WGIface
	}
	return &wsproto.WireGuardConfig{
		Iface:   iface,
		Conf:    tunnels[0].Conf,
		Peers:   append([]wsproto.WireGuardPeer(nil), tunnels[0].Peers...),
		Tunnels: tunnels,
	}
}

func wgClientPeers(peers []wgPeer) []wsproto.WireGuardPeer {
	var out []wsproto.WireGuardPeer
	for _, p := range peers {
		if p.Email == "" || p.Exit {
			continue
		}
		out = append(out, wsproto.WireGuardPeer{PublicKey: p.PublicKey, Email: p.Email})
	}
	return out
}

func HubConfig(in *db.Inbound, clients []*db.Client, exitHost string) (string, []wgPeer, error) {
	if in == nil || in.Profile != ProfileWireGuard {
		return "", nil, fmt.Errorf("不是 WireGuard 节点")
	}
	st := ParseSettings(in.Settings)
	if err := normalizeWireGuard(cloneSettings(st)); err != nil {
		return "", nil, err
	}
	if st.String("private_key") == "" || st.String("exit_public_key") == "" {
		return "", nil, fmt.Errorf("WireGuard 密钥还没生成")
	}
	exitHost = strings.TrimSpace(exitHost)
	if WGExitServerID(in) != 0 && exitHost == "" {
		return "", nil, fmt.Errorf("家宽没有公开地址，入口无法连过去")
	}
	var peers []wgPeer
	peers = append(peers, wgPeer{
		PublicKey:  st.String("exit_public_key"),
		AllowedIPs: "0.0.0.0/0",
		Exit:       true,
	})
	var users []*db.Client
	for _, c := range clients {
		if c == nil || !c.Enabled || IsRelayEmail(c.Email) {
			continue
		}
		users = append(users, c)
	}
	sort.Slice(users, func(i, j int) bool { return users[i].Email < users[j].Email })
	for _, c := range users {
		addr := strings.TrimSpace(c.UUID)
		if wgHost(addr) == "" || strings.TrimSpace(c.Username) == "" {
			return "", nil, fmt.Errorf("用户 %s 还没有 WireGuard 地址或公钥", c.Email)
		}
		peers = append(peers, wgPeer{PublicKey: strings.TrimSpace(c.Username), AllowedIPs: addr, Email: c.Email})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\nMTU = %d\nTable = off\nFwMark = %d\n",
		st.String("private_key"), st.String("address"), in.Port, st.Int("mtu", WGDefaultMTU), st.Int("fwmark", WGDefaultFwMark))
	fmt.Fprintf(&b, "PostUp = %s\nPostDown = %s\n", hubPostUp(st, in.Port), hubPostDown(st, in.Port))
	for _, p := range peers {
		// The comment is not a WireGuard key. A restarted agent reads it from
		// the on-disk conf and can still name the peer after its memory map is gone.
		if email := strings.TrimSpace(p.Email); email != "" && !p.Exit {
			fmt.Fprintf(&b, "\n[Peer]\n# liking-email=%s\nPublicKey = %s\nAllowedIPs = %s\n", email, p.PublicKey, p.AllowedIPs)
			continue
		}
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", p.PublicKey, p.AllowedIPs)
		if p.Exit && exitHost != "" {
			// The home cannot complete a handshake to the entry's public port
			// on this provider. The entry dials the home's fixed listen port
			// instead, and keepalive keeps that path up.
			ka := st.Int("keepalive", WGDefaultKeepalive)
			if ka <= 0 {
				ka = WGDefaultKeepalive
			}
			fmt.Fprintf(&b, "Endpoint = %s\nPersistentKeepalive = %d\n", hostPort(exitHost, WGExitListenPort), ka)
		}
	}
	if pub, addr := strings.TrimSpace(st.String("probe_public_key")), strings.TrimSpace(st.String("probe_address")); pub != "" && addr != "" {
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", pub, addr)
	}
	return b.String(), peers, nil
}

func hubPostUp(st Settings, port int) string {
	mark := st.Int("fwmark", WGDefaultFwMark)
	table := st.Int("route_table", WGDefaultTable)
	hub := wgHost(st.String("address"))
	exit := wgHost(st.String("exit_address"))
	network := strings.TrimSpace(st.String("network"))
	return fmt.Sprintf(
		"sysctl -w net.ipv4.ip_forward=1; sysctl -w net.ipv4.conf.%%i.rp_filter=2; iptables -C FORWARD -i %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -i %%i -j ACCEPT; iptables -C FORWARD -o %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -o %%i -j ACCEPT; ip rule del fwmark %d lookup main priority 40 2>/dev/null || true; ip rule add fwmark %d lookup main priority 40; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule add from %s lookup main priority 50; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule add from %s lookup main priority 50; ip rule del from %s lookup %d priority 100 2>/dev/null || true; ip rule add from %s lookup %d priority 100; ip route replace default dev %%i table %d; %s%s",
		mark, mark, hub, hub, exit, exit, network, table, network, table, table, hubMSSUp(st), wgTCPAllowUp(port),
	)
}

func hubPostDown(st Settings, port int) string {
	mark := st.Int("fwmark", WGDefaultFwMark)
	table := st.Int("route_table", WGDefaultTable)
	hub := wgHost(st.String("address"))
	exit := wgHost(st.String("exit_address"))
	network := strings.TrimSpace(st.String("network"))
	return fmt.Sprintf(
		"ip rule del fwmark %d lookup main priority 40 2>/dev/null || true; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule del from %s lookup %d priority 100 2>/dev/null || true; ip route del default dev %%i table %d 2>/dev/null || true; iptables -D FORWARD -i %%i -j ACCEPT 2>/dev/null || true; iptables -D FORWARD -o %%i -j ACCEPT 2>/dev/null || true; %s%s",
		mark, hub, exit, network, table, table, hubMSSDown(st), wgTCPAllowDown(port),
	)
}

// wgTCPAllowUp lets a delay-test SYN reach the accept-and-close listener.
// The provider may still drop TCP before it arrives. Failure must not take
// the tunnel down, so every clause ends with || true.
func wgTCPAllowUp(port int) string {
	if port < 1 || port > 65535 {
		return ""
	}
	return fmt.Sprintf("; iptables -C INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p tcp --dport %d -j ACCEPT 2>/dev/null || true", port, port)
}

func wgUDPAllowUp(port int) string {
	if port < 1 || port > 65535 {
		return ""
	}
	return fmt.Sprintf("; iptables -C INPUT -p udp --dport %d -j ACCEPT 2>/dev/null || iptables -I INPUT 1 -p udp --dport %d -j ACCEPT 2>/dev/null || true", port, port)
}

func wgUDPAllowDown(port int) string {
	if port < 1 || port > 65535 {
		return ""
	}
	return fmt.Sprintf("; iptables -D INPUT -p udp --dport %d -j ACCEPT 2>/dev/null || true", port)
}

func wgTCPAllowDown(port int) string {
	if port < 1 || port > 65535 {
		return ""
	}
	return fmt.Sprintf("; iptables -D INPUT -p tcp --dport %d -j ACCEPT 2>/dev/null || true", port)
}

func wgMSS(st Settings) int {
	mtu := st.Int("mtu", WGDefaultMTU)
	if mtu < 1280 || mtu > 1500 {
		mtu = WGDefaultMTU
	}
	mss := mtu - 40
	if mss < 536 {
		mss = 536
	}
	return mss
}

func wgMSSUp(st Settings) string {
	return wgMSSUpTable(st, "liking_mss")
}

// hubMSSUp uses one nft table per interface. A shared table would be deleted
// when a second tunnel's PostDown runs.
func hubMSSUp(st Settings) string {
	return wgMSSUpTable(st, "liking_mss_%i")
}

func hubMSSDown(st Settings) string {
	return wgMSSDownTable(st, "liking_mss_%i")
}

func wgMSSUpTable(st Settings, table string) string {
	mss := wgMSS(st)
	return "modprobe xt_TCPMSS 2>/dev/null || true; modprobe iptable_mangle 2>/dev/null || true; " +
		wgMSSClamp("i", "iifname", mss, table) + "; " + wgMSSClamp("o", "oifname", mss, table)
}

// wgMSSClamp loads the TCPMSS target when it can. A missing module used to be
// hidden by `|| true`, so the tunnel stayed up and large TCP stalled after a
// few KB. nft is the fallback, and a failure still must not take the tunnel down.
func wgMSSClamp(iptFlag, nftDir string, mss int, table string) string {
	return fmt.Sprintf(
		"iptables -t mangle -C FORWARD -%s %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || iptables -t mangle -I FORWARD 1 -%s %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || { nft list table ip %s >/dev/null 2>&1 || nft add table ip %s; nft list chain ip %s forward >/dev/null 2>&1 || nft add chain ip %s forward '{ type filter hook forward priority mangle; policy accept; }'; nft list chain ip %s forward 2>/dev/null | grep -q '%s \"%%i\"' || nft add rule ip %s forward %s \"%%i\" tcp flags syn tcp option maxseg size set %d; } || true",
		iptFlag, mss, iptFlag, mss, table, table, table, table, table, nftDir, table, nftDir, mss,
	)
}

func wgMSSDown(st Settings) string {
	return wgMSSDownTable(st, "liking_mss")
}

func wgMSSDownTable(st Settings, table string) string {
	mss := wgMSS(st)
	return fmt.Sprintf(
		"iptables -t mangle -D FORWARD -i %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || true; iptables -t mangle -D FORWARD -o %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || true; nft delete table ip %s 2>/dev/null || true",
		mss, mss, table,
	)
}

func HomeConfig(in *db.Inbound) (string, error) {
	if in == nil || in.Profile != ProfileWireGuard {
		return "", fmt.Errorf("不是 WireGuard 节点")
	}
	host := ShareHost(in)
	if host == "" {
		return "", fmt.Errorf("服务器未填写公开地址")
	}
	st := ParseSettings(in.Settings)
	network := strings.TrimSpace(st.String("network"))
	if network == "" {
		return "", fmt.Errorf("还没有隧道网段")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nListenPort = %d\nMTU = %d\nTable = off\n",
		st.String("exit_private_key"), st.String("exit_address"), WGExitListenPort, st.Int("mtu", WGDefaultMTU))
	fmt.Fprintf(&b, "PostUp = sysctl -w net.ipv4.ip_forward=1; ip route replace %s dev %%i; iptables -C FORWARD -i %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -i %%i -j ACCEPT; iptables -C FORWARD -o %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -o %%i -j ACCEPT; iface=$(ip route show default | awk '{print $5; exit}'); iptables -t nat -C POSTROUTING -s %s -o \"$iface\" -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s -o \"$iface\" -j MASQUERADE; %s%s\n",
		network, network, network, wgMSSUp(st), wgUDPAllowUp(WGExitListenPort))
	fmt.Fprintf(&b, "PostDown = ip route del %s dev %%i 2>/dev/null || true; iptables -D FORWARD -i %%i -j ACCEPT 2>/dev/null || true; iptables -D FORWARD -o %%i -j ACCEPT 2>/dev/null || true; iface=$(ip route show default | awk '{print $5; exit}'); iptables -t nat -D POSTROUTING -s %s -o \"$iface\" -j MASQUERADE 2>/dev/null || true; %s%s\n",
		network, network, wgMSSDown(st), wgUDPAllowDown(WGExitListenPort))
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = %s\nPersistentKeepalive = %d\n",
		st.String("public_key"), hostPort(host, in.Port), network, st.Int("keepalive", WGDefaultKeepalive))
	return b.String(), nil
}

func WireGuardClientConf(in *db.Inbound, c *db.Client) (string, error) {
	if in == nil || c == nil {
		return "", fmt.Errorf("missing inbound/client")
	}
	host := ShareHost(in)
	if host == "" {
		return "", fmt.Errorf("服务器未填写公开地址")
	}
	st := ParseSettings(in.Settings)
	if strings.TrimSpace(c.Password) == "" || strings.TrimSpace(c.UUID) == "" || st.String("public_key") == "" {
		return "", fmt.Errorf("WireGuard 客户端还没有密钥")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nDNS = %s\nMTU = %d\n",
		strings.TrimSpace(c.Password), strings.TrimSpace(c.UUID), WGClientDNS, st.Int("mtu", WGDefaultMTU))
	fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nEndpoint = %s\nAllowedIPs = 0.0.0.0/0\nPersistentKeepalive = %d\n",
		st.String("public_key"), hostPort(host, in.Port), st.Int("keepalive", WGDefaultKeepalive))
	return b.String(), nil
}

func wireGuardShareURI(in *db.Inbound, c *db.Client) (string, error) {
	host := ShareHost(in)
	st := ParseSettings(in.Settings)
	if strings.TrimSpace(c.Password) == "" || strings.TrimSpace(c.UUID) == "" || st.String("public_key") == "" {
		return "", fmt.Errorf("WireGuard 客户端还没有密钥")
	}
	q := url.Values{}
	q.Set("publickey", st.String("public_key"))
	q.Set("address", strings.TrimSpace(c.UUID))
	q.Set("mtu", strconv.Itoa(st.Int("mtu", WGDefaultMTU)))
	q.Set("keepalive", strconv.Itoa(st.Int("keepalive", WGDefaultKeepalive)))
	name := url.QueryEscape(in.Name)
	return fmt.Sprintf("wireguard://%s@%s?%s#%s", url.User(strings.TrimSpace(c.Password)).String(), hostPort(host, in.Port), q.Encode(), name), nil
}

func cloneSettings(st Settings) Settings {
	out := Settings{}
	for k, v := range st {
		out[k] = v
	}
	return out
}
