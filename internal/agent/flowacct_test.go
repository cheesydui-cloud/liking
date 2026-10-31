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
	text := "" +
		"ESTAB 0 0 0.0.0.0:443 1.2.3.4:40000\n" +
		"\tcubic bytes_sent:8000 bytes_received:1000\n" +
		"ESTAB 0 0 10.0.0.5:50000 9.9.9.9:443\n" +
		"\tcubic bytes_sent:2000 bytes_received:9000 fwmark:0x4c000009\n"
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
		t.Fatalf("fwmark %+v", marks[mark])
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

func TestFlowSamplesSkipCoveredEmail(t *testing.T) {
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
	samples, next, _, ok := c.peekFlows(map[string]struct{}{"u1.i2": {}})
	if !ok {
		t.Fatal("read")
	}
	if len(samples) != 0 {
		t.Fatalf("covered email billed %+v", samples)
	}
	if next["u1.i2"].up != 300 {
		t.Fatalf("cursor %+v", next["u1.i2"])
	}
	samples, _, _, ok = c.peekFlows(nil)
	if !ok || len(samples) != 1 || samples[0].Email != "u1.i2" || samples[0].Up != 200 {
		t.Fatalf("uncommitted cursor must still bill the interval %+v ok %v", samples, ok)
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
