package corecfg

import (
	"strings"
	"testing"

	"liking/internal/db"
)

func TestXHTTPRealityShareAndInbound(t *testing.T) {
	in := &db.Inbound{
		ID: 9, Name: "xr", Profile: ProfileVLESSXHTTPReality, Port: 443,
		Listen: "0.0.0.0", ServerHost: "ex.com", LineKind: "direct", Enabled: true, Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if NeedTLS(in.Profile) || !CanLand(in.Profile) || in.Core != CoreXray || in.Network != "xhttp" || in.Security != "reality" {
		t.Fatalf("spec %+v", in)
	}
	st := ParseSettings(in.Settings)
	if st.String("path") == "" || !strings.HasPrefix(st.String("path"), "/") || st.String("public_key") == "" || st.String("mode") != "auto" {
		t.Fatalf("settings %s", in.Settings)
	}
	c := &db.Client{UUID: "11111111-1111-4111-8111-111111111111", Email: "u.i9", Enabled: true}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "vless://") || !strings.Contains(uri, "type=xhttp") || !strings.Contains(uri, "security=reality") || strings.Contains(uri, "flow=") {
		t.Fatalf("uri %s", uri)
	}
	if !strings.Contains(uri, "pbk=") || !strings.Contains(uri, "path=") {
		t.Fatalf("uri %s", uri)
	}
	obj, err := xrayInbound(in, []*db.Client{c}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	stream, _ := obj["streamSettings"].(map[string]any)
	if stream["network"] != "xhttp" || stream["security"] != "reality" || stream["xhttpSettings"] == nil || stream["realitySettings"] == nil {
		t.Fatalf("stream %+v", stream)
	}
	settings, _ := obj["settings"].(map[string]any)
	clients, _ := settings["clients"].([]any)
	user, _ := clients[0].(map[string]any)
	if user["flow"] != nil {
		t.Fatalf("flow %+v", user)
	}
	_, yaml, err := ClashProxyYAML(in, c)
	if err != nil || !strings.Contains(yaml, "network: xhttp") || !strings.Contains(yaml, "reality-opts:") || !strings.Contains(yaml, "xhttp-opts:") || strings.Contains(yaml, "flow:") {
		t.Fatalf("clash %v %s", err, yaml)
	}
	ob, err := SingboxOutbound(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "vless" || ob["flow"] != nil {
		t.Fatalf("outbound %+v", ob)
	}
	tr, _ := ob["transport"].(map[string]any)
	tls, _ := ob["tls"].(map[string]any)
	reality, _ := tls["reality"].(map[string]any)
	if tr["type"] != "xhttp" || reality["enabled"] != true {
		t.Fatalf("outbound %+v", ob)
	}
	land, err := xrayLandOutbound("to-xr", in, Settings{"relay_uuid": c.UUID})
	if err != nil {
		t.Fatal(err)
	}
	ls, _ := land["streamSettings"].(map[string]any)
	if ls["network"] != "xhttp" || ls["security"] != "reality" {
		t.Fatalf("land %+v", ls)
	}
}

func TestHysteria2ShareAndInbound(t *testing.T) {
	certID := int64(1)
	in := &db.Inbound{
		ID: 8, Name: "hy", Profile: ProfileHysteria2, Port: 36712,
		Listen: "0.0.0.0", ServerHost: "hy.example", LineKind: "direct", Enabled: true,
		Settings: `{"sni":"hy.example"}`, CertID: &certID,
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if !NeedTLS(in.Profile) || CanLand(in.Profile) || CoreFor(in.Profile) != CoreSingbox || in.Network != "udp" {
		t.Fatalf("spec %+v", in)
	}
	st := ParseSettings(in.Settings)
	if !st.Bool("obfs", false) || st.String("obfs_password") == "" || st.String("masquerade") != DefaultHy2Masquerade {
		t.Fatalf("settings %s", in.Settings)
	}
	if up := st.Int("up_mbps", 0); up != 0 {
		t.Fatalf("bandwidth %s", in.Settings)
	}
	c := &db.Client{Email: "u5.i8", Password: "pw", Enabled: true, ID: 12, UserID: 5, InboundID: 8}
	uri, err := ShareURI(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(uri, "hysteria2://pw@hy.example:36712?") || !strings.Contains(uri, "obfs=salamander") || !strings.Contains(uri, "obfs-password=") || !strings.Contains(uri, "alpn=h3") || strings.Contains(uri, "upmbps=") {
		t.Fatalf("uri %s", uri)
	}
	_, yaml, err := ClashProxyYAML(in, c)
	if err != nil || !strings.Contains(yaml, "type: hysteria2") || !strings.Contains(yaml, "obfs: salamander") || strings.Contains(yaml, "up:") {
		t.Fatalf("clash %v %s", err, yaml)
	}
	ob, err := SingboxOutbound(in, c)
	if err != nil {
		t.Fatal(err)
	}
	if ob["type"] != "hysteria2" || ob["up_mbps"] != nil || ob["obfs"] == nil {
		t.Fatalf("outbound %+v", ob)
	}
	certs := map[int64]*db.Certificate{1: {ID: 1, CertPEM: "cert", KeyPEM: "key"}}
	cfg, err := buildSingbox([]*db.Inbound{in}, map[int64][]*db.Client{8: {c}}, certs, nil, 60190, 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawIns := cfg["inbounds"].([]any)
	ib, _ := rawIns[0].(map[string]any)
	if ib["type"] != "hysteria2" || ib["ignore_client_bandwidth"] != true || ib["up_mbps"] != nil {
		t.Fatalf("inbound %+v", ib)
	}
	tls, _ := ib["tls"].(map[string]any)
	alpn, _ := tls["alpn"].([]string)
	if len(alpn) != 1 || alpn[0] != "h3" {
		t.Fatalf("alpn %#v", tls["alpn"])
	}
	mask, _ := ib["masquerade"].(map[string]any)
	if mask["type"] != "proxy" || mask["url"] != DefaultHy2Masquerade {
		t.Fatalf("masquerade %+v", mask)
	}
	var marked bool
	for _, raw := range cfg["outbounds"].([]any) {
		o, _ := raw.(map[string]any)
		if o["routing_mark"] != nil {
			marked = true
		}
	}
	if !marked {
		t.Fatal("missing per-user mark")
	}
}

func TestHysteria2BandwidthAndRejects(t *testing.T) {
	in := &db.Inbound{Name: "h", Profile: ProfileHysteria2, Port: 443, ServerHost: "hy.example", LineKind: "direct", Settings: `{"up_mbps":50}`}
	if err := Normalize(in, nil); err == nil {
		t.Fatal("expected one-sided bandwidth reject")
	}
	in.Settings = `{"up_mbps":50,"down_mbps":100,"obfs":false,"masquerade":"not-a-url"}`
	if err := Normalize(in, nil); err == nil {
		t.Fatal("expected masquerade reject")
	}
	in.Settings = `{"up_mbps":50,"down_mbps":100,"obfs":false,"sni":"hy.example"}`
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	st := ParseSettings(in.Settings)
	if st.Bool("obfs", true) || st.String("obfs_password") != "" || st.Int("up_mbps", 0) != 50 || st.Int("down_mbps", 0) != 100 {
		t.Fatalf("settings %s", in.Settings)
	}
	c := &db.Client{Password: "pw", Email: "u"}
	uri, err := ShareURI(in, c)
	if err != nil || strings.Contains(uri, "obfs=") || !strings.Contains(uri, "upmbps=50") || !strings.Contains(uri, "downmbps=100") {
		t.Fatalf("uri %s %v", uri, err)
	}
	entry := &db.Inbound{Name: "in", Profile: ProfileVLESSReality, Port: 443, LineKind: "chain", Settings: "{}"}
	land := &db.Inbound{ID: 2, Name: "h", Profile: ProfileHysteria2, LineKind: "direct"}
	if err := Normalize(entry, land); err == nil || !strings.Contains(err.Error(), "Hysteria2") {
		t.Fatalf("land %v", err)
	}
}
