package corecfg

import (
	"fmt"

	"liking/internal/db"
)

func buildMita(inbounds []*db.Inbound, clients map[int64][]*db.Client) (map[string]any, error) {
	var bindings []any
	var users []any
	var egressProxies []any
	var egressRules []any
	used := false
	seenUser := map[string]struct{}{}

	for _, in := range inbounds {
		if in.Core != CoreMita || !in.Enabled {
			continue
		}
		used = true
		st := ParseSettings(in.Settings)
		tr := st.String("transport")
		addTCP := tr == "TCP" || tr == "BOTH" || tr == ""
		addUDP := tr == "UDP" || tr == "BOTH"
		if addTCP {
			bindings = append(bindings, map[string]any{"port": in.Port, "protocol": "TCP"})
		}
		if addUDP {
			bindings = append(bindings, map[string]any{"port": in.Port, "protocol": "UDP"})
		}
		for _, c := range clients[in.ID] {
			if c == nil || !c.Enabled {
				continue
			}
			name := c.Username
			if name == "" {
				name = c.Email
			}
			if _, ok := seenUser[name]; ok {
				continue
			}
			seenUser[name] = struct{}{}
			users = append(users, map[string]any{"name": name, "password": c.Password})
		}
		if in.LineKind == "chain" {
			name := fmt.Sprintf("socks-%d", in.ID)
			egressProxies = append(egressProxies, map[string]any{
				"name":     name,
				"protocol": "SOCKS5_PROXY_PROTOCOL",
				"host":     "127.0.0.1",
				"port":     SocksPort(in.ID),
			})
			egressRules = append(egressRules, map[string]any{
				"ipRanges":   []string{"*"},
				"action":     "PROXY",
				"proxyNames": []string{name},
			})
		}
	}
	if !used {
		return nil, nil
	}
	if users == nil {
		users = []any{}
	}
	cfg := map[string]any{
		"portBindings": bindings,
		"users":        users,
		"loggingLevel": "ERROR",
		"mtu":          1400,
		// Unset DualStack is USE_FIRST_IP. AAAA-first answers on a host
		// without IPv6 make destinations fail until DNS order changes.
		"dns": map[string]any{"dualStack": "PREFER_IPv4"},
	}
	if len(egressProxies) > 0 {
		cfg["egress"] = map[string]any{
			"proxies": egressProxies,
			"rules":   egressRules,
		}
	}
	return cfg, nil
}
