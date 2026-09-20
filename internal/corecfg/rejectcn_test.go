package corecfg

import (
	"net"
	"testing"

	"liking/internal/db"
)

func TestCollectRejectCNAllowsChainEntry(t *testing.T) {
	landID := int64(2)
	entrySrv := &db.Server{ID: 1, PublicHost: "203.0.113.10", ConnectIP: "203.0.113.11"}
	land := &db.Inbound{
		ID: landID, ServerID: 2, Name: "jp", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "direct", RejectCN: true,
		ServerHost: "198.51.100.8",
	}
	entry := &db.Inbound{
		ID: 3, ServerID: 1, Name: "cn-in", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "chain", ExitInboundID: &landID,
		ServerHost: "203.0.113.10", ConnectIP: "203.0.113.11",
	}
	got := collectRejectCN([]*db.Inbound{land, entry}, 2, func(id int64) *db.Server {
		if id == 1 {
			return entrySrv
		}
		return nil
	})
	if len(got.Ports) != 1 || got.Ports[0] != 8443 {
		t.Fatalf("ports %+v", got.Ports)
	}
	if len(got.Allow) != 2 || got.Allow[0] != "203.0.113.10" || got.Allow[1] != "203.0.113.11" {
		t.Fatalf("allow %+v", got.Allow)
	}
}

func TestCollectRejectCNSkipsChainAndOff(t *testing.T) {
	chain := &db.Inbound{ID: 1, ServerID: 9, Port: 443, Enabled: true, LineKind: "chain", RejectCN: true}
	off := &db.Inbound{ID: 2, ServerID: 9, Port: 8443, Enabled: true, LineKind: "direct", RejectCN: false}
	got := collectRejectCN([]*db.Inbound{chain, off}, 9, func(int64) *db.Server { return nil })
	if len(got.Ports) != 0 || len(got.Allow) != 0 {
		t.Fatalf("%+v", got)
	}
}

func TestCollectRejectCNExitURI(t *testing.T) {
	land := &db.Inbound{
		ID: 5, ServerID: 2, Port: 8443, Enabled: true, LineKind: "direct", RejectCN: true,
		ServerHost: "198.51.100.8",
	}
	entry := &db.Inbound{
		ID: 6, ServerID: 1, Port: 443, Enabled: true, LineKind: "chain",
		ExitURI:    "vless://abcd@198.51.100.8:8443?encryption=none&security=reality&type=tcp&sni=www.cloudflare.com&fp=chrome&pbk=aaaaaaaaaaaa&sid=abcd#x",
		ServerHost: "203.0.113.9",
	}
	got := collectRejectCN([]*db.Inbound{land, entry}, 2, func(int64) *db.Server {
		return &db.Server{PublicHost: "203.0.113.9"}
	})
	if len(got.Allow) != 1 || got.Allow[0] != "203.0.113.9" {
		t.Fatalf("allow %+v", got.Allow)
	}
}

func TestHostAddrsIPAndLookup(t *testing.T) {
	if got := hostAddrs(" 10.1.2.3 "); len(got) != 1 || got[0] != "10.1.2.3" {
		t.Fatalf("%v", got)
	}
	old := lookupIP
	lookupIP = func(host string) ([]net.IP, error) {
		if host != "entry.example" {
			t.Fatalf("host %s", host)
		}
		return []net.IP{net.ParseIP("192.0.2.8"), net.ParseIP("192.0.2.8")}, nil
	}
	defer func() { lookupIP = old }()
	got := hostAddrs("entry.example")
	if len(got) != 1 || got[0] != "192.0.2.8" {
		t.Fatalf("%v", got)
	}
}
