package agent

import (
	"strings"
	"testing"

	"liking/internal/wsproto"
)

func TestParseConntrackPortAndMark(t *testing.T) {
	text := strings.Join([]string{
		"ipv4 2 tcp 6 100 ESTABLISHED src=1.2.3.4 dst=10.0.0.5 sport=40000 dport=443 packets=2 bytes=1000 src=10.0.0.5 dst=1.2.3.4 sport=443 dport=40000 packets=3 bytes=8000 mark=0",
		"ipv4 2 tcp 6 100 ESTABLISHED src=10.0.0.5 dst=9.9.9.9 sport=50000 dport=443 packets=4 bytes=2000 src=9.9.9.9 dst=10.0.0.5 sport=443 dport=50000 packets=5 bytes=9000 mark=1275068425",
		"ipv4 2 tcp 6 100 ESTABLISHED src=8.8.8.8 dst=10.0.0.5 sport=11 dport=8443 packets=1 bytes=50 src=10.0.0.5 dst=8.8.8.8 sport=8443 dport=11 packets=1 bytes=70",
	}, "\n")
	listen := map[int]struct{}{443: {}, 8443: {}}
	local := map[string]struct{}{"10.0.0.5": {}}
	ports, marks, ok := parseConntrack(text, listen, local)
	if !ok {
		t.Fatal("no bytes")
	}
	if ports[443].up != 1000 || ports[443].down != 8000 {
		t.Fatalf("443 %+v", ports[443])
	}
	if ports[8443].up != 50 || ports[8443].down != 70 {
		t.Fatalf("8443 %+v", ports[8443])
	}
	if ports[443].up == 3000 {
		t.Fatal("outbound to :443 counted as the listen port")
	}
	if marks[1275068425].up != 2000 || marks[1275068425].down != 9000 {
		t.Fatalf("mark %+v", marks[1275068425])
	}
}

func TestParseSSListenAndFwmark(t *testing.T) {
	// iproute2 prints fwmark on the socket line and byte counters on the next line.
	text := "" +
		"Netid State Recv-Q Send-Q Local Address:Port Peer Address:Port\n" +
		"tcp ESTAB 0 0 0.0.0.0:443 1.2.3.4:40000 users:((\"xray\",pid=1,fd=3)) ino:1 sk:aa\n" +
		"\t cubic wscale:7,7 rto:201 bytes_sent:8000 bytes_acked:8000 bytes_received:1000\n" +
		"tcp ESTAB 0 0 10.0.0.5:50000 9.9.9.9:443 users:((\"sing-box\",pid=2,fd=4)) ino:2 sk:bb fwmark:0x4c000009\n" +
		"\t cubic wscale:7,7 rto:201 bytes_sent:2000 bytes_acked:2000 bytes_received:9000\n" +
		"tcp ESTAB 0 0 10.0.0.5:50001 8.8.8.8:53 ino:3 sk:cc\n" +
		"\tcubic bytes_sent:50 bytes_received:60\n" +
		"tcp ESTAB 0 0 10.0.0.5:50002 1.1.1.1:443\n" +
		"\tcubic bytes_sent:70 bytes_received:80 fwmark:0x4c00000a\n"
	listen := map[int]struct{}{443: {}}
	ports, marks := parseSS(text, listen)
	if ports[443].up != 1000 || ports[443].down != 8000 {
		t.Fatalf("port %+v", ports[443])
	}
	mark, ok := parseMark("0x4c000009")
	if !ok {
		t.Fatal("mark")
	}
	if marks[mark].up != 2000 || marks[mark].down != 9000 {
		t.Fatalf("header fwmark %+v", marks[mark])
	}
	other, ok := parseMark("0x4c00000a")
	if !ok {
		t.Fatal("detail mark")
	}
	if marks[other].up != 70 || marks[other].down != 80 {
		t.Fatalf("detail fwmark %+v", marks[other])
	}
	if len(marks) != 2 {
		t.Fatalf("unmarked socket inherited a mark: %+v", marks)
	}
}

func TestWGListenRateSkipsExitAndFirstSight(t *testing.T) {
	peers := []wsproto.WGPeerStat{
		{PublicKey: "exit", AllowedIPs: "0.0.0.0/0", Rx: 900000, Tx: 800000},
		{PublicKey: "probe", AllowedIPs: "192.0.2.1/32", Rx: 50, Tx: 50},
		{PublicKey: "home", AllowedIPs: "10.7.0.0/24", Rx: 700000, Tx: 700000},
		{Email: "relay.i1", PublicKey: "relay", AllowedIPs: "10.7.0.2/32", Rx: 400000, Tx: 400000},
		{Email: "u1", PublicKey: "cust", AllowedIPs: "10.7.0.11/32", Rx: 1000, Tx: 5000},
	}
	up, down, next, ok := wgListenDelta(wgListenCursor{}, peers, 5)
	if !ok || up != 0 || down != 0 || !next.seen || next.up != 1000 || next.down != 5000 {
		t.Fatalf("first sight %+v %d %d %v", next, up, down, ok)
	}
	peers[4].Rx = 6000
	peers[4].Tx = 5000
	up, down, next, ok = wgListenDelta(next, peers, 5)
	if !ok || up != 1000 || down != 0 || next.up != 6000 {
		t.Fatalf("delta %d %d %+v", up, down, next)
	}
	if _, _, _, ok = wgListenDelta(next, []wsproto.WGPeerStat{{AllowedIPs: "0.0.0.0/0", Rx: 1, Tx: 1}}, 5); ok {
		t.Fatal("exit must not become a listen rate")
	}
}

func TestPeekFlowsOverlaysWireGuardListen(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.lastReq["wireguard"] = []byte(`{"iface":"likingwg0","conf":"[Interface]\nListenPort = 10433\n"}`)
	c.pendingWG = []wsproto.WGPeerStat{
		{AllowedIPs: "0.0.0.0/0", Rx: 9, Tx: 9},
		{AllowedIPs: "10.7.0.11/32", Rx: 1000, Tx: 2000},
	}
	old := readFlowSources
	readFlowSources = func() (flowSources, error) {
		return flowSources{
			conntrack: "ipv4 2 tcp 6 1 ESTABLISHED src=1.2.3.4 dst=10.0.0.5 sport=1 dport=443 packets=1 bytes=10 src=10.0.0.5 dst=1.2.3.4 sport=443 dport=1 packets=1 bytes=10 mark=0",
			local:     map[string]struct{}{"10.0.0.5": {}},
		}, nil
	}
	defer func() { readFlowSources = old }()
	_, _, _, ok := c.peekFlows()
	if !ok || !c.pendingWGListenOK {
		t.Fatal("overlay cursor")
	}
	var first wsproto.PortRate
	for _, p := range c.pendingPorts {
		if p.Port == 10433 {
			first = p
		}
	}
	if first.UpBps != 0 || first.DownBps != 0 {
		t.Fatalf("absolute counters became a rate %+v", first)
	}
	c.wgListenCur = c.pendingWGListen
	c.pendingWG[1].Rx = 3500
	c.pendingWG[1].Tx = 7000
	_, _, _, ok = c.peekFlows()
	if !ok {
		t.Fatal("second peek")
	}
	var got wsproto.PortRate
	for _, p := range c.pendingPorts {
		if p.Port == 10433 {
			got = p
		}
	}
	if got.UpBps != 500 || got.DownBps != 1000 {
		t.Fatalf("listen rate %+v", got)
	}
}

func TestPortRatesStayDistinct(t *testing.T) {
	prev := map[int]bytePair{443: {up: 1000, down: 2000}, 8443: {up: 10, down: 20}}
	cur := map[int]bytePair{443: {up: 6000, down: 2000}, 8443: {up: 10, down: 20}}
	rates, _ := portRatesFrom(prev, cur, []int{443, 8443}, 5)
	by := map[int]wsproto.PortRate{}
	for _, r := range rates {
		by[r.Port] = r
	}
	if by[443].UpBps != 1000 || by[443].DownBps != 0 {
		t.Fatalf("443 %+v", by[443])
	}
	if by[8443].UpBps != 0 || by[8443].DownBps != 0 {
		t.Fatalf("8443 %+v", by[8443])
	}
}

func TestFlowSamplesBillMarkedSocket(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.acctMarks = []wsproto.AcctMark{{Mark: 0x4c000009, Email: "u1.i2"}}
	c.flowLast = map[string]bytePair{"u1.i2": {up: 100, down: 100}}
	c.portLast = map[int]bytePair{}
	old := readFlowSources
	readFlowSources = func() (flowSources, error) {
		return flowSources{
			conntrack: "ipv4 2 tcp 6 1 ESTABLISHED src=10.0.0.5 dst=9.9.9.9 sport=1 dport=443 packets=1 bytes=300 src=9.9.9.9 dst=10.0.0.5 sport=443 dport=1 packets=1 bytes=400 mark=1275068425",
			local:     map[string]struct{}{"10.0.0.5": {}},
		}, nil
	}
	defer func() { readFlowSources = old }()
	samples, next, _, ok := c.peekFlows()
	if !ok || len(samples) != 1 || samples[0].Email != "u1.i2" || samples[0].Up != 200 || samples[0].Down != 300 {
		t.Fatalf("socket delta %+v ok %v", samples, ok)
	}
	if next["u1.i2"].up != 300 || next["u1.i2"].down != 400 {
		t.Fatalf("cursor %+v", next["u1.i2"])
	}
	// The cursor moves only after the stats frame is written.
	samples, _, _, ok = c.peekFlows()
	if !ok || len(samples) != 1 || samples[0].Up != 200 {
		t.Fatalf("uncommitted cursor must still bill the interval %+v ok %v", samples, ok)
	}
}

func TestFlowCumulUsesSocketMarkWhenConntrackUnmarked(t *testing.T) {
	mark, ok := parseMark("0x4c000009")
	if !ok {
		t.Fatal("mark")
	}
	src := flowSources{
		conntrack: strings.Join([]string{
			"ipv4 2 tcp 6 1 ESTABLISHED src=1.2.3.4 dst=10.0.0.5 sport=1 dport=443 packets=1 bytes=5000 src=10.0.0.5 dst=1.2.3.4 sport=443 dport=1 packets=1 bytes=9000 mark=0",
			"ipv4 2 tcp 6 1 ESTABLISHED src=10.0.0.5 dst=9.9.9.9 sport=2 dport=443 packets=1 bytes=100 src=9.9.9.9 dst=10.0.0.5 sport=443 dport=2 packets=1 bytes=200 mark=0",
		}, "\n"),
		ss:    "tcp ESTAB 0 0 10.0.0.5:50000 9.9.9.9:443 ino:2 sk:bb fwmark:0x4c000009\n\tcubic bytes_sent:2000 bytes_received:8000\n",
		local: map[string]struct{}{"10.0.0.5": {}},
	}
	ports, byEmail, ok := flowCumul(src, map[int]struct{}{443: {}}, map[uint32]string{mark: "u1.i2"})
	if !ok {
		t.Fatal("read")
	}
	if byEmail["u1.i2"].up != 2000 || byEmail["u1.i2"].down != 8000 {
		t.Fatalf("socket mark dropped %+v", byEmail["u1.i2"])
	}
	if ports[443].up != 5000 || ports[443].down != 9000 {
		t.Fatalf("listen port %+v", ports[443])
	}
}

func TestFlowCumulAttributesUnmarkedConntrackBySocket(t *testing.T) {
	mark, ok := parseMark("0x4c000009")
	if !ok {
		t.Fatal("mark")
	}
	// Listen port has bytes and no user. Outbound UDP has fwmark on ss and
	// bytes only in conntrack, with mark=0. That is the node-moves,
	// user-card-stays-0 split for UDP and for a failed nft copy.
	src := flowSources{
		conntrack: strings.Join([]string{
			"ipv4 2 udp 17 30 src=1.2.3.4 dst=10.0.0.5 sport=1111 dport=1080 packets=1 bytes=5000 src=10.0.0.5 dst=1.2.3.4 sport=1080 dport=1111 packets=1 bytes=8000 mark=0",
			"ipv4 2 udp 17 30 src=10.0.0.5 dst=9.9.9.9 sport=40000 dport=443 packets=1 bytes=2000 src=9.9.9.9 dst=10.0.0.5 sport=443 dport=40000 packets=1 bytes=9000 mark=0",
			"ipv4 2 tcp 6 100 ESTABLISHED src=10.0.0.5 dst=8.8.8.8 sport=50000 dport=443 packets=1 bytes=3000 src=8.8.8.8 dst=10.0.0.5 sport=443 dport=50000 packets=1 bytes=7000 mark=0",
		}, "\n"),
		ss: strings.Join([]string{
			"udp UNCONN 0 0 10.0.0.5:40000 9.9.9.9:443 fwmark:0x4c000009",
			"tcp ESTAB 0 0 10.0.0.5:50000 8.8.8.8:443 fwmark:0x4c000009",
		}, "\n"),
		local: map[string]struct{}{"10.0.0.5": {}},
	}
	ports, byEmail, ok := flowCumul(src, map[int]struct{}{1080: {}}, map[uint32]string{mark: "u1.i2"})
	if !ok {
		t.Fatal("read")
	}
	if ports[1080].up != 5000 || ports[1080].down != 8000 {
		t.Fatalf("listen port %+v", ports[1080])
	}
	if byEmail["u1.i2"].up != 5000 || byEmail["u1.i2"].down != 16000 {
		t.Fatalf("user %+v", byEmail["u1.i2"])
	}
}

func TestFlowCumulDoesNotSumJoinedMark(t *testing.T) {
	mark, ok := parseMark("0x4c000009")
	if !ok {
		t.Fatal("mark")
	}
	src := flowSources{
		conntrack: "ipv4 2 tcp 6 1 ESTABLISHED src=10.0.0.5 dst=9.9.9.9 sport=50000 dport=443 packets=1 bytes=2000 src=9.9.9.9 dst=10.0.0.5 sport=443 dport=50000 packets=1 bytes=9000 mark=1275068425",
		ss:        "tcp ESTAB 0 0 10.0.0.5:50000 9.9.9.9:443 fwmark:0x4c000009\n\tcubic bytes_sent:1500 bytes_received:8000\n",
		local:     map[string]struct{}{"10.0.0.5": {}},
	}
	_, byEmail, ok := flowCumul(src, nil, map[uint32]string{mark: "u1.i2"})
	if !ok {
		t.Fatal("read")
	}
	if byEmail["u1.i2"].up != 2000 || byEmail["u1.i2"].down != 9000 {
		t.Fatalf("joined mark summed %+v", byEmail["u1.i2"])
	}
}

func TestFlowCumulDoesNotSumBothMarks(t *testing.T) {
	mark, ok := parseMark("0x4c000009")
	if !ok {
		t.Fatal("mark")
	}
	src := flowSources{
		conntrack: "ipv4 2 tcp 6 1 ESTABLISHED src=10.0.0.5 dst=9.9.9.9 sport=2 dport=443 packets=1 bytes=2000 src=9.9.9.9 dst=10.0.0.5 sport=443 dport=2 packets=1 bytes=9000 mark=1275068425",
		ss:        "tcp ESTAB 0 0 10.0.0.5:50000 9.9.9.9:443 ino:2 sk:bb fwmark:0x4c000009\n\tcubic bytes_sent:1500 bytes_received:8000\n",
		local:     map[string]struct{}{"10.0.0.5": {}},
	}
	_, byEmail, ok := flowCumul(src, nil, map[uint32]string{mark: "u1.i2"})
	if !ok {
		t.Fatal("read")
	}
	if byEmail["u1.i2"].up != 2000 || byEmail["u1.i2"].down != 9000 {
		t.Fatalf("marks summed %+v", byEmail["u1.i2"])
	}
}

func TestFlowDeltasIgnoreShrink(t *testing.T) {
	prev := map[string]bytePair{"u1.i2": {up: 10000, down: 50000}}
	// One flow expired (upload sum fell) while download still grew.
	got, next := flowDeltas(prev, []wsproto.Sample{{Email: "u1.i2", Up: 8000, Down: 52000}})
	if len(got) != 1 || got[0].Up != 0 || got[0].Down != 2000 {
		t.Fatalf("shrink billed %+v", got)
	}
	if next["u1.i2"].up != 8000 || next["u1.i2"].down != 52000 {
		t.Fatalf("cursor %+v", next["u1.i2"])
	}
}

func TestStripV2RayAPI(t *testing.T) {
	raw := []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:60190"},"v2ray_api":{"listen":"127.0.0.1:60191"}}}`)
	out := stripV2RayAPI(raw)
	if strings.Contains(string(out), "v2ray_api") {
		t.Fatalf("%s", out)
	}
	if !strings.Contains(string(out), "clash_api") {
		t.Fatalf("clash removed: %s", out)
	}
	if string(stripV2RayAPI([]byte(`{"log":{}}`))) != `{"log":{}}` {
		t.Fatalf("untouched %s", stripV2RayAPI([]byte(`{"log":{}}`)))
	}
}
