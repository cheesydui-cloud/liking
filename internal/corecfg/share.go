package corecfg

import (
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"liking/internal/db"
)

func ShareHost(in *db.Inbound) string {
	if in == nil {
		return ""
	}
	if h := strings.TrimSpace(in.ServerHost); h != "" {
		return h
	}
	return strings.TrimSpace(in.ConnectIP)
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
		if spx := st.String("spider_x"); spx != "" {
			q.Set("spx", spx)
		}
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
		q.Set("fp", nz(st.String("fingerprint"), "chrome"))
		q.Set("alpn", strings.Join(st.ALPN(), ","))
		if h := st.String("host"); h != "" {
			q.Set("host", h)
		}
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
		q.Set("fp", nz(st.String("fingerprint"), "chrome"))
		q.Set("alpn", strings.Join(st.ALPN(), ","))
		return fmt.Sprintf("trojan://%s@%s?%s#%s", url.QueryEscape(c.Password), hp, q.Encode(), name), nil
	case ProfileSS2022:
		method := st.String("method")
		combined := st.String("server_password") + ":" + c.Password
		userinfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + combined))
		return fmt.Sprintf("ss://%s@%s#%s", userinfo, hp, name), nil
	case ProfileAnyTLS:
		q := url.Values{}
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		q.Set("sni", sni)
		q.Set("fp", nz(st.String("fingerprint"), "chrome"))
		q.Set("alpn", strings.Join(st.ALPN(), ","))
		return fmt.Sprintf("anytls://%s@%s?%s#%s", url.QueryEscape(c.Password), hp, q.Encode(), name), nil
	case ProfileMieru:
		// Shadowrocket: mierus://user:pass@host?udp=1&port=39198&profile=default
		user := c.Username
		if user == "" {
			user = c.Email
		}
		udp := "0"
		tr := strings.ToUpper(st.String("transport"))
		if tr == "UDP" || tr == "BOTH" {
			udp = "1"
		}
		userinfo := url.UserPassword(user, c.Password).String()
		return fmt.Sprintf("mierus://%s@%s?udp=%s&port=%s&profile=default#%s", userinfo, shareHostOnly(host), udp, strconv.Itoa(in.Port), name), nil
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

func shareHostOnly(host string) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		return "[" + host + "]"
	}
	return host
}
