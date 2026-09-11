package agent

import "testing"

func TestParseXrayStats(t *testing.T) {
	raw := []byte(`{"stat":[
		{"name":"user>>>u1.i1>>>traffic>>>uplink","value":"10"},
		{"name":"user>>>u1.i1>>>traffic>>>downlink","value":20},
		{"name":"inbound>>>in-1>>>traffic>>>uplink","value":99}
	]}`)
	s := parseXrayStats(raw)
	if len(s) != 1 || s[0].Email != "u1.i1" || s[0].Up != 10 || s[0].Down != 20 {
		t.Fatalf("%+v", s)
	}
}
