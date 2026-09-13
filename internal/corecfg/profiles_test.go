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

func TestKnownPortForwardNotInCatalog(t *testing.T) {
	if !Known(ProfilePortForward) {
		t.Fatal("port-forward should be known")
	}
	if UserFacing(ProfilePortForward) {
		t.Fatal("port-forward is not user-facing")
	}
	if !UserFacing(ProfileVLESSReality) {
		t.Fatal("vless is user-facing")
	}
	for _, m := range Catalog() {
		if m.ID == ProfilePortForward {
			t.Fatal("port-forward must not appear in 增加节点 catalog")
		}
	}
}

func TestNormalizeChainExitURI(t *testing.T) {
	in := &db.Inbound{
		Profile: ProfileVLESSReality, Port: 8443, LineKind: "chain",
		ExitURI: "socks5://u:p@10.0.0.2:1080", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	st := ParseSettings(in.Settings)
	if st.String("relay_uuid") != "" {
		t.Fatalf("SK5 should not mint relay uuid: %+v", st)
	}
	land := &db.Inbound{ID: 9, Profile: ProfileVLESSReality, Port: 443, LineKind: "direct", Settings: "{}"}
	if err := Normalize(in, land); err == nil {
		t.Fatal("expected reject both landing and SK5")
	}
	bad := &db.Inbound{Profile: ProfileVLESSReality, Port: 8443, LineKind: "chain", ExitURI: "https://x", Settings: "{}"}
	if err := Normalize(bad, nil); err == nil {
		t.Fatal("expected reject non-socks uri")
	}
}

func TestNormalizePortForward(t *testing.T) {
	in := &db.Inbound{
		Profile: ProfilePortForward, Port: 10443, LineKind: "chain",
		Settings: `{"dest_host":"8.8.8.8","dest_port":443}`,
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if in.LineKind != "direct" || in.ExitURI != "" || in.ExitInboundID != nil {
		t.Fatalf("port-forward must be a pipe: %+v", in)
	}
	st := ParseSettings(in.Settings)
	if st.String("dest_host") != "8.8.8.8" || st.Int("dest_port", 0) != 443 || st.String("network") != "tcp" {
		t.Fatalf("settings %+v", st)
	}
	missing := &db.Inbound{Profile: ProfilePortForward, Port: 1, Settings: "{}"}
	if err := Normalize(missing, nil); err == nil {
		t.Fatal("expected dest required")
	}
}

func TestParseSocksURI(t *testing.T) {
	t1, err := ParseSocksURI("socks5://alice:s3cret@1.2.3.4:1080")
	if err != nil {
		t.Fatal(err)
	}
	if t1.Host != "1.2.3.4" || t1.Port != 1080 || t1.User != "alice" || t1.Pass != "s3cret" {
		t.Fatalf("%+v", t1)
	}
	t2, err := ParseSocksURI("socks://10.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if t2.Host != "10.0.0.1" || t2.Port != 1080 {
		t.Fatalf("%+v", t2)
	}
	got := FormatSocksURI(&SocksTarget{Host: "1.2.3.4", Port: 1080, User: "a", Pass: "b"})
	back, err := ParseSocksURI(got)
	if err != nil || back.User != "a" || back.Pass != "b" {
		t.Fatalf("roundtrip %s %+v %v", got, back, err)
	}
	if _, err := ParseSocksURI("vless://x"); err == nil {
		t.Fatal("expected reject")
	}
}

func TestShareURIPortForwardRejected(t *testing.T) {
	in := &db.Inbound{
		Name: "p", Profile: ProfilePortForward, Port: 10443, ServerHost: "ex.com",
		Settings: `{"dest_host":"1.1.1.1","dest_port":443}`,
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := ShareURI(in, &db.Client{UUID: "x"}); err == nil {
		t.Fatal("expected reject")
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
