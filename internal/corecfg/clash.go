package corecfg

import (
	"fmt"
	"strconv"
	"strings"

	"liking/internal/db"
)

func ClashProxyYAML(in *db.Inbound, c *db.Client) (string, string, error) {
	host := ShareHost(in)
	if host == "" {
		return "", "", fmt.Errorf("服务器未填写公开地址")
	}
	st := ParseSettings(in.Settings)
	name := in.Name
	var b strings.Builder
	fmt.Fprintf(&b, "  - name: %s\n    server: %s\n    port: %d\n", yq(name), yq(host), in.Port)

	switch in.Profile {
	case ProfileVLESSReality, ProfileVLESSRealityVision:
		b.WriteString("    type: vless\n")
		fmt.Fprintf(&b, "    uuid: %s\n", yq(c.UUID))
		b.WriteString("    network: tcp\n    tls: true\n    udp: true\n")
		if Vision(in.Profile) {
			b.WriteString("    flow: xtls-rprx-vision\n")
		}
		fmt.Fprintf(&b, "    servername: %s\n", yq(first(st.Strings("server_names"))))
		fmt.Fprintf(&b, "    client-fingerprint: %s\n", yq(nz(st.String("fingerprint"), "chrome")))
		b.WriteString("    reality-opts:\n")
		fmt.Fprintf(&b, "      public-key: %s\n", yq(st.String("public_key")))
		fmt.Fprintf(&b, "      short-id: %s\n", yq(first(st.Strings("short_ids"))))
	case ProfileVLESSXHTTP:
		b.WriteString("    type: vless\n")
		fmt.Fprintf(&b, "    uuid: %s\n", yq(c.UUID))
		b.WriteString("    network: xhttp\n    tls: true\n    udp: true\n")
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		fmt.Fprintf(&b, "    servername: %s\n", yq(sni))
		fmt.Fprintf(&b, "    client-fingerprint: %s\n", yq(nz(st.String("fingerprint"), "chrome")))
		writeClashALPN(&b, st)
		b.WriteString("    xhttp-opts:\n")
		fmt.Fprintf(&b, "      path: %s\n", yq(st.String("path")))
	case ProfileTrojanTLS:
		b.WriteString("    type: trojan\n")
		fmt.Fprintf(&b, "    password: %s\n", yq(c.Password))
		b.WriteString("    udp: true\n")
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		fmt.Fprintf(&b, "    sni: %s\n", yq(sni))
		fmt.Fprintf(&b, "    client-fingerprint: %s\n", yq(nz(st.String("fingerprint"), "chrome")))
		writeClashALPN(&b, st)
	case ProfileSS2022:
		b.WriteString("    type: ss\n")
		fmt.Fprintf(&b, "    cipher: %s\n", yq(st.String("method")))
		fmt.Fprintf(&b, "    password: %s\n", yq(st.String("server_password")+":"+c.Password))
		b.WriteString("    udp: true\n")
	case ProfileAnyTLS:
		b.WriteString("    type: anytls\n")
		fmt.Fprintf(&b, "    password: %s\n", yq(c.Password))
		sni := st.String("sni")
		if sni == "" {
			sni = host
		}
		fmt.Fprintf(&b, "    sni: %s\n", yq(sni))
		fmt.Fprintf(&b, "    client-fingerprint: %s\n", yq(nz(st.String("fingerprint"), "chrome")))
		writeClashALPN(&b, st)
		b.WriteString("    udp: true\n")
	case ProfileMieru:
		b.WriteString("    type: mieru\n")
		user := c.Username
		if user == "" {
			user = c.Email
		}
		fmt.Fprintf(&b, "    username: %s\n", yq(user))
		fmt.Fprintf(&b, "    password: %s\n", yq(c.Password))
		tr := st.String("transport")
		if tr == "" || tr == "BOTH" {
			tr = "UDP"
		}
		fmt.Fprintf(&b, "    transport: %s\n", yq(tr))
	default:
		return "", "", fmt.Errorf("未知协议")
	}
	return name, b.String(), nil
}

func ClashDocument(names []string, proxiesYAML string) string {
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\n")
	b.WriteString("proxies:\n")
	b.WriteString(proxiesYAML)
	b.WriteString("proxy-groups:\n  - name: liking\n    type: select\n    proxies:\n")
	if len(names) == 0 {
		b.WriteString("      - DIRECT\n")
	} else {
		for _, n := range names {
			fmt.Fprintf(&b, "      - %s\n", yq(n))
		}
	}
	b.WriteString("rules:\n  - MATCH,liking\n")
	return b.String()
}

func yq(s string) string {
	return strconv.Quote(s)
}

func writeClashALPN(b *strings.Builder, st Settings) {
	alpn := st.ALPN()
	if len(alpn) == 0 {
		return
	}
	b.WriteString("    alpn:\n")
	for _, a := range alpn {
		fmt.Fprintf(b, "      - %s\n", yq(a))
	}
}
