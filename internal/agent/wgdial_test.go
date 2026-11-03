package agent

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWGIsolateDeletes(t *testing.T) {
	routes, rules := wgIsolateDeletes(
		"default via 1.2.3.4 dev eth0\n"+
			"default dev likingwg0 scope link\n"+
			"0.0.0.0/1 dev likingwg0 scope link\n"+
			"128.0.0.0/1 dev likingwg1\n"+
			"10.7.0.0/24 dev likingwg0 proto kernel scope link\n",
		"0:\tfrom all lookup local\n"+
			"32765:\tfrom all not fwmark 0xca6c lookup 51820\n"+
			"32766:\tfrom all lookup main\n"+
			"100:\tfrom 10.7.0.0/24 lookup 100\n",
	)
	if len(routes) != 3 {
		t.Fatalf("routes %+v", routes)
	}
	if strings.Join(routes[0], " ") != "route del default dev likingwg0 table main" {
		t.Fatalf("default %+v", routes[0])
	}
	if strings.Join(routes[1], " ") != "route del 0.0.0.0/1 dev likingwg0 table main" {
		t.Fatalf("half %+v", routes[1])
	}
	if strings.Join(routes[2], " ") != "route del 128.0.0.0/1 dev likingwg1 table main" {
		t.Fatalf("upper %+v", routes[2])
	}
	if len(rules) != 1 || strings.Join(rules[0], " ") != "rule del not fwmark 51820 lookup 51820" {
		t.Fatalf("rules %+v", rules)
	}
	if _, _, ok := wgCapturingMainRoute("10.8.0.0/24 dev likingwg1 proto kernel scope link"); ok {
		t.Fatal("connected route is not a capture")
	}
	if _, _, ok := parseNotFwmarkRule("5:\tfrom all fwmark 0xca6d lookup main"); ok {
		t.Fatal("exemption was treated as a capture")
	}
}

func TestWGRouteWouldSteal(t *testing.T) {
	if !wgRouteWouldSteal("likingwg0", "likingwg1") {
		t.Fatal("second tunnel must not take the first tunnel's table")
	}
	if wgRouteWouldSteal("likingwg0", "likingwg0") || wgRouteWouldSteal("", "likingwg1") || wgRouteWouldSteal("eth0", "likingwg1") {
		t.Fatal("own device, empty table, and WAN default are not a steal")
	}
	if parseWGDefaultDev("default dev likingwg0 scope link\n10.8.0.0/24 dev likingwg1") != "likingwg0" {
		t.Fatal("default dev")
	}
}

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

func TestLoadWGDialFallsBackToLegacyFile(t *testing.T) {
	dir := t.TempDir()
	body := []byte(`{"listen_port":51820,"peers":{"abc":"198.51.100.9:51820"}}`)
	if err := os.WriteFile(filepath.Join(dir, "wg-dial.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}
	st := loadWGDial(dir, "likingwg0")
	if st.ListenPort != 51820 || st.Peers["abc"] != "198.51.100.9:51820" {
		t.Fatalf("legacy %+v", st)
	}
	saveWGDial(dir, "likingwg1", wgDialState{ListenPort: 10434})
	other := loadWGDial(dir, "likingwg1")
	if other.ListenPort != 10434 || len(other.Peers) != 0 {
		t.Fatalf("split %+v", other)
	}
	if loadWGDial(dir, "likingwg1").ListenPort == loadWGDial(dir, "likingwg0").ListenPort && loadWGDial(dir, "likingwg1").Peers["abc"] != "" {
		t.Fatal("second tunnel read the first tunnel dial state")
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
