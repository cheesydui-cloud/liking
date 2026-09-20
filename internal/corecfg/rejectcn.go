package corecfg

import (
	"database/sql"
	"net"
	"sort"
	"strings"

	"liking/internal/db"
	"liking/internal/wsproto"
)

var lookupIP = net.LookupIP

type landKey struct {
	host string
	port int
}

func CollectRejectCN(d *sql.DB, serverID int64, all []*db.Inbound) wsproto.RejectCN {
	return collectRejectCN(all, serverID, func(id int64) *db.Server {
		if d == nil {
			return nil
		}
		s, err := db.GetServer(d, id)
		if err != nil {
			return nil
		}
		return s
	})
}

func collectRejectCN(all []*db.Inbound, serverID int64, server func(int64) *db.Server) wsproto.RejectCN {
	var ports []int
	seenPort := map[int]struct{}{}
	lands := map[int64]landKey{}
	for _, in := range all {
		if in == nil || in.ServerID != serverID || !in.Enabled || in.Port < 1 || in.Port > 65535 {
			continue
		}
		if in.LineKind == "chain" || !in.RejectCN {
			continue
		}
		if _, ok := seenPort[in.Port]; !ok {
			seenPort[in.Port] = struct{}{}
			ports = append(ports, in.Port)
		}
		lands[in.ID] = landKey{host: strings.ToLower(ShareHost(in)), port: in.Port}
	}
	sort.Ints(ports)
	out := wsproto.RejectCN{Ports: ports}
	if len(ports) == 0 {
		return out
	}
	allow := map[string]struct{}{}
	addHost := func(h string) {
		for _, ip := range hostAddrs(h) {
			allow[ip] = struct{}{}
		}
	}
	for _, in := range all {
		if in == nil || !in.Enabled || in.LineKind != "chain" {
			continue
		}
		if !chainHitsLanding(in, lands) {
			continue
		}
		if in.ServerID == serverID {
			continue
		}
		if srv := server(in.ServerID); srv != nil {
			addHost(srv.PublicHost)
			addHost(srv.ConnectIP)
			continue
		}
		addHost(in.ServerHost)
		addHost(in.ConnectIP)
	}
	for ip := range allow {
		out.Allow = append(out.Allow, ip)
	}
	sort.Strings(out.Allow)
	return out
}

func chainHitsLanding(in *db.Inbound, lands map[int64]landKey) bool {
	if in.ExitInboundID != nil {
		if _, ok := lands[*in.ExitInboundID]; ok {
			return true
		}
	}
	for _, id := range HopInboundIDs(in) {
		if _, ok := lands[id]; ok {
			return true
		}
	}
	if strings.TrimSpace(in.ExitURI) == "" {
		return false
	}
	t, err := ParseShareURI(in.ExitURI)
	if err != nil || t == nil || t.Port < 1 {
		return false
	}
	host := strings.ToLower(strings.TrimSpace(t.Host))
	for _, land := range lands {
		if t.Port == land.port && host == land.host {
			return true
		}
	}
	return false
}

func hostAddrs(h string) []string {
	h = strings.TrimSpace(h)
	if h == "" {
		return nil
	}
	if ip := net.ParseIP(h); ip != nil {
		return []string{ip.String()}
	}
	ips, err := lookupIP(h)
	if err != nil {
		return nil
	}
	var out []string
	seen := map[string]struct{}{}
	for _, ip := range ips {
		s := ip.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
