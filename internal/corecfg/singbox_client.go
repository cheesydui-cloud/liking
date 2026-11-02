package corecfg

import (
	"fmt"
	"strings"

	"liking/internal/db"
)

func SingboxOutbound(in *db.Inbound, c *db.Client) (map[string]any, error) {
	host := ShareHost(in)
	if host == "" {
		return nil, fmt.Errorf("服务器未填写公开地址")
	}
	st := ParseSettings(in.Settings)
	tag := in.Name
	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		ob := map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"uuid":        c.UUID,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": first(st.Strings("server_names")),
				"utls":        map[string]any{"enabled": true, "fingerprint": nz(st.String("fingerprint"), "chrome")},
				"reality": map[string]any{
					"enabled":    true,
					"public_key": st.String("public_key"),
					"short_id":   first(st.Strings("short_ids")),
				},
			},
		}
		if Vision(in.Profile) {
			ob["flow"] = "xtls-rprx-vision"
		}
		return ob, nil
	case ProfileVLESSXHTTP:
		sni := tlsServerName(st.String("sni"), host)
		return map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"uuid":        c.UUID,
			"tls":         singClientTLS(st, sni),
			"transport":   singXHTTPTransport(st.String("path"), st.String("mode"), st.String("host")),
		}, nil
	case ProfileTrojanTLS:
		sni := tlsServerName(st.String("sni"), host)
		return map[string]any{
			"type":        "trojan",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"password":    c.Password,
			"tls":         singClientTLS(st, sni),
		}, nil
	case ProfileSS2022:
		return map[string]any{
			"type":        "shadowsocks",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"method":      st.String("method"),
			"password":    st.String("server_password") + ":" + c.Password,
		}, nil
	case ProfileAnyTLS:
		sni := tlsServerName(st.String("sni"), host)
		return map[string]any{
			"type":        "anytls",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"password":    c.Password,
			"tls":         singClientTLS(st, sni),
		}, nil
	case ProfileSOCKS5:
		ob := map[string]any{
			"type":        "socks",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"version":     "5",
			"username":    clientSocksUser(c),
			"password":    c.Password,
		}
		return ob, nil
	case ProfileMieru:
		return nil, ErrSkip
	case ProfileWireGuard:
		if strings.TrimSpace(c.Password) == "" || wgHost(c.UUID) == "" || st.String("public_key") == "" {
			return nil, fmt.Errorf("WireGuard 客户端还没有密钥")
		}
		epTag := tag
		return map[string]any{
			"tag": epTag,
			"_endpoint": map[string]any{
				"type":        "wireguard",
				"tag":         epTag,
				"mtu":         st.Int("mtu", WGDefaultMTU),
				"address":     []string{strings.TrimSpace(c.UUID)},
				"private_key": strings.TrimSpace(c.Password),
				"peers": []any{map[string]any{
					"address":                       host,
					"port":                          in.Port,
					"public_key":                    st.String("public_key"),
					"allowed_ips":                   []string{"0.0.0.0/0", "::/0"},
					"persistent_keepalive_interval": st.Int("keepalive", WGDefaultKeepalive),
				}},
			},
		}, nil
	default:
		return nil, fmt.Errorf("未知协议")
	}
}

var ErrSkip = fmt.Errorf("skip")

// SingboxEndpoint pulls a WireGuard endpoint out of an outbound map.
// sing-box 1.14 uses the endpoint itself as the outbound tag.
func SingboxEndpoint(ob map[string]any) map[string]any {
	if ob == nil {
		return nil
	}
	ep, _ := ob["_endpoint"].(map[string]any)
	delete(ob, "_endpoint")
	if len(ep) == 0 {
		return nil
	}
	return ep
}
