package agent

import (
	"encoding/base64"
	"strings"
	"testing"

	"liking/internal/wsproto"
)

func TestParseWGDumpDeltas(t *testing.T) {
	dump := strings.Join([]string{
		"serverpriv\tserverpub\t51820\t51820",
		"exitpub\tnone\t(none)\t0.0.0.0/0\t0\t999\t999\toff",
		"clientpub\tnone\t1.2.3.4:9\t10.7.0.10/32\t1\t100\t40\t25",
	}, "\n")
	peers := map[string]string{"clientpub": "u1.i2"}
	first := parseWGDump(dump, peers, nil)
	if len(first) != 1 || first[0].Email != "u1.i2" || first[0].Up != 100 || first[0].Down != 40 {
		t.Fatalf("parse %+v", first)
	}
	base, next := mitaDeltas(nil, first)
	if len(base) != 0 {
		t.Fatalf("baseline %+v", base)
	}
	dump2 := strings.Replace(dump, "100\t40", "130\t55", 1)
	delta, _ := mitaDeltas(next, parseWGDump(dump2, peers, nil))
	if len(delta) != 1 || delta[0].Up != 30 || delta[0].Down != 15 {
		t.Fatalf("delta %+v", delta)
	}
}

func TestStripWGQuickKeepsPeers(t *testing.T) {
	conf := "[Interface]\nPrivateKey = abc\nAddress = 10.7.0.1/24\nListenPort = 51820\nTable = off\nFwMark = 51820\nPostUp = ip route replace default dev %i table 100\n\n[Peer]\nPublicKey = peer\nAllowedIPs = 10.7.0.10/32\n"
	got := stripWGQuick(conf)
	if strings.Contains(got, "Address") || strings.Contains(got, "PostUp") || strings.Contains(got, "Table") {
		t.Fatalf("strip kept interface extras\n%s", got)
	}
	if !strings.Contains(got, "PrivateKey = abc") || !strings.Contains(got, "ListenPort = 51820") || !strings.Contains(got, "FwMark = 51820") {
		t.Fatalf("strip lost keys\n%s", got)
	}
	if !strings.Contains(got, "AllowedIPs = 10.7.0.10/32") {
		t.Fatalf("strip lost peer\n%s", got)
	}
	if wgInterfaceBlock(conf) == wgInterfaceBlock(strings.Replace(conf, "PostUp = ip route", "PostUp = true", 1)) {
		t.Fatal("interface block should notice PostUp")
	}
	_ = wsproto.WireGuardConfig{}
}

func TestParseWGDumpMatchesKeyOrAddress(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	std := base64.StdEncoding.EncodeToString(raw)
	bare := base64.RawStdEncoding.EncodeToString(raw)
	if canonicalWGKey(bare) != std {
		t.Fatalf("canonical %s %s", canonicalWGKey(bare), std)
	}
	dump := "priv\tpub\t10433\toff\n" + bare + "\tnone\t1.2.3.4:9\t10.7.0.10/32\t1\t80\t20\t25\n"
	got := parseWGDump(dump, map[string]string{std: "u9.i3"}, nil)
	if len(got) != 1 || got[0].Email != "u9.i3" || got[0].Up != 80 || got[0].Down != 20 {
		t.Fatalf("key %+v", got)
	}
	dump = "priv\tpub\t10433\toff\nnope\tnone\t1.2.3.4:9\t10.7.0.10/32\t1\t80\t20\t25\n"
	got = parseWGDump(dump, map[string]string{std: "u9.i3"}, map[string]string{"10.7.0.10/32": "u9.i3"})
	if len(got) != 1 || got[0].Email != "u9.i3" {
		t.Fatalf("addr %+v", got)
	}
	dump = "priv\tpub\t10433\toff\nexit\tnone\t(none)\t0.0.0.0/0\t1\t999\t999\toff\n"
	got = parseWGDump(dump, map[string]string{std: "u9.i3"}, map[string]string{"0.0.0.0/0": "u9.i3"})
	if len(got) != 0 {
		t.Fatalf("exit billed %+v", got)
	}
	reset, next := flowDeltas(map[string]bytePair{"u9.i3": {up: 500, down: 500}}, []wsproto.Sample{{Email: "u9.i3", Up: 8, Down: 8}})
	if len(reset) != 0 || next["u9.i3"].up != 8 {
		t.Fatalf("reset %+v %+v", reset, next)
	}
}

func TestWGJSONListenPort(t *testing.T) {
	raw := []byte("{\"iface\":\"likingwg0\",\"conf\":\"[Interface]\\nListenPort = 10433\\n\\n[Peer]\\nPublicKey = a\\n\"}")
	if wgJSONListenPort(raw) != 10433 {
		t.Fatalf("port %d", wgJSONListenPort(raw))
	}
	home := []byte("{\"conf\":\"[Interface]\\nAddress = 10.7.0.2/32\\n\"}")
	if wgJSONListenPort(home) != 0 {
		t.Fatal("home listens")
	}
}
