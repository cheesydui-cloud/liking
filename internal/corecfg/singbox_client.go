package corecfg

import (
	"fmt"

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
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		return map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"uuid":        c.UUID,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": sni,
			},
			"transport": map[string]any{
				"type": "httpupgrade",
				"path": st.String("path"),
			},
		}, nil
	case ProfileTrojanTLS:
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		return map[string]any{
			"type":        "trojan",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"password":    c.Password,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": sni,
			},
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
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		return map[string]any{
			"type":        "anytls",
			"tag":         tag,
			"server":      host,
			"server_port": in.Port,
			"password":    c.Password,
			"tls": map[string]any{
				"enabled":     true,
				"server_name": sni,
			},
		}, nil
	case ProfileMieru:
		return nil, ErrSkip
	default:
		return nil, fmt.Errorf("未知协议")
	}
}

var ErrSkip = fmt.Errorf("skip")
