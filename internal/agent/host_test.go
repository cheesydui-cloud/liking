package agent

import "testing"

func TestParseMeminfo(t *testing.T) {
	raw := []byte("MemTotal:       2048000 kB\nMemFree:         100000 kB\nMemAvailable:    512000 kB\n")
	avail, total := parseMeminfo(raw)
	if total != 2048000*1024 || avail != 512000*1024 {
		t.Fatalf("got avail=%d total=%d", avail, total)
	}
}

func TestParseLoadMilli(t *testing.T) {
	if n := parseLoadMilli([]byte("0.42 0.30 0.20 1/123 1\n")); n != 420 {
		t.Fatalf("got %d", n)
	}
}

func TestParseTCPEstablished(t *testing.T) {
	raw := []byte("  sl  local_address rem_address   st\n   0: 00000000:20FB 0100007F:1234 01\n   1: 00000000:20FB 0100007F:1235 0A\n   2: 0100007F:1F90 00000000:0000 01\n")
	want := map[int]struct{}{8443: {}}
	if n := parseTCPEstablished(raw, want); n != 1 {
		t.Fatalf("got %d", n)
	}
}

func TestHexPort(t *testing.T) {
	if p := hexPort("00000000:20FB"); p != 8443 {
		t.Fatalf("got %d", p)
	}
}
