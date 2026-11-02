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
	WGClientDNS        = "1.1.1.1"
	WGProbeAddress     = "192.0.2.1/32"
	wgClientStart      = 10
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

func buildWireGuard(serverID int64, ins []*db.Inbound, all []*db.Inbound, clients map[int64][]*db.Client) (*wsproto.WireGuardConfig, error) {
	var found []*db.Inbound
	for _, in := range ins {
		if in != nil && in.Profile == ProfileWireGuard {
			found = append(found, in)
		}
	}
	var enabled []*db.Inbound
	for _, in := range found {
		if in.Enabled {
			enabled = append(enabled, in)
		}
	}
	if len(enabled) > 1 {
		return nil, fmt.Errorf("一台机器只能有一条 WireGuard。家宽出口写在这条节点里")
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
	if len(enabled) == 1 {
		if exitOf != nil {
			return nil, fmt.Errorf("这台机器已是家宽出口，不能再在上面建 WireGuard 节点")
		}
		in := enabled[0]
		conf, peers, err := HubConfig(in, clients[in.ID])
		if err != nil {
			return nil, err
		}
		out := &wsproto.WireGuardConfig{Iface: WGIface, Conf: conf}
		for _, p := range peers {
			if p.Email == "" || p.Exit {
				continue
			}
			out.Peers = append(out.Peers, wsproto.WireGuardPeer{PublicKey: p.PublicKey, Email: p.Email})
		}
		return out, nil
	}
	if exitOf != nil {
		conf, err := HomeConfig(exitOf)
		if err != nil {
			return nil, err
		}
		return &wsproto.WireGuardConfig{Iface: WGIface, Conf: conf}, nil
	}
	if len(found) > 0 {
		return &wsproto.WireGuardConfig{Iface: WGIface}, nil
	}
	return nil, nil
}

func HubConfig(in *db.Inbound, clients []*db.Client) (string, []wgPeer, error) {
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
	fmt.Fprintf(&b, "PostUp = %s\nPostDown = %s\n", hubPostUp(st), hubPostDown(st))
	for _, p := range peers {
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", p.PublicKey, p.AllowedIPs)
	}
	if pub, addr := strings.TrimSpace(st.String("probe_public_key")), strings.TrimSpace(st.String("probe_address")); pub != "" && addr != "" {
		fmt.Fprintf(&b, "\n[Peer]\nPublicKey = %s\nAllowedIPs = %s\n", pub, addr)
	}
	return b.String(), peers, nil
}

func hubPostUp(st Settings) string {
	mark := st.Int("fwmark", WGDefaultFwMark)
	table := st.Int("route_table", WGDefaultTable)
	hub := wgHost(st.String("address"))
	exit := wgHost(st.String("exit_address"))
	network := strings.TrimSpace(st.String("network"))
	return fmt.Sprintf(
		"sysctl -w net.ipv4.ip_forward=1; sysctl -w net.ipv4.conf.%%i.rp_filter=2; iptables -C FORWARD -i %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -i %%i -j ACCEPT; iptables -C FORWARD -o %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -o %%i -j ACCEPT; ip rule del fwmark %d lookup main priority 40 2>/dev/null || true; ip rule add fwmark %d lookup main priority 40; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule add from %s lookup main priority 50; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule add from %s lookup main priority 50; ip rule del from %s lookup %d priority 100 2>/dev/null || true; ip rule add from %s lookup %d priority 100; ip route replace default dev %%i table %d; %s",
		mark, mark, hub, hub, exit, exit, network, table, network, table, table, wgMSSUp(st),
	)
}

func hubPostDown(st Settings) string {
	mark := st.Int("fwmark", WGDefaultFwMark)
	table := st.Int("route_table", WGDefaultTable)
	hub := wgHost(st.String("address"))
	exit := wgHost(st.String("exit_address"))
	network := strings.TrimSpace(st.String("network"))
	return fmt.Sprintf(
		"ip rule del fwmark %d lookup main priority 40 2>/dev/null || true; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule del from %s lookup main priority 50 2>/dev/null || true; ip rule del from %s lookup %d priority 100 2>/dev/null || true; ip route del default dev %%i table %d 2>/dev/null || true; iptables -D FORWARD -i %%i -j ACCEPT 2>/dev/null || true; iptables -D FORWARD -o %%i -j ACCEPT 2>/dev/null || true; %s",
		mark, hub, exit, network, table, table, wgMSSDown(st),
	)
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
	mss := wgMSS(st)
	return fmt.Sprintf(
		"iptables -t mangle -C FORWARD -i %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || iptables -t mangle -I FORWARD 1 -i %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || true; iptables -t mangle -C FORWARD -o %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || iptables -t mangle -I FORWARD 1 -o %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || true",
		mss, mss, mss, mss,
	)
}

func wgMSSDown(st Settings) string {
	mss := wgMSS(st)
	return fmt.Sprintf(
		"iptables -t mangle -D FORWARD -i %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || true; iptables -t mangle -D FORWARD -o %%i -p tcp -m tcp --tcp-flags SYN,RST SYN -j TCPMSS --set-mss %d 2>/dev/null || true",
		mss, mss,
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
	fmt.Fprintf(&b, "[Interface]\nPrivateKey = %s\nAddress = %s\nMTU = %d\nTable = off\n",
		st.String("exit_private_key"), st.String("exit_address"), st.Int("mtu", WGDefaultMTU))
	fmt.Fprintf(&b, "PostUp = sysctl -w net.ipv4.ip_forward=1; ip route replace %s dev %%i; iptables -C FORWARD -i %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -i %%i -j ACCEPT; iptables -C FORWARD -o %%i -j ACCEPT 2>/dev/null || iptables -I FORWARD 1 -o %%i -j ACCEPT; iface=$(ip route show default | awk '{print $5; exit}'); iptables -t nat -C POSTROUTING -s %s -o \"$iface\" -j MASQUERADE 2>/dev/null || iptables -t nat -A POSTROUTING -s %s -o \"$iface\" -j MASQUERADE; %s\n",
		network, network, network, wgMSSUp(st))
	fmt.Fprintf(&b, "PostDown = ip route del %s dev %%i 2>/dev/null || true; iptables -D FORWARD -i %%i -j ACCEPT 2>/dev/null || true; iptables -D FORWARD -o %%i -j ACCEPT 2>/dev/null || true; iface=$(ip route show default | awk '{print $5; exit}'); iptables -t nat -D POSTROUTING -s %s -o \"$iface\" -j MASQUERADE 2>/dev/null || true; %s\n",
		network, network, wgMSSDown(st))
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
