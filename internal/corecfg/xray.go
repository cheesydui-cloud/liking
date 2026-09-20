package corecfg

import (
	"fmt"
	"strings"

	"liking/internal/db"
)

func buildXray(inbounds []*db.Inbound, clients map[int64][]*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound, apiPort int, speeds map[int64]int64, denies map[int64]SiteDeny) (map[string]any, error) {
	var ins []any
	var outs []any
	var rules []any

	ins = append(ins, map[string]any{
		"tag":      "api",
		"listen":   "127.0.0.1",
		"port":     apiPort,
		"protocol": "dokodemo-door",
		"settings": map[string]any{"address": "127.0.0.1"},
	})
	outs = append(outs,
		map[string]any{"tag": "direct", "protocol": "freedom"},
		map[string]any{"tag": "block", "protocol": "blackhole"},
	)
	// API inbound must route to the api module tag, not freedom.
	// freedom here loops 127.0.0.1:API onto itself and opens tens of thousands of fds.
	rules = append(rules, map[string]any{
		"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api",
	})

	used := false
	limitOuts := map[uint32]struct{}{}
	var chainRules []any
	var speedRules []any
	var directRules []any
	for _, in := range inbounds {
		if in.Core != CoreXray || !in.Enabled {
			continue
		}
		obj, err := xrayInbound(in, clients[in.ID], certs, byID)
		if err != nil {
			return nil, err
		}
		if obj == nil {
			continue
		}
		used = true
		tag := inboundTag(in.ID)
		obj["tag"] = tag
		ins = append(ins, obj)

		if in.LineKind == "chain" {
			obs, obTag, err := xrayChainOutbounds(in, byID)
			if err != nil {
				return nil, err
			}
			outs = append(outs, obs...)
			chainRules = append(chainRules, map[string]any{
				"type": "field", "inboundTag": []string{tag}, "outboundTag": obTag,
			})
			continue
		}
		if directThrottle(in) {
			for _, c := range clients[in.ID] {
				mark, ok := clientSpeedMark(c, speeds)
				if !ok {
					continue
				}
				if _, seen := limitOuts[mark]; !seen {
					limitOuts[mark] = struct{}{}
					outs = append(outs, xrayLimitFreedom(mark))
				}
				speedRules = append(speedRules, map[string]any{
					"type":        "field",
					"inboundTag":  []string{tag},
					"user":        []string{c.Email},
					"outboundTag": limitTag(mark),
				})
			}
		}
		directRules = append(directRules, map[string]any{
			"type": "field", "inboundTag": []string{tag}, "outboundTag": "direct",
		})
	}

	// Socks inbounds used as Mieru chain egress (mita -> local xray -> landing).
	for _, in := range inbounds {
		if in.Core != CoreMita || !in.Enabled || in.LineKind != "chain" {
			continue
		}
		obs, obTag, err := xrayChainOutbounds(in, byID)
		if err != nil {
			return nil, err
		}
		used = true
		tag := socksTag(in.ID)
		ins = append(ins, map[string]any{
			"tag":      tag,
			"listen":   "127.0.0.1",
			"port":     SocksPort(in.ID),
			"protocol": "socks",
			"settings": map[string]any{"udp": true, "auth": "noauth"},
		})
		outs = append(outs, obs...)
		chainRules = append(chainRules, map[string]any{
			"type": "field", "inboundTag": []string{tag}, "outboundTag": obTag,
		})
	}

	// Deny, then allow-pass, allow-block, then chain. Catch-all would steal filtered users.
	pass, block := collectSiteAllowGroups(inbounds, clients, denies, speeds, CoreXray)
	rules = append(rules, xraySiteDenyRules(collectSiteDenyGroups(inbounds, clients, denies, CoreXray))...)
	rules = append(rules, xraySiteAllowPassRules(pass)...)
	rules = append(rules, xraySiteAllowBlockRules(block)...)
	rules = append(rules, chainRules...)
	rules = append(rules, speedRules...)
	rules = append(rules, directRules...)

	if !used {
		return nil, nil
	}

	return map[string]any{
		"log": map[string]any{
			"access":   "none",
			"loglevel": "warning",
		},
		"stats": map[string]any{},
		"api":   map[string]any{"tag": "api", "services": []string{"StatsService"}},
		"policy": map[string]any{
			"levels": map[string]any{"0": map[string]any{"statsUserUplink": true, "statsUserDownlink": true}},
			"system": map[string]any{"statsInboundUplink": true, "statsInboundDownlink": true},
		},
		"inbounds":  ins,
		"outbounds": outs,
		"routing":   map[string]any{"domainStrategy": "AsIs", "rules": rules},
	}, nil
}

func inboundTag(id int64) string  { return fmt.Sprintf("in-%d", id) }
func outboundTag(id int64) string { return fmt.Sprintf("ob-%d", id) }
func socksTag(id int64) string    { return fmt.Sprintf("socks-%d", id) }

func xrayLimitFreedom(mark uint32) map[string]any {
	return map[string]any{
		"tag":      limitTag(mark),
		"protocol": "freedom",
		"streamSettings": map[string]any{
			"sockopt": map[string]any{"mark": mark},
		},
	}
}

func xrayInbound(in *db.Inbound, clients []*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound) (map[string]any, error) {
	st := ParseSettings(in.Settings)
	if in.Profile == ProfilePortForward {
		netw := st.String("network")
		if netw == "" {
			netw = "tcp"
		}
		return map[string]any{
			"listen":   in.Listen,
			"port":     in.Port,
			"protocol": "dokodemo-door",
			"settings": map[string]any{
				"address": st.String("dest_host"),
				"port":    st.Int("dest_port", 0),
				"network": netw,
			},
		}, nil
	}
	users := xrayUsers(in, clients, st)
	users = append(users, relayUsers(in, byID)...)
	if len(users) == 0 {
		// Keep the inbound so the port is reserved; xray allows empty clients.
		users = []any{}
	}

	obj := map[string]any{
		"listen": in.Listen,
		"port":   in.Port,
	}

	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP:
		obj["protocol"] = "vless"
		obj["settings"] = map[string]any{"clients": users, "decryption": "none"}
	case ProfileTrojanTLS:
		obj["protocol"] = "trojan"
		obj["settings"] = map[string]any{"clients": users}
	case ProfileSS2022:
		obj["protocol"] = "shadowsocks"
		obj["settings"] = map[string]any{
			"method":   st.String("method"),
			"password": st.String("server_password"),
			"clients":  users,
			"network":  "tcp,udp",
		}
	default:
		return nil, nil
	}

	stream, err := xrayStream(in, st, certs)
	if err != nil {
		return nil, err
	}
	if stream != nil {
		obj["streamSettings"] = stream
	}
	obj["sniffing"] = xraySniffing()
	return obj, nil
}

func xrayUsers(in *db.Inbound, clients []*db.Client, st Settings) []any {
	var users []any
	for _, c := range clients {
		if c == nil || !c.Enabled {
			continue
		}
		switch in.Profile {
		case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP:
			u := map[string]any{"id": c.UUID, "email": c.Email}
			if Vision(in.Profile) {
				u["flow"] = "xtls-rprx-vision"
			}
			users = append(users, u)
		case ProfileTrojanTLS:
			users = append(users, map[string]any{"password": c.Password, "email": c.Email})
		case ProfileSS2022:
			users = append(users, map[string]any{"password": c.Password, "email": c.Email})
		}
	}
	_ = st
	return users
}

func relayUsers(landing *db.Inbound, byID map[int64]*db.Inbound) []any {
	var users []any
	forEachRelayTo(landing.ID, byID, func(email string, st Settings) {
		switch landing.Profile {
		case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP:
			if id := st.String("relay_uuid"); id != "" {
				u := map[string]any{"id": id, "email": email}
				if Vision(landing.Profile) {
					u["flow"] = "xtls-rprx-vision"
				}
				users = append(users, u)
			}
		case ProfileTrojanTLS:
			if pw := st.String("relay_password"); pw != "" {
				users = append(users, map[string]any{"password": pw, "email": email})
			}
		case ProfileSS2022:
			if pw := st.String("relay_password"); pw != "" {
				users = append(users, map[string]any{"password": pw, "email": email})
			}
		}
	})
	return users
}

func xrayStream(in *db.Inbound, st Settings, certs map[int64]*db.Certificate) (map[string]any, error) {
	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		sni := st.Strings("server_names")
		return map[string]any{
			"network":  "tcp",
			"security": "reality",
			"realitySettings": map[string]any{
				"show":        false,
				"dest":        st.String("dest"),
				"xver":        st.Int("xver", 0),
				"serverNames": sni,
				"privateKey":  st.String("private_key"),
				"shortIds":    st.Strings("short_ids"),
			},
		}, nil
	case ProfileVLESSXHTTP:
		tls, err := xrayTLS(in, st, certs)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"network":       "xhttp",
			"security":      "tls",
			"tlsSettings":   tls,
			"xhttpSettings": xrayXHTTPSettings(st),
		}, nil
	case ProfileTrojanTLS:
		tls, err := xrayTLS(in, st, certs)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"network":     "tcp",
			"security":    "tls",
			"tlsSettings": tls,
		}, nil
	default:
		return nil, nil
	}
}

func xrayTLS(in *db.Inbound, st Settings, certs map[int64]*db.Certificate) (map[string]any, error) {
	if in.CertID == nil {
		return nil, fmt.Errorf("入站 %s 需要 TLS 证书", in.Name)
	}
	c := certs[*in.CertID]
	if c == nil {
		return nil, fmt.Errorf("入站 %s 的证书不存在", in.Name)
	}
	tls := map[string]any{
		"certificates": []any{
			map[string]any{
				"certificate": pemLines(c.CertPEM),
				"key":         pemLines(c.KeyPEM),
			},
		},
		"minVersion":       nz(st.String("min_version"), "1.3"),
		"alpn":             st.ALPN(),
		"rejectUnknownSni": st.Bool("reject_unknown_sni", true),
	}
	if sni := st.String("sni"); sni != "" {
		tls["serverName"] = sni
	}
	return tls, nil
}

func xrayClientTLS(st Settings, sni string) map[string]any {
	return map[string]any{
		"serverName":    sni,
		"allowInsecure": false,
		"fingerprint":   nz(st.String("fingerprint"), "chrome"),
		"alpn":          st.ALPN(),
	}
}

func xrayXHTTPSettings(st Settings) map[string]any {
	xh := map[string]any{
		"path": st.String("path"),
		"mode": st.String("mode"),
	}
	if host := strings.TrimSpace(st.String("host")); host != "" {
		xh["host"] = host
	}
	return xh
}

func xraySocksOutbound(tag string, t *SocksTarget) (map[string]any, string, error) {
	if t == nil || t.Host == "" {
		return nil, "", fmt.Errorf("SK5 缺少主机")
	}
	srv := map[string]any{
		"address": t.Host,
		"port":    t.Port,
	}
	if t.User != "" || t.Pass != "" {
		srv["users"] = []any{map[string]any{"user": t.User, "pass": t.Pass}}
	}
	return map[string]any{
		"tag":      tag,
		"protocol": "socks",
		"settings": map[string]any{"servers": []any{srv}},
	}, tag, nil
}

func withDialerProxy(ob map[string]any, via string) {
	if ob == nil || via == "" {
		return
	}
	stream, _ := ob["streamSettings"].(map[string]any)
	if stream == nil {
		stream = map[string]any{}
		ob["streamSettings"] = stream
	}
	sockopt, _ := stream["sockopt"].(map[string]any)
	if sockopt == nil {
		sockopt = map[string]any{}
		stream["sockopt"] = sockopt
	}
	sockopt["dialerProxy"] = via
}

func xrayChainOutbounds(entry *db.Inbound, byID map[int64]*db.Inbound) ([]any, string, error) {
	path, err := ChainPath(entry, byID)
	if err != nil {
		return nil, "", err
	}
	if len(path) == 0 {
		return nil, "", fmt.Errorf("链式线路 %s 没有落地", entry.Name)
	}
	var outs []any
	var prev string
	for _, p := range path {
		ob, err := xrayPathOutbound(p)
		if err != nil {
			return nil, "", err
		}
		if prev != "" {
			withDialerProxy(ob, prev)
		}
		outs = append(outs, ob)
		prev = p.Tag
	}
	return outs, path[len(path)-1].Tag, nil
}

func xrayPathOutbound(p PathHop) (map[string]any, error) {
	if p.Share != nil {
		return xrayShareOutbound(p.Tag, p.Share)
	}
	if p.Socks != nil {
		ob, _, err := xraySocksOutbound(p.Tag, p.Socks)
		return ob, err
	}
	if p.Land == nil {
		return nil, fmt.Errorf("没有落地")
	}
	return xrayLandOutbound(p.Tag, p.Land, p.Cred)
}

func xrayShareOutbound(tag string, t *ShareTarget) (map[string]any, error) {
	if t == nil {
		return nil, fmt.Errorf("出口链接无效")
	}
	if t.Host == "" || t.Port < 1 {
		return nil, fmt.Errorf("出口链接缺少主机或端口")
	}
	switch t.Scheme {
	case "socks", "socks5", "socks5h":
		ob, _, err := xraySocksOutbound(tag, t.SocksTarget())
		return ob, err
	case "ss":
		if t.Method == "" || t.Password == "" {
			return nil, fmt.Errorf("SS 链接缺少加密或密码")
		}
		return map[string]any{
			"tag":      tag,
			"protocol": "shadowsocks",
			"settings": map[string]any{
				"servers": []any{map[string]any{
					"address":  t.Host,
					"port":     t.Port,
					"method":   t.Method,
					"password": t.Password,
				}},
			},
		}, nil
	case "trojan":
		if t.Password == "" {
			return nil, fmt.Errorf("Trojan 链接缺少密码")
		}
		ob := map[string]any{
			"tag":      tag,
			"protocol": "trojan",
			"settings": map[string]any{
				"servers": []any{map[string]any{
					"address":  t.Host,
					"port":     t.Port,
					"password": t.Password,
				}},
			},
		}
		if stream := xrayShareStream(t); stream != nil {
			ob["streamSettings"] = stream
		}
		return ob, nil
	case "vless":
		if t.UUID == "" {
			return nil, fmt.Errorf("VLESS 链接缺少 UUID")
		}
		user := map[string]any{"id": t.UUID, "encryption": nz(t.Encryption, "none")}
		if t.Flow != "" {
			user["flow"] = t.Flow
		}
		ob := map[string]any{
			"tag":      tag,
			"protocol": "vless",
			"settings": map[string]any{
				"vnext": []any{map[string]any{
					"address": t.Host,
					"port":    t.Port,
					"users":   []any{user},
				}},
			},
		}
		if stream := xrayShareStream(t); stream != nil {
			ob["streamSettings"] = stream
		}
		return ob, nil
	default:
		return nil, fmt.Errorf("暂不支持 %s 出口", t.Scheme)
	}
}

func xrayShareStream(t *ShareTarget) map[string]any {
	if t == nil {
		return nil
	}
	netw := t.Network
	if netw == "" {
		netw = "tcp"
	}
	sec := t.Security
	if sec == "" && t.Scheme == "trojan" {
		sec = "tls"
	}
	if sec == "none" && netw == "tcp" && t.Scheme != "trojan" {
		return nil
	}
	stream := map[string]any{"network": netw}
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
	switch sec {
	case "tls":
		tls := xrayClientTLS(st, sni)
		if t.AllowInsecure {
			tls["allowInsecure"] = true
		}
		stream["security"] = "tls"
		stream["tlsSettings"] = tls
	case "reality":
		rs := map[string]any{
			"serverName":  sni,
			"fingerprint": nz(t.Fingerprint, "chrome"),
			"publicKey":   t.PublicKey,
			"shortId":     t.ShortID,
		}
		if t.SpiderX != "" {
			rs["spiderX"] = t.SpiderX
		}
		stream["security"] = "reality"
		stream["realitySettings"] = rs
	}
	switch netw {
	case "ws":
		ws := map[string]any{"path": nz(t.Path, "/")}
		if t.HostHeader != "" {
			ws["headers"] = map[string]any{"Host": t.HostHeader}
		}
		stream["wsSettings"] = ws
	case "grpc":
		stream["grpcSettings"] = map[string]any{"serviceName": t.ServiceName}
	case "xhttp":
		xh := map[string]any{"path": nz(t.Path, "/"), "mode": nz(t.Mode, "auto")}
		if t.HostHeader != "" {
			xh["host"] = t.HostHeader
		}
		stream["xhttpSettings"] = xh
	case "httpupgrade":
		hu := map[string]any{"path": nz(t.Path, "/")}
		if t.HostHeader != "" {
			hu["host"] = t.HostHeader
		}
		stream["httpupgradeSettings"] = hu
	case "http":
		hs := map[string]any{}
		if t.Path != "" {
			hs["path"] = t.Path
		}
		if t.HostHeader != "" {
			hs["host"] = []any{t.HostHeader}
		}
		stream["httpSettings"] = hs
	}
	return stream
}

func xrayLandOutbound(tag string, land *db.Inbound, st Settings) (map[string]any, error) {
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
	case ProfileVLESSReality, ProfileVLESSRealityVision, ProfileVLESSXHTTP:
		user := map[string]any{"id": st.String("relay_uuid"), "encryption": "none"}
		if Vision(land.Profile) {
			user["flow"] = "xtls-rprx-vision"
		}
		stream := map[string]any{}
		if land.Profile == ProfileVLESSXHTTP {
			sni := lst.String("sni")
			if sni == "" {
				sni = host
			}
			stream = map[string]any{
				"network":       "xhttp",
				"security":      "tls",
				"tlsSettings":   xrayClientTLS(lst, sni),
				"xhttpSettings": xrayXHTTPSettings(lst),
			}
		} else {
			sni := first(lst.Strings("server_names"))
			sid := first(lst.Strings("short_ids"))
			rs := map[string]any{
				"serverName":  sni,
				"fingerprint": nz(lst.String("fingerprint"), "chrome"),
				"publicKey":   lst.String("public_key"),
				"shortId":     sid,
			}
			if spx := lst.String("spider_x"); spx != "" {
				rs["spiderX"] = spx
			}
			stream = map[string]any{
				"network":         "tcp",
				"security":        "reality",
				"realitySettings": rs,
			}
		}
		return map[string]any{
			"tag":      tag,
			"protocol": "vless",
			"settings": map[string]any{
				"vnext": []any{map[string]any{
					"address": host,
					"port":    land.Port,
					"users":   []any{user},
				}},
			},
			"streamSettings": stream,
		}, nil
	case ProfileTrojanTLS:
		sni := lst.String("sni")
		if sni == "" {
			sni = host
		}
		return map[string]any{
			"tag":      tag,
			"protocol": "trojan",
			"settings": map[string]any{
				"servers": []any{map[string]any{
					"address":  host,
					"port":     land.Port,
					"password": st.String("relay_password"),
				}},
			},
			"streamSettings": map[string]any{
				"network":     "tcp",
				"security":    "tls",
				"tlsSettings": xrayClientTLS(lst, sni),
			},
		}, nil
	case ProfileSS2022:
		return map[string]any{
			"tag":      tag,
			"protocol": "shadowsocks",
			"settings": map[string]any{
				"servers": []any{map[string]any{
					"address":  host,
					"port":     land.Port,
					"method":   lst.String("method"),
					"password": lst.String("server_password") + ":" + st.String("relay_password"),
				}},
			},
		}, nil
	case ProfileSOCKS5:
		ob, _, err := xraySocksOutbound(tag, &SocksTarget{
			Host: host,
			Port: land.Port,
			User: st.String("relay_username"),
			Pass: st.String("relay_password"),
		})
		return ob, err
	default:
		return nil, fmt.Errorf("不支持的落地协议 %s", land.Profile)
	}
}

func first(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

func hostPort(host string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}
