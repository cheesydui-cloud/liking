package corecfg

import (
	"fmt"

	"liking/internal/db"
)

func buildSingbox(inbounds []*db.Inbound, clients map[int64][]*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound, apiPort int, speeds map[int64]int64, denies map[int64]SiteDeny) (map[string]any, error) {
	var ins []any
	var outs []any
	used := false

	outs = append(outs, map[string]any{"type": "direct", "tag": "direct"})

	limitOuts := map[uint32]struct{}{}
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
			continue
		}
		if !directThrottle(in) {
			continue
		}
		for _, c := range clients[in.ID] {
			mark, ok := clientSpeedMark(c, speeds)
			if !ok {
				continue
			}
			if _, seen := limitOuts[mark]; seen {
				continue
			}
			limitOuts[mark] = struct{}{}
			outs = append(outs, map[string]any{
				"type":         "direct",
				"tag":          limitTag(mark),
				"routing_mark": mark,
			})
		}
	}

	if !used {
		return nil, nil
	}

	// Deny, then chain, then speed. Chain catch-all would otherwise steal denied users.
	var chainRules []any
	var speedRules []any
	for _, in := range inbounds {
		if in.Core != CoreSingbox || !in.Enabled {
			continue
		}
		if in.LineKind == "chain" {
			chainRules = append(chainRules, map[string]any{
				"inbound":  []string{inboundTag(in.ID)},
				"outbound": outboundTag(in.ID),
			})
			continue
		}
		if !directThrottle(in) {
			continue
		}
		tag := inboundTag(in.ID)
		for _, c := range clients[in.ID] {
			mark, ok := clientSpeedMark(c, speeds)
			if !ok {
				continue
			}
			speedRules = append(speedRules, map[string]any{
				"inbound":   []string{tag},
				"auth_user": []string{c.Email},
				"outbound":  limitTag(mark),
			})
		}
	}
	routeRules := singSiteDenyRules(collectSiteDenyGroups(inbounds, clients, denies, CoreSingbox))
	routeRules = append(routeRules, chainRules...)
	routeRules = append(routeRules, speedRules...)
	final := "direct"

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
		"type":                       "anytls",
		"tag":                        inboundTag(in.ID),
		"listen":                     in.Listen,
		"listen_port":                in.Port,
		"users":                      users,
		"tls":                        tls,
		"sniff":                      true,
		"sniff_override_destination": false,
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
		"type":                       "socks",
		"tag":                        inboundTag(in.ID),
		"listen":                     in.Listen,
		"listen_port":                in.Port,
		"users":                      users,
		"sniff":                      true,
		"sniff_override_destination": false,
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
	if p.Share != nil {
		return singShareOutbound(p.Tag, p.Share)
	}
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

func singShareOutbound(tag string, t *ShareTarget) (map[string]any, error) {
	if t == nil {
		return nil, fmt.Errorf("出口链接无效")
	}
	if t.Host == "" || t.Port < 1 {
		return nil, fmt.Errorf("出口链接缺少主机或端口")
	}
	switch t.Scheme {
	case "socks", "socks5", "socks5h":
		ob := map[string]any{
			"type":        "socks",
			"tag":         tag,
			"server":      t.Host,
			"server_port": t.Port,
			"version":     "5",
		}
		if t.User != "" || t.Password != "" {
			ob["username"] = t.User
			ob["password"] = t.Password
		}
		return ob, nil
	case "ss":
		if t.Method == "" || t.Password == "" {
			return nil, fmt.Errorf("SS 链接缺少加密或密码")
		}
		return map[string]any{
			"type":        "shadowsocks",
			"tag":         tag,
			"server":      t.Host,
			"server_port": t.Port,
			"method":      t.Method,
			"password":    t.Password,
		}, nil
	case "trojan":
		if t.Password == "" {
			return nil, fmt.Errorf("Trojan 链接缺少密码")
		}
		ob := map[string]any{
			"type":        "trojan",
			"tag":         tag,
			"server":      t.Host,
			"server_port": t.Port,
			"password":    t.Password,
		}
		if tls := singShareTLS(t); tls != nil {
			ob["tls"] = tls
		}
		if tr := singShareTransport(t); tr != nil {
			ob["transport"] = tr
		}
		return ob, nil
	case "vless":
		if t.UUID == "" {
			return nil, fmt.Errorf("VLESS 链接缺少 UUID")
		}
		ob := map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      t.Host,
			"server_port": t.Port,
			"uuid":        t.UUID,
		}
		if t.Flow != "" {
			ob["flow"] = t.Flow
		}
		if tls := singShareTLS(t); tls != nil {
			ob["tls"] = tls
		}
		if tr := singShareTransport(t); tr != nil {
			ob["transport"] = tr
		}
		return ob, nil
	default:
		return nil, fmt.Errorf("暂不支持 %s 出口", t.Scheme)
	}
}

func singShareTLS(t *ShareTarget) map[string]any {
	if t == nil {
		return nil
	}
	sec := t.Security
	if sec == "" && t.Scheme == "trojan" {
		sec = "tls"
	}
	if sec != "tls" && sec != "reality" {
		return nil
	}
	sni := t.SNI
	if sni == "" {
		sni = t.Host
	}
	st := Settings{}
	if t.Fingerprint != "" {
		st["fingerprint"] = t.Fingerprint
	}
	if len(t.ALPN) > 0 {
		st["alpn"] = t.ALPN
	}
	tls := singClientTLS(st, sni)
	if t.AllowInsecure {
		tls["insecure"] = true
	}
	if sec == "reality" {
		tls["reality"] = map[string]any{
			"enabled":    true,
			"public_key": t.PublicKey,
			"short_id":   t.ShortID,
		}
	}
	return tls
}

func singShareTransport(t *ShareTarget) map[string]any {
	if t == nil {
		return nil
	}
	netw := t.Network
	if netw == "" || netw == "tcp" {
		return nil
	}
	switch netw {
	case "ws":
		tr := map[string]any{"type": "ws", "path": nz(t.Path, "/")}
		if t.HostHeader != "" {
			tr["headers"] = map[string]any{"Host": t.HostHeader}
		}
		return tr
	case "grpc":
		return map[string]any{"type": "grpc", "service_name": t.ServiceName}
	case "httpupgrade", "xhttp":
		tr := map[string]any{"type": "httpupgrade", "path": nz(t.Path, "/")}
		if t.HostHeader != "" {
			tr["host"] = t.HostHeader
		}
		return tr
	case "http":
		tr := map[string]any{"type": "http"}
		if t.Path != "" {
			tr["path"] = t.Path
		}
		if t.HostHeader != "" {
			tr["host"] = []any{t.HostHeader}
		}
		return tr
	default:
		return nil
	}
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
