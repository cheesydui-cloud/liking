package agent

import (
	"testing"
	"time"

	"liking/internal/wsproto"
)

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
