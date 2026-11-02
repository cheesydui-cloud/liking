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

func TestParseWGPeerDumpKeepsExitForPanel(t *testing.T) {
	dump := strings.Join([]string{
		"priv\tpub\t10433\toff",
		"exit\tnone\t(none)\t0.0.0.0/0\t1\t999\t999\toff",
		"probe\tnone\t(none)\t192.0.2.1/32\t1\t1\t1\toff",
		"client\tnone\t1.2.3.4:9\t10.7.0.10/32\t1\t80\t20\t25",
	}, "\n")
	got := parseWGPeerDump(dump)
	if len(got) != 3 || got[2].PublicKey != "client" || got[2].Rx != 80 || got[2].Tx != 20 || got[2].AllowedIPs != "10.7.0.10/32" {
		t.Fatalf("peers %+v", got)
	}
	if got[0].AllowedIPs != "0.0.0.0/0" || got[1].AllowedIPs != "192.0.2.1/32" {
		t.Fatalf("unbilled peers %+v", got[:2])
	}
	c := &Cores{wgPeers: map[string]string{"client": "u1.i2"}, wgLast: map[string]bytePair{"u1.i2": {up: 80, down: 20}}}
	deltas, next, ok := c.wgEmailDeltas(got)
	if !ok || len(deltas) != 0 || next["u1.i2"].up != 80 {
		t.Fatalf("no growth %+v %v %+v", deltas, ok, next)
	}
	got[2].Rx = 100
	got[2].Tx = 35
	deltas, _, ok = c.wgEmailDeltas(got)
	if !ok || len(deltas) != 1 || deltas[0].Email != "u1.i2" || deltas[0].Up != 20 || deltas[0].Down != 15 {
		t.Fatalf("email delta %+v %v", deltas, ok)
	}
}

func TestParseWGTransferAndEmptyDump(t *testing.T) {
	dump := "priv\tpub\t10433\toff\n"
	if peers := parseWGPeerDump(dump); len(peers) != 0 {
		t.Fatalf("interface line is not a peer %+v", peers)
	}
	xfer := "client\t80\t20\nexit\t9\t9\n"
	got := parseWGTransfer(xfer)
	if len(got) != 2 || got[0].PublicKey != "client" || got[0].Rx != 80 || got[0].Tx != 20 || got[0].AllowedIPs != "" {
		t.Fatalf("transfer %+v", got)
	}
	if peers := parseWGPeerDump(xfer); len(peers) != 0 {
		t.Fatalf("transfer must not be read as dump %+v", peers)
	}
}

func TestWGNeedsBounceUsesDiskConf(t *testing.T) {
	iface := "[Interface]\nPrivateKey = abc\nAddress = 10.7.0.1/24\nListenPort = 10433\nPostUp = iptables -I FORWARD 1 -i %i -j ACCEPT\n"
	disk := iface + "\n[Peer]\nPublicKey = old\nAllowedIPs = 10.7.0.10/32\n"
	next := iface + "\n[Peer]\n# liking-email=u1.i0\nPublicKey = new\nAllowedIPs = 10.7.0.11/32\n"
	if wgNeedsBounce(next, "", disk) {
		t.Fatal("peer-only change against the on-disk conf must not bounce")
	}
	changed := strings.Replace(iface, "PostUp = iptables", "PostUp = true", 1) + "\n[Peer]\nPublicKey = new\nAllowedIPs = 10.7.0.11/32\n"
	if !wgNeedsBounce(changed, "", disk) {
		t.Fatal("PostUp change must bounce")
	}
	if !wgNeedsBounce(next, "", "") {
		t.Fatal("no previous conf must bounce")
	}
}

func TestWGLedgerAnnotatesAfterRestart(t *testing.T) {
	dir := t.TempDir()
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	pub := base64.StdEncoding.EncodeToString(raw)
	saveWGLedger(dir, map[string]string{pub: "u1.i2"}, map[string]string{"10.7.0.10/32": "u1.i2"})
	c := &Cores{dir: dir, wgLast: map[string]bytePair{"u1.i2": {up: 80, down: 20}}}
	peers := []wsproto.WGPeerStat{{PublicKey: pub, AllowedIPs: "10.7.0.10/32", Rx: 100, Tx: 40}}
	got := c.annotateWGPeers(peers)
	if len(got) != 1 || got[0].Email != "u1.i2" {
		t.Fatalf("annotate %+v", got)
	}
	deltas, _, ok := c.wgEmailDeltas(got)
	if !ok || len(deltas) != 1 || deltas[0].Email != "u1.i2" || deltas[0].Up != 20 || deltas[0].Down != 20 {
		t.Fatalf("ledger delta %+v %v", deltas, ok)
	}
}

func TestIdentityFromWGConf(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 4)
	}
	pub := base64.StdEncoding.EncodeToString(raw)
	conf := "[Interface]\nPrivateKey = abc\n\n[Peer]\nPublicKey = exit\nAllowedIPs = 0.0.0.0/0\n\n[Peer]\n# liking-email=u1.i2\nPublicKey = " + pub + "\nAllowedIPs = 10.7.0.10/32\n"
	peers, byIP := identityFromWGConf(conf)
	if peers[pub] != "u1.i2" || byIP["10.7.0.10/32"] != "u1.i2" {
		t.Fatalf("identity %+v %+v", peers, byIP)
	}
	if _, ok := peers["exit"]; ok || byIP["0.0.0.0/0"] != "" {
		t.Fatal("exit comment missing must not be a customer")
	}
}

func TestParseWGAllDumpDoesNotShiftRealKey(t *testing.T) {
	raw := make([]byte, 32)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	pub := base64.StdEncoding.EncodeToString(raw)
	dump := "likingwg0\tprivkey\tpubkey\t10433\toff\nlikingwg0\t" + pub + "\tnone\t1.2.3.4:9\t10.7.0.10/32\t1\t80\t20\t25\n"
	got := parseWGPeerDump(dump)
	if len(got) != 1 || got[0].PublicKey != pub || got[0].Rx != 80 || got[0].Tx != 20 || got[0].AllowedIPs != "10.7.0.10/32" {
		t.Fatalf("shifted %+v", got)
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
