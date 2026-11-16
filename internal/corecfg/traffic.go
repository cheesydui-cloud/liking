package corecfg

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
	"unicode/utf8"

	"liking/internal/db"
	"liking/internal/wsproto"
)

const MaxTrafficHops = 4

// ForwardProto is the data plane for a landing node.
// TCP is relayed in userspace. UDP is kernel DNAT. tcp+udp does both.
func ForwardProto(profile, network string) string {
	switch profile {
	case ProfileHysteria2, ProfileWireGuard:
		return "udp"
	case ProfileSOCKS5:
		return "tcp+udp"
	case ProfileMieru:
		switch strings.ToUpper(strings.TrimSpace(network)) {
		case "UDP":
			return "udp"
		case "TCP":
			return "tcp"
		default:
			return "tcp+udp"
		}
	default:
		return "tcp"
	}
}

func ForwardProtoWants(proto, part string) bool {
	return proto == part || proto == "tcp+udp"
}

// DialHost picks the address the previous machine should connect to.
func DialHost(explicit, publicHost string) string {
	if h := strings.TrimSpace(explicit); h != "" {
		return h
	}
	return strings.TrimSpace(publicHost)
}

// ValidDialHost accepts an empty value, an IP, or a hostname. It rejects
// schemes, ports, and spaces.
func ValidDialHost(raw string) error {
	h := strings.TrimSpace(raw)
	if h == "" {
		return nil
	}
	if strings.ContainsAny(h, " /?#\\") || strings.Contains(h, "://") {
		return fmt.Errorf("连接地址无效")
	}
	if _, _, err := net.SplitHostPort(h); err == nil {
		return fmt.Errorf("连接地址不要带端口")
	}
	if ip := net.ParseIP(h); ip != nil {
		return nil
	}
	if len(h) > 253 || strings.Contains(h, "..") {
		return fmt.Errorf("连接地址无效")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("连接地址无效")
		}
	}
	return nil
}

// TrafficRulesForServer returns the listen rules this instance should run
// for one forward. The landing machine gets none.
func TrafficRulesForServer(fw *db.TrafficForward, serverID int64, servers map[int64]*db.Server, landing *db.Inbound) []wsproto.ForwardRule {
	if fw == nil || !fw.Enabled || landing == nil || landing.Port < 1 {
		return nil
	}
	proto := forwardProtoFor(landing)
	type hop struct {
		serverID int64
		port     int
		dial     string
	}
	chain := []hop{{
		serverID: fw.EntryServerID,
		port:     fw.EntryPort,
	}}
	for _, h := range fw.Hops {
		chain = append(chain, hop{serverID: h.ServerID, port: h.ListenPort, dial: h.DialHost})
	}
	var rules []wsproto.ForwardRule
	for i, cur := range chain {
		if cur.serverID != serverID || cur.port < 1 {
			continue
		}
		var destHost string
		var destPort int
		if i+1 < len(chain) {
			next := chain[i+1]
			srv := servers[next.serverID]
			pub := ""
			if srv != nil {
				pub = srv.PublicHost
			}
			destHost = DialHost(next.dial, pub)
			destPort = next.port
		} else {
			srv := servers[landing.ServerID]
			pub := ""
			if srv != nil {
				pub = srv.PublicHost
			}
			destHost = DialHost(fw.LandingDialHost, pub)
			destPort = landing.Port
		}
		if destHost == "" || destPort < 1 {
			continue
		}
		rules = append(rules, wsproto.ForwardRule{
			ID:       fw.ID,
			Port:     cur.port,
			DestHost: destHost,
			DestPort: destPort,
			Proto:    proto,
		})
	}
	return rules
}

func forwardProtoFor(landing *db.Inbound) string {
	if landing == nil {
		return "tcp"
	}
	netw := landing.Network
	if landing.Profile == ProfileMieru {
		netw = ParseSettings(landing.Settings).String("transport")
	}
	return ForwardProto(landing.Profile, netw)
}

func trafficRules(d *sql.DB, serverID int64) ([]wsproto.ForwardRule, error) {
	list, err := db.ListTrafficForwards(d)
	if err != nil {
		return nil, err
	}
	srvs, err := db.ListServers(d)
	if err != nil {
		return nil, err
	}
	byServer := map[int64]*db.Server{}
	for _, s := range srvs {
		if s != nil {
			byServer[s.ID] = s
		}
	}
	var rules []wsproto.ForwardRule
	for _, fw := range list {
		if fw == nil {
			continue
		}
		landing, err := db.GetInbound(d, fw.LandingInboundID)
		if err != nil || landing == nil {
			continue
		}
		rules = append(rules, TrafficRulesForServer(fw, serverID, byServer, landing)...)
	}
	return rules, nil
}

// RewriteForwardInbound copies a landing node so the share link uses the
// entry address and port. Credentials stay on the original client.
func RewriteForwardInbound(landing *db.Inbound, fw *db.TrafficForward, entryHost string) *db.Inbound {
	if landing == nil || fw == nil {
		return nil
	}
	cp := *landing
	cp.Name = strings.TrimSpace(fw.Name)
	if cp.Name == "" {
		cp.Name = landing.Name
	}
	if cp.Name == landing.Name {
		cp.Name = landing.Name + " · 入口"
	}
	cp.Port = fw.EntryPort
	cp.ServerHost = strings.TrimSpace(entryHost)
	cp.ConnectIP = ""
	cp.ServerID = fw.EntryServerID
	return &cp
}

// PrepareTrafficForward checks the path, fills blank ports, and names an
// unnamed forward from the machines on it. Entry and hops stay free of nodes.
func PrepareTrafficForward(d *sql.DB, fw *db.TrafficForward) error {
	if fw == nil {
		return fmt.Errorf("缺少转发")
	}
	if fw.EntryServerID <= 0 {
		return fmt.Errorf("请选择入口实例")
	}
	entry, err := db.GetServer(d, fw.EntryServerID)
	if err != nil {
		return fmt.Errorf("入口实例不存在")
	}
	if strings.TrimSpace(entry.PublicHost) == "" {
		return fmt.Errorf("入口需要公网地址，订阅要用它")
	}
	if len(fw.Hops) > MaxTrafficHops {
		return fmt.Errorf("中间机器太多，最多 %d 台", MaxTrafficHops)
	}
	if fw.LandingInboundID <= 0 {
		return fmt.Errorf("请选择落地节点")
	}
	landing, err := db.GetInbound(d, fw.LandingInboundID)
	if err != nil || landing == nil {
		return fmt.Errorf("落地节点不存在")
	}
	if !UserFacing(landing.Profile) {
		return fmt.Errorf("端口中转不能当落地")
	}
	if err := ValidDialHost(fw.LandingDialHost); err != nil {
		return err
	}
	fw.LandingDialHost = strings.TrimSpace(fw.LandingDialHost)
	landSrv, err := db.GetServer(d, landing.ServerID)
	if err != nil || landSrv == nil {
		return fmt.Errorf("落地实例不存在")
	}
	if DialHost(fw.LandingDialHost, landSrv.PublicHost) == "" {
		return fmt.Errorf("落地需要公网地址，或填写给上一台连的地址")
	}
	seen := map[int64]struct{}{fw.EntryServerID: {}}
	if _, ok := seen[landing.ServerID]; ok {
		return fmt.Errorf("落地和转发机器不能是同一台")
	}
	seen[landing.ServerID] = struct{}{}
	hopNames := make([]string, 0, len(fw.Hops))
	for i := range fw.Hops {
		h := &fw.Hops[i]
		h.Position = i + 1
		h.DialHost = strings.TrimSpace(h.DialHost)
		if err := ValidDialHost(h.DialHost); err != nil {
			return err
		}
		if h.ServerID <= 0 {
			return fmt.Errorf("请选择中间机器")
		}
		if _, ok := seen[h.ServerID]; ok {
			return fmt.Errorf("路径里不能重复同一台机器")
		}
		seen[h.ServerID] = struct{}{}
		srv, err := db.GetServer(d, h.ServerID)
		if err != nil || srv == nil {
			return fmt.Errorf("中间机器不存在")
		}
		if DialHost(h.DialHost, srv.PublicHost) == "" {
			return fmt.Errorf("「%s」需要公网地址，或填写给上一台连的地址", srv.Name)
		}
		hopNames = append(hopNames, srv.Name)
		if err := assignListenPort(d, fw.ID, h.ServerID, &h.ListenPort, srv.PortMin, srv.PortMax); err != nil {
			return err
		}
	}
	if err := assignListenPort(d, fw.ID, fw.EntryServerID, &fw.EntryPort, entry.PortMin, entry.PortMax); err != nil {
		return err
	}
	fw.Name = strings.TrimSpace(fw.Name)
	if fw.Name == "" {
		fw.Name = clipRunes(strings.Join(append([]string{entry.Name}, append(hopNames, landing.Name)...), " → "), 64)
	}
	return nil
}

func assignListenPort(d *sql.DB, forwardID, serverID int64, port *int, min, max int) error {
	if port == nil {
		return fmt.Errorf("端口无效")
	}
	used, err := db.ListenPortsUsed(d, serverID, 0, forwardID)
	if err != nil {
		return err
	}
	ins, err := db.ListInboundsByServer(d, serverID)
	if err != nil {
		return err
	}
	MarkReservedPorts(used, ins)
	if *port == 0 {
		p, err := PickFreePortRange(used, min, max)
		if err != nil {
			return err
		}
		*port = p
		return nil
	}
	if *port < 1 || *port > 65535 {
		return fmt.Errorf("端口范围 1–65535，或不填则随机")
	}
	if _, ok := used[*port]; ok {
		return fmt.Errorf("该机器上端口已被占用")
	}
	return nil
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	if n == 1 {
		return string(r[:1])
	}
	return string(r[:n-1]) + "…"
}
