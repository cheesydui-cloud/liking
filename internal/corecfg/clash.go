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
	case ProfilePortForward:
		return "", "", fmt.Errorf("端口中转没有分享链接")
	case ProfileSOCKS5:
		b.WriteString("    type: socks5\n")
		fmt.Fprintf(&b, "    username: %s\n", yq(clientSocksUser(c)))
		fmt.Fprintf(&b, "    password: %s\n", yq(c.Password))
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

func ClashDocument(names []string, proxiesYAML string, selected []string) string {
	var b strings.Builder
	b.WriteString("mixed-port: 7890\nallow-lan: false\nmode: rule\n")
	b.WriteString("dns:\n  enable: true\n  ipv6: false\n  enhanced-mode: fake-ip\n")
	b.WriteString("  fake-ip-range: 198.18.0.1/16\n")
	b.WriteString("  nameserver:\n    - 1.1.1.1\n    - 8.8.8.8\n")
	b.WriteString("  fallback:\n    - 1.0.0.1\n")
	b.WriteString("  fake-ip-filter:\n    - '*.lan'\n    - localhost\n    - '*.local'\n")
	b.WriteString("proxies:\n")
	b.WriteString(proxiesYAML)
	writeClashGroups(&b, names, selected)
	writeClashRules(&b, selected)
	writeClashRuleProviders(&b, selected)
	return b.String()
}

func writeClashGroups(b *strings.Builder, names, selected []string) {
	cats := SelectedCategories(selected)
	auto := append([]string{}, names...)
	if len(auto) == 0 {
		auto = []string{"DIRECT"}
	}
	selectProxies := append([]string{"DIRECT", "REJECT", GroupAuto}, names...)
	b.WriteString("proxy-groups:\n")
	writeClashGroup(b, GroupSelect, "select", nil, selectProxies)
	writeClashGroup(b, GroupAuto, "url-test", func(b *strings.Builder) {
		b.WriteString("    url: https://www.gstatic.com/generate_204\n")
		b.WriteString("    interval: 300\n")
		b.WriteString("    lazy: false\n")
	}, auto)
	for _, c := range cats {
		writeClashGroup(b, c.Label, "select", nil, categoryClashProxies(c.Name, names))
	}
	writeClashGroup(b, GroupFallback, "select", nil, append([]string{GroupSelect, "DIRECT", "REJECT", GroupAuto}, names...))
}

func categoryClashProxies(name string, names []string) []string {
	switch name {
	case "ads":
		return append([]string{"REJECT", GroupSelect, "DIRECT", GroupAuto}, names...)
	case "domestic":
		return []string{"DIRECT", GroupSelect}
	case "private":
		return append([]string{"DIRECT", GroupSelect, "REJECT", GroupAuto}, names...)
	default:
		return append([]string{GroupSelect, "DIRECT", "REJECT", GroupAuto}, names...)
	}
}

func writeClashGroup(b *strings.Builder, name, typ string, extra func(*strings.Builder), proxies []string) {
	fmt.Fprintf(b, "  - name: %s\n    type: %s\n", yq(name), typ)
	if extra != nil {
		extra(b)
	}
	b.WriteString("    proxies:\n")
	if len(proxies) == 0 {
		b.WriteString("      - DIRECT\n")
		return
	}
	for _, p := range proxies {
		fmt.Fprintf(b, "      - %s\n", yq(p))
	}
}

func writeClashRules(b *strings.Builder, selected []string) {
	cats := SelectedCategories(selected)
	b.WriteString("rules:\n")
	seen := map[string]bool{}
	for _, c := range cats {
		for _, p := range c.SiteRules {
			if p.Key == "" || seen[p.Key] {
				continue
			}
			seen[p.Key] = true
			fmt.Fprintf(b, "  - %s\n", yq("RULE-SET,"+p.Key+","+c.Label))
		}
	}
	for _, c := range cats {
		for _, p := range c.IPRules {
			if p.Key == "" || seen[p.Key] {
				continue
			}
			seen[p.Key] = true
			fmt.Fprintf(b, "  - %s\n", yq("RULE-SET,"+p.Key+","+c.Label+",no-resolve"))
		}
	}
	fmt.Fprintf(b, "  - %s\n", yq("MATCH,"+GroupFallback))
}

func writeClashRuleProviders(b *strings.Builder, selected []string) {
	cats := SelectedCategories(selected)
	if len(cats) == 0 {
		return
	}
	b.WriteString("rule-providers:\n")
	seen := map[string]bool{}
	for _, c := range cats {
		for _, p := range append(append([]RuleProvider{}, c.SiteRules...), c.IPRules...) {
			if p.Key == "" || seen[p.Key] {
				continue
			}
			seen[p.Key] = true
			interval := p.Interval
			if interval <= 0 {
				interval = 86400
			}
			typ := nz(p.Type, "http")
			format := nz(p.Format, "mrs")
			behavior := nz(p.Behavior, "domain")
			fmt.Fprintf(b, "  %s:\n", clashYAMLKey(p.Key))
			fmt.Fprintf(b, "    type: %s\n    behavior: %s\n    format: %s\n", typ, behavior, format)
			fmt.Fprintf(b, "    url: %s\n", yq(p.URL))
			fmt.Fprintf(b, "    path: %s\n", yq(nz(p.Path, "./ruleset/"+p.Key+"."+format)))
			fmt.Fprintf(b, "    interval: %d\n", interval)
		}
	}
}

func clashYAMLKey(s string) string {
	if s == "" || strings.ContainsAny(s, "[]{}#&*!|>'\"%@`,:?") || strings.Contains(s, " ") {
		return yq(s)
	}
	return s
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
