package corecfg

import (
	"strings"
	"testing"

	"liking/internal/db"
)

func TestNormalizeSecurityDefaults(t *testing.T) {
	in := &db.Inbound{Profile: ProfileVLESSRealityVision, Port: 8443, LineKind: "direct", Settings: "{}"}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	st := ParseSettings(in.Settings)
	if st.String("dest") != DefaultRealityDest {
		t.Fatalf("dest %s", st.String("dest"))
	}
	if st.String("fingerprint") != "chrome" {
		t.Fatalf("fp %s", st.String("fingerprint"))
	}
	if st.Int("xver", -1) != 0 {
		t.Fatalf("xver %d", st.Int("xver", -1))
	}
	if first(st.Strings("server_names")) != "www.microsoft.com" {
		t.Fatalf("sni %v", st.Strings("server_names"))
	}

	tr := &db.Inbound{Profile: ProfileTrojanTLS, Port: 8443, LineKind: "direct", Settings: "{}"}
	if err := Normalize(tr, nil); err != nil {
		t.Fatal(err)
	}
	ts := ParseSettings(tr.Settings)
	if ts.String("min_version") != "1.3" {
		t.Fatalf("min %s", ts.String("min_version"))
	}
	if !ts.Bool("reject_unknown_sni", false) {
		t.Fatal("reject_unknown_sni")
	}
	if got := strings.Join(ts.ALPN(), ","); got != "h2,http/1.1" {
		t.Fatalf("alpn %s", got)
	}
}

func TestNormalizeRejectsLocalRealityDest(t *testing.T) {
	in := &db.Inbound{Profile: ProfileVLESSReality, Port: 8443, LineKind: "direct", Settings: `{"dest":"127.0.0.1:443"}`}
	if err := Normalize(in, nil); err == nil {
		t.Fatal("expected reject")
	}
}

func TestNormalizeRejectsBadShortID(t *testing.T) {
	in := &db.Inbound{Profile: ProfileVLESSReality, Port: 8443, LineKind: "direct", Settings: `{"short_ids":["zz"]}`}
	if err := Normalize(in, nil); err == nil {
		t.Fatal("expected reject")
	}
}

func TestDestIsSelf(t *testing.T) {
	if !DestIsSelf("www.example.com:443", "www.example.com") {
		t.Fatal("same host")
	}
	if !DestIsSelf("1.2.3.4:443", "1.2.3.4") {
		t.Fatal("same ip")
	}
	if DestIsSelf("www.microsoft.com:443", "1.2.3.4", "node.example.com") {
		t.Fatal("foreign dest")
	}
	if DestIsSelf("www.microsoft.com:443", "") {
		t.Fatal("empty addr")
	}
}

func TestShareURIIncludesFingerprint(t *testing.T) {
	in := &db.Inbound{
		Name: "x", Profile: ProfileVLESSXHTTP, Port: 8443, ServerHost: "ex.com",
		LineKind: "direct", Settings: `{"path":"/ab","sni":"ex.com"}`,
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	c := &db.Client{UUID: "11111111-1111-4111-8111-111111111111"}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uri, "fp=chrome") || !strings.Contains(uri, "alpn=") {
		t.Fatalf("uri %s", uri)
	}
}
