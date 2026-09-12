package corecfg

import (
	"encoding/base64"
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

func TestNormalizeMieruDefaultBoth(t *testing.T) {
	in := &db.Inbound{Name: "m", Profile: ProfileMieru, Port: 8444, LineKind: "direct", Settings: "{}"}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if ParseSettings(in.Settings).String("transport") != "BOTH" {
		t.Fatalf("transport %s", ParseSettings(in.Settings).String("transport"))
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
	if err != nil || name != "m" || !strings.Contains(yaml, "type: mieru") || !strings.Contains(yaml, `transport: "TCP"`) {
		t.Fatalf("clash %v %s", err, yaml)
	}
	inBoth := *in
	inBoth.Settings = `{"transport":"BOTH"}`
	_, yamlBoth, err := ClashProxyYAML(&inBoth, c)
	if err != nil || !strings.Contains(yamlBoth, `transport: "UDP"`) {
		t.Fatalf("clash both %v %s", err, yamlBoth)
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
	in.Settings = `{"transport":"BOTH"}`
	uri, err = ShareURI(in, c)
	if err != nil || !strings.Contains(uri, "udp=1") {
		t.Fatalf("both uri %s %v", uri, err)
	}
}

func TestShareHostFallsBackToConnectIP(t *testing.T) {
	in := &db.Inbound{ConnectIP: "177.5.54.5"}
	if ShareHost(in) != "177.5.54.5" {
		t.Fatalf("fallback %q", ShareHost(in))
	}
	in.ServerHost = "jp.example"
	if ShareHost(in) != "jp.example" {
		t.Fatalf("public host %q", ShareHost(in))
	}
}

func TestShareSS2022SIP002(t *testing.T) {
	in := &db.Inbound{
		Name: "jp", Profile: ProfileSS2022, Port: 8789, ServerHost: "1.2.3.4",
		Settings: `{"method":"2022-blake3-aes-128-gcm","server_password":"g4HmbRR/PV7edxBymvePcg=="}`,
	}
	c := &db.Client{Password: "dGVzdHBhc3N3b3JkMTI="}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "ss://") || !strings.Contains(uri, "@1.2.3.4:8789") {
		t.Fatalf("uri %s", uri)
	}
	userinfo := strings.TrimPrefix(uri, "ss://")
	userinfo = userinfo[:strings.Index(userinfo, "@")]
	raw, err := base64.RawURLEncoding.DecodeString(userinfo)
	if err != nil {
		t.Fatalf("sip002 %v %s", err, uri)
	}
	got := string(raw)
	if !strings.HasPrefix(got, "2022-blake3-aes-128-gcm:g4HmbRR/PV7edxBymvePcg==:") {
		t.Fatalf("decoded %s", got)
	}
	if strings.Contains(got, "%") {
		t.Fatalf("password should not be percent-encoded %s", got)
	}

	in.ServerHost = ""
	in.ConnectIP = "177.5.54.5"
	uri, err = ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(uri, "@177.5.54.5:8789") {
		t.Fatalf("connect ip uri %s", uri)
	}
}
