package corecfg

import (
	"fmt"
	"net/url"
	"strings"

	"liking/internal/db"
)

func ShareHost(in *db.Inbound) string {
	if in.ServerHost != "" {
		return in.ServerHost
	}
	return ""
}

func ShareURI(in *db.Inbound, c *db.Client) (string, error) {
	if in == nil || c == nil {
		return "", fmt.Errorf("missing inbound/client")
	}
	host := ShareHost(in)
	if host == "" {
		return "", fmt.Errorf("服务器未填写公开地址")
	}
	st := ParseSettings(in.Settings)
	name := url.QueryEscape(in.Name)
	hp := hostPort(host, in.Port)

	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		q := url.Values{}
		q.Set("encryption", "none")
		q.Set("security", "reality")
		q.Set("type", "tcp")
		q.Set("sni", first(st.Strings("server_names")))
		q.Set("fp", nz(st.String("fingerprint"), "chrome"))
		q.Set("pbk", st.String("public_key"))
		q.Set("sid", first(st.Strings("short_ids")))
		if Vision(in.Profile) {
			q.Set("flow", "xtls-rprx-vision")
		}
		return fmt.Sprintf("vless://%s@%s?%s#%s", c.UUID, hp, q.Encode(), name), nil
	case ProfileVLESSXHTTP:
		q := url.Values{}
		q.Set("encryption", "none")
		q.Set("security", "tls")
		q.Set("type", "xhttp")
		q.Set("path", st.String("path"))
		q.Set("mode", nz(st.String("mode"), "auto"))
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		q.Set("sni", sni)
		return fmt.Sprintf("vless://%s@%s?%s#%s", c.UUID, hp, q.Encode(), name), nil
	case ProfileTrojanTLS:
		q := url.Values{}
		q.Set("security", "tls")
		q.Set("type", "tcp")
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		q.Set("sni", sni)
		return fmt.Sprintf("trojan://%s@%s?%s#%s", url.QueryEscape(c.Password), hp, q.Encode(), name), nil
	case ProfileSS2022:
		method := st.String("method")
		userinfo := url.UserPassword(method, st.String("server_password")+":"+c.Password).String()
		return fmt.Sprintf("ss://%s@%s#%s", userinfo, hp, name), nil
	case ProfileAnyTLS:
		q := url.Values{}
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		q.Set("sni", sni)
		return fmt.Sprintf("anytls://%s@%s?%s#%s", url.QueryEscape(c.Password), hp, q.Encode(), name), nil
	case ProfileMieru:
		q := url.Values{}
		tr := st.String("transport")
		if tr == "" {
			tr = "TCP"
		}
		if tr == "BOTH" {
			tr = "TCP"
		}
		q.Set("transport", tr)
		user := c.Username
		if user == "" {
			user = c.Email
		}
		return fmt.Sprintf("mieru://%s:%s@%s?%s#%s", url.PathEscape(user), url.PathEscape(c.Password), hp, q.Encode(), name), nil
	default:
		return "", fmt.Errorf("未知协议")
	}
}

func nz(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}
