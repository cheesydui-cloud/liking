package corecfg

import (
	"encoding/json"
	"testing"

	"liking/internal/db"
)

func TestBuildXrayAndMita(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "v1", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, in); err != nil {
		t.Fatal(err)
	}
	m := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 8964, Enabled: true, LineKind: "direct", Settings: `{"transport":"TCP"}`,
	}
	if err := Normalize(m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, m); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Xray) == 0 || len(b.Apply.Mita) == 0 {
		t.Fatalf("expected xray+mita, got xray=%d mita=%d singbox=%d", len(b.Apply.Xray), len(b.Apply.Mita), len(b.Apply.Singbox))
	}
	var mita map[string]any
	if err := json.Unmarshal(b.Apply.Mita, &mita); err != nil {
		t.Fatal(err)
	}
	if mita["mtu"] != float64(1400) {
		t.Fatalf("mtu %v", mita["mtu"])
	}
	dns, _ := mita["dns"].(map[string]any)
	if dns["dualStack"] != "PREFER_IPv4" {
		t.Fatalf("dns %v", mita["dns"])
	}
	if b.Rev == "" {
		t.Fatal("rev")
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	if _, ok := xray["inbounds"]; !ok {
		t.Fatal("xray inbounds")
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	apiRule := ""
	for _, r := range rules {
		m, _ := r.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		for _, t := range tags {
			if t == "api" {
				apiRule, _ = m["outboundTag"].(string)
			}
		}
	}
	if apiRule != "api" {
		t.Fatalf("api inbound must route to api module, got %q", apiRule)
	}
	for _, o := range xray["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == "api-out" {
			t.Fatal("freedom api-out loops the stats API onto itself")
		}
	}
	foundDest := ""
	for _, raw := range xray["inbounds"].([]any) {
		m, _ := raw.(map[string]any)
		ss, _ := m["streamSettings"].(map[string]any)
		if ss == nil {
			continue
		}
		rs, _ := ss["realitySettings"].(map[string]any)
		if rs == nil {
			continue
		}
		foundDest, _ = rs["dest"].(string)
		if xver, ok := rs["xver"].(float64); !ok || xver != 0 {
			t.Fatalf("xver %v", rs["xver"])
		}
	}
	if foundDest != DefaultRealityDest {
		t.Fatalf("reality dest %s", foundDest)
	}
}

func TestProvisionKeepsMieruPasswordWhenDisabled(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 54675, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{srv.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetClient(d, created.ID, u.ID)
	if err != nil || c.Password == "" || !c.Enabled {
		t.Fatalf("client %+v %v", c, err)
	}
	pw := c.Password
	user := c.Username
	created.Enabled = false
	if err := db.UpdateInbound(d, created); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err = db.GetClient(d, created.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != pw || c.Username != user {
		t.Fatalf("password rotated on disable: %s/%s -> %s/%s", user, pw, c.Username, c.Password)
	}
	if c.Enabled {
		t.Fatal("disabled inbound should disable client")
	}
	created.Enabled = true
	if err := db.UpdateInbound(d, created); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err = db.GetClient(d, created.ID, u.ID)
	if err != nil || !c.Enabled || c.Password != pw || c.Username != user {
		t.Fatalf("after enable %+v %v", c, err)
	}
}

func TestBuildIncludesMitaWhenCoreNotReported(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkServerOnline(d, srv.ID, "0.1.6", "linux", "amd64", "10.0.0.1", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "v1", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, in); err != nil {
		t.Fatal(err)
	}
	m := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 8964, Enabled: true, LineKind: "direct", Settings: `{"transport":"TCP"}`,
	}
	if err := Normalize(m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, m); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Xray) == 0 {
		t.Fatal("xray")
	}
	if len(b.Apply.Mita) == 0 {
		t.Fatal("mita config must be sent so the agent can install the core")
	}
}

func TestBuildSS2022TCPUDP(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "jp", "", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkServerOnline(d, srv.ID, "0.1.9", "linux", "amd64", "177.5.54.5", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "ss", Profile: ProfileSS2022,
		Port: 8789, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{srv.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Xray) == 0 {
		t.Fatal("xray")
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range xray["inbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "shadowsocks" {
			continue
		}
		found = true
		st, _ := obj["settings"].(map[string]any)
		if st["network"] != "tcp,udp" {
			t.Fatalf("network %v", st["network"])
		}
		clients, _ := st["clients"].([]any)
		if len(clients) != 1 {
			t.Fatalf("clients %v", st["clients"])
		}
		if obj["port"] != float64(created.Port) {
			t.Fatalf("port %v", obj["port"])
		}
	}
	if !found {
		t.Fatal("missing shadowsocks inbound")
	}
}

func TestBuildSingboxClashAPI(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	cert, err := db.CreateCert(d, &db.Certificate{
		Name: "c1", CertPEM: "-----BEGIN CERTIFICATE-----\nA\n-----END CERTIFICATE-----",
		KeyPEM: "-----BEGIN PRIVATE KEY-----\nB\n-----END PRIVATE KEY-----", Domains: "a.example",
	})
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "a1", Profile: ProfileAnyTLS,
		Port: 8443, Enabled: true, LineKind: "direct", Settings: "{}", CertID: &cert.ID,
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, in); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Singbox) == 0 || b.Apply.SingboxAPI == "" {
		t.Fatalf("singbox api %q cfg %d", b.Apply.SingboxAPI, len(b.Apply.Singbox))
	}
	var cfg map[string]any
	if err := json.Unmarshal(b.Apply.Singbox, &cfg); err != nil {
		t.Fatal(err)
	}
	exp, _ := cfg["experimental"].(map[string]any)
	clash, _ := exp["clash_api"].(map[string]any)
	if clash["external_controller"] != b.Apply.SingboxAPI {
		t.Fatalf("controller %v api %s", clash["external_controller"], b.Apply.SingboxAPI)
	}
}
