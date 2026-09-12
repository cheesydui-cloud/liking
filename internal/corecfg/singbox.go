package corecfg

import (
	"fmt"

	"liking/internal/db"
)

func buildSingbox(inbounds []*db.Inbound, clients map[int64][]*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound) (map[string]any, error) {
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
			ob, err := singChainOutbound(in, byID)
			if err != nil {
				return nil, err
			}
			outs = append(outs, ob)
		}
	}

	if !used {
		return nil, nil
	}

	// Route: each anytls inbound uses a detour outbound when chained.
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
	return cfg, nil
}

func singInbound(in *db.Inbound, clients []*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound) (map[string]any, error) {
	if in.Profile != ProfileAnyTLS {
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
	for _, other := range byID {
		if other == nil || other.ExitInboundID == nil || *other.ExitInboundID != in.ID || !other.Enabled {
			continue
		}
		pw := ParseSettings(other.Settings).String("relay_password")
		if pw == "" {
			continue
		}
		users = append(users, map[string]any{"name": fmt.Sprintf("relay.i%d", other.ID), "password": pw})
	}
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

func singChainOutbound(entry *db.Inbound, byID map[int64]*db.Inbound) (map[string]any, error) {
	if entry.ExitInboundID == nil {
		return nil, fmt.Errorf("链式线路 %s 没有落地", entry.Name)
	}
	land := byID[*entry.ExitInboundID]
	if land == nil {
		return nil, fmt.Errorf("链式线路 %s 的落地不存在", entry.Name)
	}
	st := ParseSettings(entry.Settings)
	lst := ParseSettings(land.Settings)
	host := land.ServerHost
	tag := outboundTag(entry.ID)
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
