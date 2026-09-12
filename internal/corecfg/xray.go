package corecfg

import (
	"fmt"
	"strings"

	"liking/internal/db"
)

func buildXray(inbounds []*db.Inbound, clients map[int64][]*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound, apiPort int) (map[string]any, error) {
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
	// freedom here loops 127.0.0.1:10085 onto itself and opens tens of thousands of fds.
	rules = append(rules, map[string]any{
		"type": "field", "inboundTag": []string{"api"}, "outboundTag": "api",
	})

	used := false
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
			ob, obTag, err := xrayChainOutbound(in, byID)
			if err != nil {
				return nil, err
			}
			outs = append(outs, ob)
			rules = append(rules, map[string]any{
				"type": "field", "inboundTag": []string{tag}, "outboundTag": obTag,
			})
		} else {
			rules = append(rules, map[string]any{
				"type": "field", "inboundTag": []string{tag}, "outboundTag": "direct",
			})
		}
	}

	// Socks inbounds used as Mieru chain egress (mita -> local xray -> landing).
	for _, in := range inbounds {
		if in.Core != CoreMita || !in.Enabled || in.LineKind != "chain" {
			continue
		}
		ob, obTag, err := xrayChainOutbound(in, byID)
		if err != nil {
			return nil, err
		}
		used = true
		tag := socksTag(in.ID)
		ins = append(ins, map[string]any{
			"tag":      tag,
			"listen":   "127.0.0.1",
			"port":     socksPort(in.ID),
			"protocol": "socks",
			"settings": map[string]any{"udp": true, "auth": "noauth"},
		})
		outs = append(outs, ob)
		rules = append(rules, map[string]any{
			"type": "field", "inboundTag": []string{tag}, "outboundTag": obTag,
		})
	}

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
func socksPort(id int64) int      { return 20000 + int(id) }

func xrayInbound(in *db.Inbound, clients []*db.Client, certs map[int64]*db.Certificate, byID map[int64]*db.Inbound) (map[string]any, error) {
	st := ParseSettings(in.Settings)
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
	for _, in := range byID {
		if in == nil || in.ExitInboundID == nil || *in.ExitInboundID != landing.ID || !in.Enabled {
			continue
		}
		st := ParseSettings(in.Settings)
		email := fmt.Sprintf("relay.i%d", in.ID)
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
	}
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

func xrayChainOutbound(entry *db.Inbound, byID map[int64]*db.Inbound) (map[string]any, string, error) {
	if entry.ExitInboundID == nil {
		return nil, "", fmt.Errorf("链式线路 %s 没有落地", entry.Name)
	}
	land := byID[*entry.ExitInboundID]
	if land == nil {
		return nil, "", fmt.Errorf("链式线路 %s 的落地不存在", entry.Name)
	}
	st := ParseSettings(entry.Settings)
	lst := ParseSettings(land.Settings)
	host := land.ServerHost
	if host == "" {
		return nil, "", fmt.Errorf("落地 %s 未填写公开地址", land.Name)
	}
	tag := outboundTag(entry.ID)

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
		}, tag, nil
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
		}, tag, nil
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
		}, tag, nil
	default:
		return nil, "", fmt.Errorf("不支持的落地协议 %s", land.Profile)
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
