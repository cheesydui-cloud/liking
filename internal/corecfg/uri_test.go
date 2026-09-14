package corecfg

import (
	"encoding/base64"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestParseShareURIRoundtrip(t *testing.T) {
	in := &db.Inbound{
		Name: "HK", Profile: ProfileVLESSRealityVision, Port: 443,
		ServerHost: "example.com", LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	c := &db.Client{UUID: "11111111-1111-4111-8111-111111111111", Password: "trojan-pass"}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseShareURI(uri)
	if err != nil {
		t.Fatal(err)
	}
	if got.Scheme != "vless" || got.Host != "example.com" || got.Port != 443 {
		t.Fatalf("vless %+v", got)
	}
	if got.UUID != c.UUID || got.Security != "reality" || got.Flow != "xtls-rprx-vision" {
		t.Fatalf("vless fields %+v", got)
	}
	st := ParseSettings(in.Settings)
	if got.PublicKey != st.String("public_key") {
		t.Fatalf("pbk %s vs %s", got.PublicKey, st.String("public_key"))
	}

	ss := &db.Inbound{
		Name: "ss", Profile: ProfileSS2022, Port: 8388,
		ServerHost: "1.2.3.4", LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(ss, nil); err != nil {
		t.Fatal(err)
	}
	ssURI, err := ShareURI(ss, c)
	if err != nil {
		t.Fatal(err)
	}
	ssGot, err := ParseShareURI(ssURI)
	if err != nil {
		t.Fatal(err)
	}
	if ssGot.Scheme != "ss" || ssGot.Host != "1.2.3.4" || ssGot.Port != 8388 || ssGot.Method == "" || ssGot.Password == "" {
		t.Fatalf("ss %+v uri %s", ssGot, ssURI)
	}

	tr := &db.Inbound{
		Name: "tr", Profile: ProfileTrojanTLS, Port: 443,
		ServerHost: "trojan.example", LineKind: "direct",
		Settings: `{"sni":"trojan.example"}`,
	}
	if err := Normalize(tr, nil); err != nil {
		t.Fatal(err)
	}
	trURI, err := ShareURI(tr, c)
	if err != nil {
		t.Fatal(err)
	}
	trGot, err := ParseShareURI(trURI)
	if err != nil {
		t.Fatal(err)
	}
	if trGot.Scheme != "trojan" || trGot.Host != "trojan.example" || trGot.Password != c.Password || trGot.Security != "tls" {
		t.Fatalf("trojan %+v uri %s", trGot, trURI)
	}
}

func TestParseShareURIVariants(t *testing.T) {
	vless := "vless://11111111-1111-4111-8111-111111111111@203.0.113.10:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=abc&sid=abcd&type=tcp&flow=xtls-rprx-vision#ext"
	t1, err := ParseShareURI("  \n" + vless + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if t1.Host != "203.0.113.10" || t1.Name != "ext" || t1.PublicKey != "abc" {
		t.Fatalf("%+v", t1)
	}

	ssSIP := "ss://YWVzLTI1Ni1nY206cGFzcw@1.2.3.4:8388#n1"
	t2, err := ParseShareURI(ssSIP)
	if err != nil {
		t.Fatal(err)
	}
	if t2.Method != "aes-256-gcm" || t2.Password != "pass" || t2.Host != "1.2.3.4" {
		t.Fatalf("sip002 %+v", t2)
	}

	plain := "ss://aes-256-gcm:secret@9.9.9.9:1000"
	t3, err := ParseShareURI(plain)
	if err != nil {
		t.Fatal(err)
	}
	if t3.Method != "aes-256-gcm" || t3.Password != "secret" || t3.Port != 1000 {
		t.Fatalf("plain %+v", t3)
	}

	legacy := "ss://" + base64.StdEncoding.EncodeToString([]byte("aes-256-gcm:pass@8.8.8.8:8388")) + "#n2"
	t4, err := ParseShareURI(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if t4.Method != "aes-256-gcm" || t4.Password != "pass" || t4.Host != "8.8.8.8" || t4.Name != "n2" {
		t.Fatalf("legacy %+v", t4)
	}

	if _, err := ParseShareURI("ss://YWVzLTI1Ni1nY206cGFzcw@1.2.3.4:8388/?plugin=obfs-local"); err == nil || !strings.Contains(err.Error(), "插件") {
		t.Fatalf("plugin %v", err)
	}

	sk, err := ParseShareURI("socks5://u:p@203.0.113.9:1080#sk")
	if err != nil || sk.SocksTarget() == nil || sk.SocksTarget().User != "u" {
		t.Fatalf("socks %+v %v", sk, err)
	}

	if _, err := ParseShareURI("vmess://abc"); err == nil || !strings.Contains(err.Error(), "vmess") {
		t.Fatalf("vmess %v", err)
	}
	if _, err := ParseShareURI("vless://x"); err == nil {
		t.Fatal("expected vless host error")
	}
}

func TestNormalizeChainShareExitURI(t *testing.T) {
	entry := &db.Inbound{
		Name: "in", Profile: ProfileVLESSReality, Port: 8443, LineKind: "chain",
		ExitURI:  "vless://11111111-1111-4111-8111-111111111111@203.0.113.10:443?security=reality&pbk=abc&sid=1&sni=www.microsoft.com&type=tcp#ext",
		Settings: "{}",
	}
	if err := Normalize(entry, nil); err != nil {
		t.Fatal(err)
	}
	if entry.ExitInboundID != nil {
		t.Fatal("exit inbound should be cleared")
	}
	path, err := ChainPath(entry, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 1 || path[0].Share == nil || path[0].Share.Host != "203.0.113.10" {
		t.Fatalf("path %+v", path)
	}
	ob, err := xrayPathOutbound(path[0])
	if err != nil {
		t.Fatal(err)
	}
	if ob["protocol"] != "vless" {
		t.Fatalf("ob %+v", ob)
	}
	stream, _ := ob["streamSettings"].(map[string]any)
	if stream["security"] != "reality" {
		t.Fatalf("stream %+v", stream)
	}

	trojan := &db.Inbound{
		Name: "in2", Profile: ProfileVLESSReality, Port: 8444, LineKind: "chain",
		ExitURI:  "trojan://pw@203.0.113.11:443?security=tls&sni=a.example&type=tcp#t",
		Settings: "{}",
	}
	if err := Normalize(trojan, nil); err != nil {
		t.Fatal(err)
	}
	p2, err := ChainPath(trojan, nil)
	if err != nil {
		t.Fatal(err)
	}
	sob, err := singPathOutbound(p2[0])
	if err != nil || sob["type"] != "trojan" || sob["password"] != "pw" {
		t.Fatalf("sing %+v %v", sob, err)
	}

	if err := Normalize(&db.Inbound{
		Name: "bad", Profile: ProfileVLESSReality, Port: 1, LineKind: "chain",
		ExitURI: "vmess://x", Settings: "{}",
	}, nil); err == nil {
		t.Fatal("expected reject vmess")
	}
}

func TestNormalizeHopsShareURI(t *testing.T) {
	st := Settings{"hops": []any{
		map[string]any{"kind": "uri", "uri": "vless://11111111-1111-4111-8111-111111111111@1.1.1.1:443?security=reality&pbk=x&type=tcp"},
	}}
	if err := NormalizeHops(st); err != nil {
		t.Fatal(err)
	}
	hops := ParseHops(st)
	if len(hops) != 1 || hops[0].Kind != "socks" || hops[0].URI == "" {
		t.Fatalf("%+v", hops)
	}
}
