package corecfg

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClashDocumentLeakOff(t *testing.T) {
	doc := ClashDocument([]string{"jp"}, "  - name: \"jp\"\n    type: ss\n", []string{"domestic"}, false)
	for _, s := range []string{"tun:", "respect-rules", "dns-hijack", "https://1.1.1.1/dns-query", "MATCH,节点选择"} {
		if strings.Contains(doc, s) {
			t.Fatalf("leak off contains %q\n%s", s, doc)
		}
	}
	if !strings.Contains(doc, "enhanced-mode: fake-ip") || !strings.Contains(doc, "fallback:") || !strings.Contains(doc, "MATCH,回落") {
		t.Fatalf("default dns/rules changed\n%s", doc)
	}
}

func TestClashDocumentLeakOn(t *testing.T) {
	doc := ClashDocument([]string{"jp"}, "  - name: \"jp\"\n    type: ss\n", []string{"domestic"}, true)
	for _, s := range []string{
		"ipv6: false",
		"tun:",
		"stack: mixed",
		"auto-route: true",
		"auto-detect-interface: true",
		"strict-route: true",
		"dns-hijack:",
		"any:53",
		"enhanced-mode: fake-ip",
		"respect-rules: true",
		"proxy-server-nameserver:",
		"https://1.1.1.1/dns-query",
		"https://dns.google/dns-query",
		"GEOIP,private,DIRECT,no-resolve",
		"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
		"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
		"AND,((NETWORK,UDP),(DST-PORT,3478)),节点选择",
		"AND,((NETWORK,UDP),(DST-PORT,19302)),节点选择",
		"RULE-SET,cn,国内服务,no-resolve",
		"MATCH,节点选择",
	} {
		if !strings.Contains(doc, s) {
			t.Fatalf("missing %q\n%s", s, doc)
		}
	}
	if strings.Contains(doc, "fallback:") || strings.Contains(doc, "MATCH,回落") {
		t.Fatalf("leak on still falls open\n%s", doc)
	}
	ip := strings.Index(doc, "IP-CIDR,10.0.0.0/8,DIRECT,no-resolve")
	stun := strings.Index(doc, "DST-PORT,3478")
	dom := strings.Index(doc, "RULE-SET,cn,国内服务,no-resolve")
	match := strings.Index(doc, "MATCH,节点选择")
	if ip < 0 || stun < ip || dom < stun || match < dom {
		t.Fatalf("rule order ip=%d stun=%d dom=%d match=%d", ip, stun, dom, match)
	}
}

func TestSingboxClientDocumentLeak(t *testing.T) {
	off := SingboxClientDocument(nil, []string{"jp"}, nil, false)
	if _, ok := off["inbounds"]; ok {
		t.Fatal("tun inbound while leak off")
	}
	if _, ok := off["dns"]; ok {
		t.Fatal("dns while leak off")
	}
	if off["route"].(map[string]any)["final"] != GroupFallback {
		t.Fatal("final changed while leak off")
	}

	doc := SingboxClientDocument([]any{map[string]any{"type": "shadowsocks", "tag": "jp"}}, []string{"jp"}, []string{"domestic"}, true)
	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, want := range []string{
		`"type":"tun"`,
		`"tag":"tun-in"`,
		`"inet4_address":"172.19.0.1/30"`,
		`"auto_route":true`,
		`"strict_route":true`,
		`"action":"hijack-dns"`,
		`"ip_is_private":true`,
		`"outbound":"direct"`,
		`"port":[3478,19302]`,
		`"outbound":"节点选择"`,
		`"final":"节点选择"`,
		`"detour":"节点选择"`,
		`"/dns-query"`,
		`"strategy":"ipv4_only"`,
		`"outbound":"any"`,
		`"download_detour":"direct"`,
		`"国内服务"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %s\n%s", want, s)
		}
	}
	if strings.Contains(s, `"address":`) {
		t.Fatalf("deprecated address field\n%s", s)
	}
	route := doc["route"].(map[string]any)
	rules, _ := route["rules"].([]any)
	priv, stun, dom := -1, -1, -1
	for i, raw := range rules {
		m, _ := raw.(map[string]any)
		if m["ip_is_private"] == true {
			priv = i
		}
		if m["network"] == "udp" {
			stun = i
		}
		if m["outbound"] == "国内服务" {
			dom = i
		}
	}
	if priv < 0 || stun < priv || dom < stun {
		t.Fatalf("rule order priv=%d stun=%d dom=%d rules=%v", priv, stun, dom, rules)
	}
}
