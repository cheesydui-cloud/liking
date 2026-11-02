package agent

import (
	"encoding/base64"
	"strings"
	"testing"
)

func TestWGPolicyRuleParse(t *testing.T) {
	conf := "PostUp = ip rule del fwmark 51820 lookup main priority 40 2>/dev/null || true; ip rule add fwmark 51820 lookup main priority 40; ip rule add from 10.7.0.1 lookup main priority 50; ip rule add from 10.7.0.0/24 lookup 100 priority 100; ip route replace default dev %i table 100\n"
	rules := wgPolicyRules(conf)
	if len(rules) != 3 {
		t.Fatalf("rules %d", len(rules))
	}
	if rules[0].FwMark != 51820 || rules[0].Priority != 40 || rules[0].Table != "main" {
		t.Fatalf("fwmark %+v", rules[0])
	}
	if rules[2].From != "10.7.0.0/24" || rules[2].Priority != 100 || rules[2].Table != "100" {
		t.Fatalf("net %+v", rules[2])
	}
	if wgPolicyTable(conf) != 100 {
		t.Fatal(wgPolicyTable(conf))
	}
	show := "0:\tfrom all lookup local\n40:\tfrom all fwmark 0xca6c lookup main\n50:\tfrom 10.7.0.1 lookup main\n100:\tfrom 10.7.0.0/24 lookup 100\n"
	if !ipRuleShowHas(show, rules[0]) || !ipRuleShowHas(show, rules[1]) || !ipRuleShowHas(show, rules[2]) {
		t.Fatal("present rules were not matched")
	}
	missing := rules[2]
	missing.Priority = 101
	if ipRuleShowHas(show, missing) {
		t.Fatal("matched a rule that is not there")
	}
}

func TestWGPeersWithoutEndpoint(t *testing.T) {
	conf := "[Peer]\nPublicKey = exit\nAllowedIPs = 0.0.0.0/0\n\n[Peer]\nPublicKey = home\nEndpoint = cm.example:10433\nAllowedIPs = 10.7.0.0/24\n"
	got := wgPeersWithoutEndpoint(conf)
	if got["exit"] != "0.0.0.0/0" {
		t.Fatalf("exit %+v", got)
	}
	if _, ok := got["home"]; ok {
		t.Fatal("peer with endpoint should stay")
	}
}

func TestParseWGDumpDial(t *testing.T) {
	iface := base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))
	peer := base64.StdEncoding.EncodeToString([]byte("abcdef0123456789abcdef0123456789"))
	dump := iface + "\t" + iface + "\t51820\t51820\n" + peer + "\t(none)\t99.29.110.122:51820\t0.0.0.0/0\t1\t2\t3\t25\n"
	got := parseWGDumpDial(dump)
	if got.ListenPort != 51820 {
		t.Fatalf("port %d", got.ListenPort)
	}
	if got.Peers[canonicalWGKey(peer)] != "99.29.110.122:51820" {
		t.Fatalf("peers %+v", got.Peers)
	}
	if strings.Contains(dump, "private") {
		t.Fatal("fixture drift")
	}
}
