package agent

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"liking/internal/wsproto"
)

type tripFunc func(*http.Request) (*http.Response, error)

func (f tripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseXrayStats(t *testing.T) {
	raw := []byte(`{"stat":[
		{"name":"user>>>u1.i2>>>traffic>>>uplink","value":100},
		{"name":"user>>>u1.i2>>>traffic>>>downlink","value":250},
		{"name":"inbound>>>in-2>>>traffic>>>uplink","value":9},
		{"name":"user>>>u9.i1>>>traffic>>>uplink","value":0}
	]}`)
	got := parseXrayStats(raw)
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 100 || got[0].Down != 250 {
		t.Fatalf("%+v", got)
	}
}

func TestParseXrayStatsPrefixAndStringValue(t *testing.T) {
	raw := []byte("Xray 26.3.27 started\n{\"Stat\":[{\"name\":\"user>>>u1.i2>>>traffic>>>uplink\",\"value\":\"100\"},{\"name\":\"user>>>u1.i2>>>traffic>>>downlink\",\"value\":\"250\"}]}\n")
	got, ok := parseXrayStatsOK(raw)
	if !ok || len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 100 || got[0].Down != 250 {
		t.Fatalf("ok %v %+v", ok, got)
	}
	if _, ok := parseXrayStatsOK([]byte("failed to query stats")); ok {
		t.Fatal("non-JSON must not commit")
	}
	empty, ok := parseXrayStatsOK([]byte(`{"stat":[]}`))
	if !ok || len(empty) != 0 {
		t.Fatalf("empty ok %v %+v", ok, empty)
	}
}

func TestSingboxStatsListen(t *testing.T) {
	okAddr, ok := singboxStatsListen([]byte(`{"experimental":{"v2ray_api":{"listen":"127.0.0.1:60191","stats":{"enabled":true}}}}`))
	if !ok || okAddr != "127.0.0.1:60191" {
		t.Fatalf("listen %q ok %v", okAddr, ok)
	}
	if _, ok := singboxStatsListen([]byte(`{"experimental":{"v2ray_api":{"listen":"0.0.0.0:60191","stats":{"enabled":true}}}}`)); ok {
		t.Fatal("non-loopback must be ignored")
	}
	if _, ok := singboxStatsListen([]byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:60190"}}}`)); ok {
		t.Fatal("missing v2ray_api must fall back")
	}
}

func TestQueryXrayUserStatsNoRetryOnTimeout(t *testing.T) {
	old := runXrayStatsQueryFn
	defer func() { runXrayStatsQueryFn = old }()
	calls := 0
	runXrayStatsQueryFn = func(bin, api string, pattern bool) ([]byte, error) {
		calls++
		return nil, context.DeadlineExceeded
	}
	if _, err := queryXrayUserStats("xray", "127.0.0.1:60085"); err == nil || calls != 1 {
		t.Fatalf("calls %d err %v", calls, err)
	}
}

func TestQueryXrayUserStatsRetriesFastFailure(t *testing.T) {
	old := runXrayStatsQueryFn
	defer func() { runXrayStatsQueryFn = old }()
	calls := 0
	runXrayStatsQueryFn = func(bin, api string, pattern bool) ([]byte, error) {
		calls++
		if pattern {
			return []byte("unknown flag"), fmt.Errorf("exit status 2")
		}
		return []byte(`{"stat":[{"name":"user>>>u1.i2>>>traffic>>>uplink","value":4}]}`), nil
	}
	got, err := queryXrayUserStats("xray", "127.0.0.1:60085")
	if err != nil || calls != 2 || len(got) != 1 || got[0].Up != 4 {
		t.Fatalf("calls %d err %v %+v", calls, err, got)
	}
}

func TestBeginCollectPrefersV2RayStats(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.last["singbox"] = []byte(`{"experimental":{"v2ray_api":{"listen":"127.0.0.1:60191","stats":{"enabled":true,"users":["u1.i2"]}}}}`)
	n := 0
	old := queryV2RayStatsFn
	queryV2RayStatsFn = func(ctx context.Context, addr string) ([]statRow, error) {
		if addr != "127.0.0.1:60191" {
			t.Errorf("addr %s", addr)
		}
		n++
		up := int64(100)
		if n > 1 {
			up = 130
		}
		return []statRow{
			{Name: "user>>>u1.i2>>>traffic>>>uplink", Value: up},
			{Name: "user>>>u1.i2>>>traffic>>>downlink", Value: 40},
		}, nil
	}
	defer func() { queryV2RayStatsFn = old }()
	oldHTTP := clashHTTP
	clashHTTP = &http.Client{Transport: tripFunc(func(r *http.Request) (*http.Response, error) {
		t.Error("clash queried while v2ray stats succeeded")
		return nil, fmt.Errorf("clash")
	})}
	defer func() { clashHTTP = oldHTTP }()

	if got := c.Collect(); len(got) != 0 {
		t.Fatalf("baseline %+v", got)
	}
	got := c.Collect()
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 30 || got[0].Down != 0 {
		t.Fatalf("delta %+v", got)
	}
	if len(c.clashLast) != 0 {
		t.Fatal("clash cursor should be cleared")
	}
}

func TestBeginCollectCommitFalseKeepsCursor(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.last["singbox"] = []byte(`{"experimental":{"v2ray_api":{"listen":"127.0.0.1:60191","stats":{"enabled":true}}}}`)
	n := 0
	old := queryV2RayStatsFn
	queryV2RayStatsFn = func(ctx context.Context, addr string) ([]statRow, error) {
		n++
		return []statRow{{Name: "user>>>u1.i2>>>traffic>>>uplink", Value: int64(100 * n)}}, nil
	}
	defer func() { queryV2RayStatsFn = old }()
	samples, finish := c.BeginCollect()
	if len(samples) != 0 {
		t.Fatalf("baseline %+v", samples)
	}
	finish(false)
	samples, finish = c.BeginCollect()
	finish(true)
	if len(samples) != 0 {
		t.Fatalf("uncommitted cursor billed %+v", samples)
	}
}

func TestBeginCollectClashFallback(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.last["singbox"] = []byte(`{"experimental":{"clash_api":{"external_controller":"127.0.0.1:60190"}}}`)
	c.singboxAPI = "127.0.0.1:60190"
	called := false
	oldHTTP := clashHTTP
	clashHTTP = &http.Client{Transport: tripFunc(func(r *http.Request) (*http.Response, error) {
		called = true
		body := `{"connections":[{"id":"a","upload":1,"download":2,"metadata":{"user":"u9.i1"}}]}`
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
	defer func() { clashHTTP = oldHTTP }()
	if got := c.Collect(); len(got) != 0 {
		t.Fatalf("first sight %+v", got)
	}
	if !called {
		t.Fatal("expected clash fallback")
	}
}

func TestClashChainAttributesWithoutUsername(t *testing.T) {
	raw := []byte(`{"connections":[
		{"id":"a","upload":40,"download":80,"chains":["limit-1275068425"],"metadata":{}},
		{"id":"b","upload":5,"download":6,"chains":["direct"],"metadata":{}}
	]}`)
	conns := fillClashUsers(parseClashConnections(raw), map[uint32]string{0x4c000009: "u1.i2"})
	if len(conns) != 2 {
		t.Fatalf("conns %d", len(conns))
	}
	if conns[0].User != "u1.i2" || conns[1].User != "" {
		t.Fatalf("users %+v %+v", conns[0].User, conns[1].User)
	}
	_, next := clashDeltas(nil, conns, time.Now())
	samples, _ := clashDeltas(next, conns, time.Now().Add(5*time.Second))
	if len(samples) != 0 {
		t.Fatalf("unchanged connection billed %+v", samples)
	}
	conns[0].Up = 140
	conns[0].Down = 280
	samples, _ = clashDeltas(next, conns, time.Now().Add(5*time.Second))
	if len(samples) != 1 || samples[0].Email != "u1.i2" || samples[0].Up != 100 || samples[0].Down != 200 {
		t.Fatalf("chain sample %+v", samples)
	}
}

func TestParseClashAndDeltas(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	raw := []byte(`{"connections":[
		{"id":"a","upload":40,"download":80,"start":"2026-09-13T11:59:55Z","metadata":{"user":"u1.i3"}},
		{"id":"b","upload":1000,"download":2000,"start":"2026-09-13T10:00:00Z","metadata":{"user":"u2.i1"}},
		{"id":"c","upload":1,"download":1,"metadata":{"user":""}}
	]}`)
	conns := parseClashConnections(raw)
	if len(conns) != 2 {
		t.Fatalf("conns %d", len(conns))
	}
	samples, next := clashDeltas(nil, conns, now)
	if len(samples) != 0 {
		t.Fatalf("first sight should baseline, got %+v", samples)
	}
	if _, ok := next["a"]; !ok {
		t.Fatal("young conn should be tracked")
	}
	if _, ok := next["b"]; !ok {
		t.Fatal("old conn should be tracked")
	}
	conns[0].Up = 50
	conns[0].Down = 90
	samples, _ = clashDeltas(next, conns, now)
	if len(samples) != 1 || samples[0].Email != "u1.i3" || samples[0].Up != 10 || samples[0].Down != 10 {
		t.Fatalf("delta %+v", samples)
	}
}

func TestParseMitaMetricsAndDeltas(t *testing.T) {
	nested := []byte(`{"user":{"alice":{"UploadToInternet":{"Total":100},"DownloadFromInternet":20}}}`)
	got := parseMitaMetrics(nested)
	if len(got) != 1 || got[0].Email != "alice" || got[0].Up != 100 || got[0].Down != 20 {
		t.Fatalf("nested %+v", got)
	}
	flat := []byte(`{"user - bob - UploadToInternet":{"Total":7},"user - bob - DownloadFromInternet":9}`)
	got = parseMitaMetrics(flat)
	if len(got) != 1 || got[0].Email != "bob" || got[0].Up != 7 || got[0].Down != 9 {
		t.Fatalf("flat %+v", got)
	}
	first, next := mitaDeltas(nil, got)
	if len(first) != 0 {
		t.Fatalf("baseline %v", first)
	}
	got[0].Up = 17
	got[0].Down = 19
	delta, _ := mitaDeltas(next, got)
	if len(delta) != 1 || delta[0].Up != 10 || delta[0].Down != 10 {
		t.Fatalf("delta %+v", delta)
	}
	restart := []wsproto.Sample{{Email: "bob", Up: 3, Down: 4}}
	delta, _ = mitaDeltas(map[string]bytePair{"bob": {up: 17, down: 19}}, restart)
	if len(delta) != 1 || delta[0].Up != 3 || delta[0].Down != 4 {
		t.Fatalf("restart %+v", delta)
	}
}

func TestBeginCollectBillsSocketMarkWithoutCoreCounters(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.acctMarks = []wsproto.AcctMark{{Mark: 0x4c000009, Email: "u1.i2"}}
	old := readFlowSources
	n := 0
	readFlowSources = func() (flowSources, error) {
		n++
		sent := 1000
		if n > 1 {
			sent = 1600
		}
		return flowSources{
			conntrack: "ipv4 2 tcp 6 1 ESTABLISHED src=1.2.3.4 dst=10.0.0.5 sport=1 dport=443 packets=1 bytes=8000 src=10.0.0.5 dst=1.2.3.4 sport=443 dport=1 packets=1 bytes=9000 mark=0",
			ss:        fmt.Sprintf("tcp ESTAB 0 0 10.0.0.5:9 9.9.9.9:443 fwmark:0x4c000009\n\tcubic bytes_sent:%d bytes_received:50\n", sent),
			local:     map[string]struct{}{"10.0.0.5": {}},
		}, nil
	}
	defer func() { readFlowSources = old }()
	if got := c.Collect(); len(got) != 0 {
		t.Fatalf("baseline %+v", got)
	}
	got := c.Collect()
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 600 || got[0].Down != 0 {
		t.Fatalf("socket delta %+v", got)
	}
}

func TestBeginCollectEmptyGRPCUsesCLI(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.xrayAPI = "127.0.0.1:60085"
	c.xrayBin = "xray"
	oldG := queryXrayGRPCFn
	queryXrayGRPCFn = func(context.Context, string) ([]statRow, error) {
		return nil, nil
	}
	defer func() { queryXrayGRPCFn = oldG }()
	oldC := runXrayStatsQueryFn
	n := 0
	runXrayStatsQueryFn = func(bin, api string, pattern bool) ([]byte, error) {
		n++
		up := 100
		if n > 1 {
			up = 180
		}
		return []byte(fmt.Sprintf(`{"stat":[{"name":"user>>>u1.i2>>>traffic>>>uplink","value":%d},{"name":"user>>>u1.i2>>>traffic>>>downlink","value":40}]}`, up)), nil
	}
	defer func() { runXrayStatsQueryFn = oldC }()
	oldF := readFlowSources
	readFlowSources = func() (flowSources, error) { return flowSources{}, os.ErrNotExist }
	defer func() { readFlowSources = oldF }()
	if got := c.Collect(); len(got) != 0 {
		t.Fatalf("baseline %+v calls %d", got, n)
	}
	if n < 1 {
		t.Fatal("CLI was not queried after empty gRPC")
	}
	got := c.Collect()
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 80 || got[0].Down != 0 {
		t.Fatalf("cli delta %+v", got)
	}
}

func TestBeginCollectDoesNotDoubleBillTrackedUser(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.xrayAPI = "127.0.0.1:60085"
	c.acctMarks = []wsproto.AcctMark{{Mark: 0x4c000009, Email: "u1.i2"}}
	oldG := queryXrayGRPCFn
	n := 0
	queryXrayGRPCFn = func(context.Context, string) ([]statRow, error) {
		n++
		up := int64(1000)
		if n > 1 {
			up = 1250
		}
		return []statRow{
			{Name: "user>>>u1.i2>>>traffic>>>uplink", Value: up},
			{Name: "user>>>u1.i2>>>traffic>>>downlink", Value: 40},
		}, nil
	}
	defer func() { queryXrayGRPCFn = oldG }()
	oldF := readFlowSources
	fn := 0
	readFlowSources = func() (flowSources, error) {
		fn++
		return flowSources{
			ss: fmt.Sprintf("tcp ESTAB 0 0 10.0.0.5:9 9.9.9.9:443 fwmark:0x4c000009\n\tcubic bytes_sent:%d bytes_received:8000\n", 5000+fn*1000),
		}, nil
	}
	defer func() { readFlowSources = oldF }()
	if got := c.Collect(); len(got) != 0 {
		t.Fatalf("baseline %+v", got)
	}
	got := c.Collect()
	// Core uplink grew 250. The socket grew 1000 in the same interval.
	// Bill the larger side once, not 1250.
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 1000 || got[0].Down != 0 {
		t.Fatalf("double billed %+v", got)
	}
}

func TestBeginCollectBillsSocketWhenCoreCounterFrozen(t *testing.T) {
	c := NewCores(t.TempDir())
	defer c.Close()
	c.xrayAPI = "127.0.0.1:60085"
	c.acctMarks = []wsproto.AcctMark{{Mark: 0x4c000009, Email: "u1.i2"}}
	oldG := queryXrayGRPCFn
	queryXrayGRPCFn = func(context.Context, string) ([]statRow, error) {
		return []statRow{
			{Name: "user>>>u1.i2>>>traffic>>>uplink", Value: 1200},
			{Name: "user>>>u1.i2>>>traffic>>>downlink", Value: 3400},
		}, nil
	}
	defer func() { queryXrayGRPCFn = oldG }()
	oldF := readFlowSources
	n := 0
	readFlowSources = func() (flowSources, error) {
		n++
		sent := 5000
		recv := 8000
		if n > 1 {
			sent = 9000
			recv = 15000
		}
		return flowSources{
			ss: fmt.Sprintf("tcp ESTAB 0 0 10.0.0.5:9 9.9.9.9:443 fwmark:0x4c000009\n\tcubic bytes_sent:%d bytes_received:%d\n", sent, recv),
		}, nil
	}
	defer func() { readFlowSources = oldF }()
	if got := c.Collect(); len(got) != 0 {
		t.Fatalf("baseline %+v", got)
	}
	got := c.Collect()
	if len(got) != 1 || got[0].Email != "u1.i2" || got[0].Up != 4000 || got[0].Down != 7000 {
		t.Fatalf("frozen core hid socket %+v", got)
	}
}

func TestMaxMergeSamplesPerDirection(t *testing.T) {
	got := maxMergeSamples(
		[]wsproto.Sample{{Email: "u1.i2", Up: 10, Down: 0}, {Email: "u1.i2", Up: 5, Down: 1}},
		[]wsproto.Sample{{Email: "u1.i2", Up: 4, Down: 80}},
	)
	if len(got) != 1 || got[0].Up != 15 || got[0].Down != 80 {
		t.Fatalf("%+v", got)
	}
}

func TestMergeSamples(t *testing.T) {
	got := mergeSamples([]wsproto.Sample{
		{Email: "a", Up: 1, Down: 2},
		{Email: "a", Up: 3, Down: 4},
		{Email: "", Up: 9, Down: 9},
		{Email: "b", Up: 0, Down: 0},
	})
	if len(got) != 1 || got[0].Email != "a" || got[0].Up != 4 || got[0].Down != 6 {
		t.Fatalf("%+v", got)
	}
}
