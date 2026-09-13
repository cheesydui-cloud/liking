package corecfg

import (
	"fmt"

	"liking/internal/db"
)

func buildSingbox(inbounds []*db.Inbound, clients map[int64][]*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound, apiPort int) (map[string]any, error) {
	var ins []any
	var outs []any
	used := false

	outs = append(outs, map[string]any{"type": "direct", "tag": "direct"})

	for _, in := range inbounds {
		if in.Core != CoreSingbox || !in.Enabled {
			continue
		}
		obj, err := singInbound(in, clients[in.ID], certs, byID)
		if err != nil {
			return nil, err
		}
		if obj == nil {
			continue
		}
		used = true
		ins = append(ins, obj)
		if in.LineKind == "chain" {
			obs, err := singChainOutbounds(in, byID)
			if err != nil {
				return nil, err
			}
			outs = append(outs, obs...)
		}
	}

	if !used {
		return nil, nil
	}

	// Route: each chained sing-box inbound uses a detour outbound.
	routeRules := []any{}
	final := "direct"
	for _, in := range inbounds {
		if in.Core != CoreSingbox || !in.Enabled {
			continue
		}
		if in.LineKind == "chain" {
			routeRules = append(routeRules, map[string]any{
				"inbound":  []string{inboundTag(in.ID)},
				"outbound": outboundTag(in.ID),
			})
		}
	}

	cfg := map[string]any{
		"log":       map[string]any{"level": "warn"},
		"inbounds":  ins,
		"outbounds": outs,
		"route":     map[string]any{"rules": routeRules, "final": final},
	}
	if apiPort > 0 {
		cfg["experimental"] = map[string]any{
			"clash_api": map[string]any{
				"external_controller": fmt.Sprintf("127.0.0.1:%d", apiPort),
			},
		}
	}
	return cfg, nil
}

func singInbound(in *db.Inbound, clients []*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound) (map[string]any, error) {
	switch in.Profile {
	case ProfileSOCKS5:
		return singSocksInbound(in, clients, byID)
	case ProfileAnyTLS:
	default:
		return nil, nil
	}
	st := ParseSettings(in.Settings)
	var users []any
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		users = append(users, map[string]any{"name": c.Email, "password": c.Password})
	}
	forEachRelayTo(in.ID, byID, func(email string, st Settings) {
		pw := st.String("relay_password")
		if pw == "" {
			return
		}
		users = append(users, map[string]any{"name": email, "password": pw})
	})
	if users == nil {
		users = []any{}
	}
	if in.CertID == nil {
		return nil, fmt.Errorf("入站 %s 需要 TLS 证书", in.Name)
	}
	c := certs[*in.CertID]
	if c == nil {
		return nil, fmt.Errorf("入站 %s 的证书不存在", in.Name)
	}
	sni := st.String("sni")
	tls := map[string]any{
		"enabled":     true,
		"certificate": pemBlock(c.CertPEM),
		"key":         pemBlock(c.KeyPEM),
		"min_version": nz(st.String("min_version"), "1.3"),
		"alpn":        st.ALPN(),
	}
	if sni != "" {
		tls["server_name"] = sni
	}
	return map[string]any{
		"type":        "anytls",
		"tag":         inboundTag(in.ID),
		"listen":      in.Listen,
		"listen_port": in.Port,
		"users":       users,
		"tls":         tls,
	}, nil
}

func singSocksInbound(in *db.Inbound, clients []*db.Client, byID map[int64]*db.Inbound) (map[string]any, error) {
	var users []any
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		user := clientSocksUser(c)
		if user == "" || c.Password == "" {
			continue
		}
		users = append(users, map[string]any{"username": user, "password": c.Password})
	}
	forEachRelayTo(in.ID, byID, func(_ string, st Settings) {
		user := st.String("relay_username")
		pass := st.String("relay_password")
		if user == "" || pass == "" {
			return
		}
		users = append(users, map[string]any{"username": user, "password": pass})
	})
	st := ParseSettings(in.Settings)
	if u, p := st.String("hold_user"), st.String("hold_pass"); u != "" && p != "" {
		users = append(users, map[string]any{"username": u, "password": p})
	}
	if users == nil {
		users = []any{map[string]any{"username": "hold", "password": "hold"}}
	}
	return map[string]any{
		"type":        "socks",
		"tag":         inboundTag(in.ID),
		"listen":      in.Listen,
		"listen_port": in.Port,
		"users":       users,
	}, nil
}

func singChainOutbounds(entry *db.Inbound, byID map[int64]*db.Inbound) ([]any, error) {
	path, err := ChainPath(entry, byID)
	if err != nil {
		return nil, err
	}
	if len(path) == 0 {
		return nil, fmt.Errorf("链式线路 %s 没有落地", entry.Name)
	}
	var outs []any
	var prev string
	for _, p := range path {
		ob, err := singPathOutbound(p)
		if err != nil {
			return nil, err
		}
		if prev != "" {
			ob["detour"] = prev
		}
		outs = append(outs, ob)
		prev = p.Tag
	}
	return outs, nil
}

func singPathOutbound(p PathHop) (map[string]any, error) {
	if p.Socks != nil {
		ob := map[string]any{
			"type":        "socks",
			"tag":         p.Tag,
			"server":      p.Socks.Host,
			"server_port": p.Socks.Port,
			"version":     "5",
		}
		if p.Socks.User != "" || p.Socks.Pass != "" {
			ob["username"] = p.Socks.User
			ob["password"] = p.Socks.Pass
		}
		return ob, nil
	}
	if p.Land == nil {
		return nil, fmt.Errorf("没有落地")
	}
	return singLandOutbound(p.Tag, p.Land, p.Cred)
}

func singLandOutbound(tag string, land *db.Inbound, st Settings) (map[string]any, error) {
	if land == nil {
		return nil, fmt.Errorf("落地不存在")
	}
	if st == nil {
		st = Settings{}
	}
	lst := ParseSettings(land.Settings)
	host := land.ServerHost
	if host == "" {
		return nil, fmt.Errorf("落地 %s 未填写公开地址", land.Name)
	}
	switch land.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		sni := first(lst.Strings("server_names"))
		ob := map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      host,
			"server_port": land.Port,
			"uuid":        st.String("relay_uuid"),
			"tls": map[string]any{
				"enabled":     true,
				"server_name": sni,
				"utls":        map[string]any{"enabled": true, "fingerprint": nz(lst.String("fingerprint"), "chrome")},
				"reality": map[string]any{
					"enabled":    true,
					"public_key": lst.String("public_key"),
					"short_id":   first(lst.Strings("short_ids")),
				},
			},
		}
		if Vision(land.Profile) {
			ob["flow"] = "xtls-rprx-vision"
		}
		return ob, nil
	case ProfileVLESSXHTTP:
		sni := lst.String("sni")
		if sni == "" {
			sni = host
		}
		return map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      host,
			"server_port": land.Port,
			"uuid":        st.String("relay_uuid"),
			"tls":         singClientTLS(lst, sni),
			"transport": map[string]any{
				"type": "httpupgrade",
				"path": lst.String("path"),
			},
		}, nil
	case ProfileTrojanTLS:
		sni := lst.String("sni")
		if sni == "" {
			sni = host
		}
		return map[string]any{
			"type":        "trojan",
			"tag":         tag,
			"server":      host,
			"server_port": land.Port,
			"password":    st.String("relay_password"),
			"tls":         singClientTLS(lst, sni),
		}, nil
	case ProfileSS2022:
		return map[string]any{
			"type":        "shadowsocks",
			"tag":         tag,
			"server":      host,
			"server_port": land.Port,
			"method":      lst.String("method"),
			"password":    lst.String("server_password") + ":" + st.String("relay_password"),
		}, nil
	case ProfileSOCKS5:
		ob := map[string]any{
			"type":        "socks",
			"tag":         tag,
			"server":      host,
			"server_port": land.Port,
			"version":     "5",
		}
		if u := st.String("relay_username"); u != "" {
			ob["username"] = u
			ob["password"] = st.String("relay_password")
		}
		return ob, nil
	default:
		return nil, fmt.Errorf("不支持的落地协议 %s", land.Profile)
	}
}

func singClientTLS(st Settings, sni string) map[string]any {
	return map[string]any{
		"enabled":     true,
		"server_name": sni,
		"min_version": nz(st.String("min_version"), "1.3"),
		"alpn":        st.ALPN(),
		"utls":        map[string]any{"enabled": true, "fingerprint": nz(st.String("fingerprint"), "chrome")},
	}
}
