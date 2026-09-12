package corecfg

import (
	"strings"
	"testing"

	"liking/internal/db"
)

func TestNormalizeRealityAndURI(t *testing.T) {
	in := &db.Inbound{
		Name: "HK", Profile: ProfileVLESSRealityVision, Port: 443,
		ServerHost: "example.com", LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if in.Core != CoreXray || in.Security != "reality" {
		t.Fatalf("spec %s %s", in.Core, in.Security)
	}
	st := ParseSettings(in.Settings)
	if st.String("private_key") == "" || st.String("public_key") == "" {
		t.Fatal("keys")
	}
	c := &db.Client{UUID: "11111111-1111-4111-8111-111111111111"}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "vless://") || !strings.Contains(uri, "security=reality") || !strings.Contains(uri, "flow=xtls-rprx-vision") {
		t.Fatalf("uri %s", uri)
	}
}

func TestMieruCannotLand(t *testing.T) {
	entry := &db.Inbound{Name: "in", Profile: ProfileVLESSReality, Port: 443, LineKind: "chain", Settings: "{}"}
	land := &db.Inbound{ID: 2, Name: "m", Profile: ProfileMieru, LineKind: "direct"}
	if err := Normalize(entry, land); err == nil {
		t.Fatal("expected reject")
	}
}

func TestAnyTLSNeedTLS(t *testing.T) {
	if !NeedTLS(ProfileAnyTLS) || CoreFor(ProfileAnyTLS) != CoreSingbox {
		t.Fatal("anytls")
	}
	if CoreFor(ProfileMieru) != CoreMita {
		t.Fatal("mieru core")
	}
}

func TestClashAndSingboxSkipMieru(t *testing.T) {
	in := &db.Inbound{
		Name: "m", Profile: ProfileMieru, Port: 8964, ServerHost: "1.2.3.4",
		Settings: `{"transport":"TCP"}`,
	}
	c := &db.Client{Username: "u1", Password: "p"}
	name, yaml, err := ClashProxyYAML(in, c)
	if err != nil || name != "m" || !strings.Contains(yaml, "type: mieru") {
		t.Fatalf("clash %v %s", err, yaml)
	}
	_, err = SingboxOutbound(in, c)
	if err != ErrSkip {
		t.Fatalf("skip %v", err)
	}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "mierus://") {
		t.Fatalf("uri %s", uri)
	}
	if !strings.Contains(uri, "udp=0") || !strings.Contains(uri, "port=8964") || !strings.Contains(uri, "profile=default") {
		t.Fatalf("uri %s", uri)
	}
	if strings.Contains(uri, "1.2.3.4:8964") {
		t.Fatalf("port must be in query %s", uri)
	}
	in.Settings = `{"transport":"UDP"}`
	uri, err = ShareURI(in, c)
	if err != nil || !strings.Contains(uri, "udp=1") {
		t.Fatalf("udp uri %s %v", uri, err)
	}
}
