package server

import (
	"testing"

	"liking/internal/corecfg"
	"liking/internal/wsproto"
)

func TestWireGuardNodeCardUsesCustomerRate(t *testing.T) {
	up, down, ok := inboundLiveRates(corecfg.ProfileWireGuard, 0, 0, true, 1200, 3400, true)
	if !ok || up != 1200 || down != 3400 {
		t.Fatalf("zero port hid customer rate %d %d %v", up, down, ok)
	}
	up, down, ok = inboundLiveRates(corecfg.ProfileWireGuard, 50, 80, true, 1200, 3400, true)
	if !ok || up != 1200 || down != 3400 {
		t.Fatalf("port rate replaced customer rate %d %d %v", up, down, ok)
	}
	up, down, ok = inboundLiveRates(corecfg.ProfileWireGuard, 50, 0, true, 0, 0, false)
	if !ok || up != 50 || down != 0 {
		t.Fatalf("agent overlay %d %d %v", up, down, ok)
	}
	up, down, ok = inboundLiveRates("vless", 0, 0, true, 9, 9, true)
	if !ok || up != 0 || down != 0 {
		t.Fatalf("other profile must keep the listen-port rate %d %d %v", up, down, ok)
	}
}

func TestPortLiveIsDisplayOnly(t *testing.T) {
	h := NewHub(nil)
	h.notePorts(9, wsproto.Stats{
		HasPorts: true,
		Ports:    []wsproto.PortRate{{Port: 443, UpBps: 1000, DownBps: 2000}, {Port: 8443, UpBps: 5, DownBps: 0}},
	})
	up, down, ok := h.PortLive(9, 443)
	if !ok || up != 1000 || down != 2000 {
		t.Fatalf("443 %d %d %v", up, down, ok)
	}
	up, down, ok = h.PortLive(9, 8443)
	if !ok || up != 5 || down != 0 {
		t.Fatalf("8443 %d %d %v", up, down, ok)
	}
	if _, _, ok := h.PortLive(9, 1); ok {
		t.Fatal("missing port")
	}
	h.notePorts(9, wsproto.Stats{HasPorts: false, Ports: []wsproto.PortRate{{Port: 443, UpBps: 1, DownBps: 1}}})
	up, _, ok = h.PortLive(9, 443)
	if !ok || up != 1000 {
		t.Fatalf("empty frame cleared port live: %d %v", up, ok)
	}
}
