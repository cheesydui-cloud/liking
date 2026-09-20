package corecfg

import (
	"encoding/json"
	"testing"

	"liking/internal/db"
)

func TestBuildDisableIPv6(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	b1, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Apply.DisableIPv6 {
		t.Fatal("default")
	}
	srv.DisableIPv6 = true
	if err := db.UpdateServer(d, srv); err != nil {
		t.Fatal(err)
	}
	b2, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !b2.Apply.DisableIPv6 {
		t.Fatal("flag")
	}
	if b1.Rev == b2.Rev {
		t.Fatalf("rev should change %s", b1.Rev)
	}
	srv.DisableIPv6 = false
	if err := db.UpdateServer(d, srv); err != nil {
		t.Fatal(err)
	}
	b3, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b3.Apply.DisableIPv6 || b3.Rev != b1.Rev {
		t.Fatalf("re-enable %+v rev %s want %s", b3.Apply.DisableIPv6, b3.Rev, b1.Rev)
	}
}

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

func TestProvisionAdminGetsClients(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	admin, err := db.CreateUser(d, "admin", "h", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
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
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, admin); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetClient(d, created.ID, admin.ID)
	if err != nil || c.UUID == "" || !c.Enabled {
		t.Fatalf("admin client %+v %v", c, err)
	}
}

func TestProvisionUserIncremental(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	admin, err := db.CreateUser(d, "admin", "h", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
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
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := ProvisionUser(d, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != srv.ID {
		t.Fatalf("first provision %+v", ids)
	}
	c1, err := db.GetClient(d, created.ID, admin.ID)
	if err != nil || c1.UUID == "" {
		t.Fatalf("client %+v %v", c1, err)
	}
	ids, err = ProvisionUser(d, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != srv.ID {
		t.Fatalf("unchanged still needs sync %+v", ids)
	}
	c2, err := db.GetClient(d, created.ID, admin.ID)
	if err != nil || c2.UUID != c1.UUID {
		t.Fatalf("uuid changed %+v -> %+v %v", c1, c2, err)
	}
	created.Enabled = false
	if err := db.UpdateInbound(d, created); err != nil {
		t.Fatal(err)
	}
	ids, err = ProvisionUser(d, admin)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != srv.ID {
		t.Fatalf("disable provision %+v", ids)
	}
	c3, err := db.GetClient(d, created.ID, admin.ID)
	if err != nil || c3.Enabled || c3.UUID != c1.UUID {
		t.Fatalf("after disable %+v %v", c3, err)
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

func TestBuildSK5AndPortForward(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	entry := &db.Inbound{
		ServerID: srv.ID, Name: "sk5-in", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain",
		ExitURI: "socks5://u:p@203.0.113.9:1080", Settings: "{}",
	}
	if err := Normalize(entry, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, entry); err != nil {
		t.Fatal(err)
	}
	pipe := &db.Inbound{
		ServerID: srv.ID, Name: "pipe", Profile: ProfilePortForward,
		Port: 10443, Enabled: true, Settings: `{"dest_host":"8.8.8.8","dest_port":443,"network":"tcp,udp"}`,
	}
	if err := Normalize(pipe, nil); err != nil {
		t.Fatal(err)
	}
	createdPipe, err := db.CreateInbound(d, pipe)
	if err != nil {
		t.Fatal(err)
	}

	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
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
	if _, err := db.GetClient(d, createdPipe.ID, u.ID); err == nil {
		t.Fatal("port-forward must not get a user client")
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
	foundDoor := false
	for _, raw := range xray["inbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "dokodemo-door" || obj["tag"] == "api" {
			continue
		}
		foundDoor = true
		st, _ := obj["settings"].(map[string]any)
		if st["address"] != "8.8.8.8" || st["port"] != float64(443) || st["network"] != "tcp,udp" {
			t.Fatalf("dokodemo %+v", st)
		}
	}
	if !foundDoor {
		t.Fatal("missing dokodemo-door")
	}
	foundSocks := false
	for _, raw := range xray["outbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "socks" {
			continue
		}
		foundSocks = true
		st, _ := obj["settings"].(map[string]any)
		srvs, _ := st["servers"].([]any)
		if len(srvs) != 1 {
			t.Fatalf("socks servers %v", st["servers"])
		}
		s0, _ := srvs[0].(map[string]any)
		if s0["address"] != "203.0.113.9" || s0["port"] != float64(1080) {
			t.Fatalf("socks server %+v", s0)
		}
	}
	if !foundSocks {
		t.Fatal("missing socks outbound")
	}
}

func TestBuildVLESSExitURI(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	entry := &db.Inbound{
		ServerID: srv.ID, Name: "vless-in", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain",
		ExitURI:  "vless://11111111-1111-4111-8111-111111111111@203.0.113.10:443?encryption=none&security=reality&sni=www.microsoft.com&fp=chrome&pbk=abc&sid=abcd&type=tcp&flow=xtls-rprx-vision#ext",
		Settings: "{}",
	}
	if err := Normalize(entry, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, entry); err != nil {
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
	for _, raw := range xray["outbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "vless" {
			continue
		}
		st, _ := obj["settings"].(map[string]any)
		vnext, _ := st["vnext"].([]any)
		if len(vnext) == 0 {
			continue
		}
		n0, _ := vnext[0].(map[string]any)
		if n0["address"] != "203.0.113.10" || n0["port"] != float64(443) {
			continue
		}
		found = true
		stream, _ := obj["streamSettings"].(map[string]any)
		if stream["security"] != "reality" {
			t.Fatalf("stream %+v", stream)
		}
		rs, _ := stream["realitySettings"].(map[string]any)
		if rs["publicKey"] != "abc" {
			t.Fatalf("reality %+v", rs)
		}
	}
	if !found {
		t.Fatal("missing vless outbound")
	}
}

func TestBuildSOCKS5InboundAndLanding(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	entrySrv, err := db.CreateServer(d, "in", "10.0.0.1", "tok-in")
	if err != nil {
		t.Fatal(err)
	}
	landSrv, err := db.CreateServer(d, "out", "10.0.0.2", "tok-out")
	if err != nil {
		t.Fatal(err)
	}
	land := &db.Inbound{
		ServerID: landSrv.ID, Name: "sk", Profile: ProfileSOCKS5,
		Port: 1080, Enabled: true, LineKind: "direct", Settings: "{}", ServerHost: "10.0.0.2",
	}
	if err := Normalize(land, nil); err != nil {
		t.Fatal(err)
	}
	createdLand, err := db.CreateInbound(d, land)
	if err != nil {
		t.Fatal(err)
	}
	entry := &db.Inbound{
		ServerID: entrySrv.ID, Name: "v-in", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain", Settings: "{}",
	}
	exitID := createdLand.ID
	entry.ExitInboundID = &exitID
	if err := Normalize(entry, createdLand); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, entry); err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{landSrv.ID}); err != nil {
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
	c, err := db.GetClient(d, createdLand.ID, u.ID)
	if err != nil || c.Password == "" || c.Username != db.EmailFor(u.ID, createdLand.ID) {
		t.Fatalf("client %+v %v", c, err)
	}

	landBundle, err := Build(d, landSrv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(landBundle.Apply.Singbox) == 0 || landBundle.Apply.SingboxAPI == "" {
		t.Fatalf("singbox %d api %q", len(landBundle.Apply.Singbox), landBundle.Apply.SingboxAPI)
	}
	var sb map[string]any
	if err := json.Unmarshal(landBundle.Apply.Singbox, &sb); err != nil {
		t.Fatal(err)
	}
	foundUser, foundRelay := false, false
	relayUser := ParseSettings(entry.Settings).String("relay_username")
	for _, raw := range sb["inbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["type"] != "socks" {
			continue
		}
		if obj["listen_port"] != float64(1080) {
			t.Fatalf("port %v", obj["listen_port"])
		}
		for _, uraw := range obj["users"].([]any) {
			uu, _ := uraw.(map[string]any)
			if uu["username"] == c.Email && uu["password"] == c.Password {
				foundUser = true
			}
			if uu["username"] == relayUser {
				foundRelay = true
			}
		}
	}
	if !foundUser || !foundRelay {
		t.Fatalf("socks users user=%v relay=%v cfg=%s", foundUser, foundRelay, landBundle.Apply.Singbox)
	}

	entryBundle, err := Build(d, entrySrv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entryBundle.Apply.Xray) == 0 {
		t.Fatal("xray")
	}
	var xray map[string]any
	if err := json.Unmarshal(entryBundle.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, raw := range xray["outbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "socks" {
			continue
		}
		found = true
		st, _ := obj["settings"].(map[string]any)
		srvs, _ := st["servers"].([]any)
		s0, _ := srvs[0].(map[string]any)
		if s0["address"] != "10.0.0.2" || s0["port"] != float64(1080) {
			t.Fatalf("socks land %+v", s0)
		}
		users, _ := s0["users"].([]any)
		if len(users) != 1 {
			t.Fatalf("users %v", s0["users"])
		}
		u0, _ := users[0].(map[string]any)
		if u0["user"] != relayUser {
			t.Fatalf("relay user %+v want %s", u0, relayUser)
		}
	}
	if !found {
		t.Fatal("missing socks landing outbound")
	}
}

func TestBuildMultiHopDialerProxy(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	entrySrv, err := db.CreateServer(d, "in", "10.0.0.1", "tok-in")
	if err != nil {
		t.Fatal(err)
	}
	hopSrv, err := db.CreateServer(d, "hop", "10.0.0.2", "tok-hop")
	if err != nil {
		t.Fatal(err)
	}
	landSrv, err := db.CreateServer(d, "out", "10.0.0.3", "tok-out")
	if err != nil {
		t.Fatal(err)
	}
	hop := &db.Inbound{
		ServerID: hopSrv.ID, Name: "hop", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}", ServerHost: "10.0.0.2",
	}
	if err := Normalize(hop, nil); err != nil {
		t.Fatal(err)
	}
	createdHop, err := db.CreateInbound(d, hop)
	if err != nil {
		t.Fatal(err)
	}
	land := &db.Inbound{
		ServerID: landSrv.ID, Name: "land", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}", ServerHost: "10.0.0.3",
	}
	if err := Normalize(land, nil); err != nil {
		t.Fatal(err)
	}
	createdLand, err := db.CreateInbound(d, land)
	if err != nil {
		t.Fatal(err)
	}
	st := Settings{"hops": []any{map[string]any{"kind": "panel", "inbound_id": createdHop.ID}}}
	if err := ApplyHopRelays(st, map[int64]*db.Inbound{createdHop.ID: createdHop}, 0, createdLand.ID); err != nil {
		t.Fatal(err)
	}
	raw, err := st.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	entry := &db.Inbound{
		ServerID: entrySrv.ID, Name: "v-in", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "chain", Settings: raw,
	}
	exitID := createdLand.ID
	entry.ExitInboundID = &exitID
	if err := Normalize(entry, createdLand); err != nil {
		t.Fatal(err)
	}
	createdEntry, err := db.CreateInbound(d, entry)
	if err != nil {
		t.Fatal(err)
	}

	hopBundle, err := Build(d, hopSrv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var hopXray map[string]any
	if err := json.Unmarshal(hopBundle.Apply.Xray, &hopXray); err != nil {
		t.Fatal(err)
	}
	hopRelay := ParseHops(ParseSettings(createdEntry.Settings))[0].RelayUUID
	if hopRelay == "" {
		t.Fatal("hop relay")
	}
	foundHopRelay := false
	for _, raw := range hopXray["inbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "vless" {
			continue
		}
		st, _ := obj["settings"].(map[string]any)
		for _, uraw := range st["clients"].([]any) {
			uu, _ := uraw.(map[string]any)
			if uu["id"] == hopRelay {
				foundHopRelay = true
			}
		}
	}
	if !foundHopRelay {
		t.Fatalf("hop inbound missing relay %s", hopRelay)
	}

	landBundle, err := Build(d, landSrv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var landXray map[string]any
	if err := json.Unmarshal(landBundle.Apply.Xray, &landXray); err != nil {
		t.Fatal(err)
	}
	landRelay := ParseSettings(createdEntry.Settings).String("relay_uuid")
	foundLandRelay := false
	for _, raw := range landXray["inbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "vless" {
			continue
		}
		st, _ := obj["settings"].(map[string]any)
		for _, uraw := range st["clients"].([]any) {
			uu, _ := uraw.(map[string]any)
			if uu["id"] == landRelay {
				foundLandRelay = true
			}
		}
	}
	if !foundLandRelay {
		t.Fatalf("land inbound missing relay %s", landRelay)
	}

	entryBundle, err := Build(d, entrySrv.ID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(entryBundle.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	byTag := map[string]map[string]any{}
	for _, raw := range xray["outbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		tag, _ := obj["tag"].(string)
		byTag[tag] = obj
	}
	hopTag := hopOutboundTag(createdEntry.ID, 0)
	landTag := outboundTag(createdEntry.ID)
	hopOb := byTag[hopTag]
	landOb := byTag[landTag]
	if hopOb == nil || landOb == nil {
		t.Fatalf("outbounds %v", keysOf(byTag))
	}
	if hopOb["protocol"] != "vless" || landOb["protocol"] != "vless" {
		t.Fatalf("proto hop=%v land=%v", hopOb["protocol"], landOb["protocol"])
	}
	stream, _ := landOb["streamSettings"].(map[string]any)
	sockopt, _ := stream["sockopt"].(map[string]any)
	if sockopt["dialerProxy"] != hopTag {
		t.Fatalf("dialerProxy %v want %s", sockopt["dialerProxy"], hopTag)
	}
	hopStream, _ := hopOb["streamSettings"].(map[string]any)
	if hopSock, _ := hopStream["sockopt"].(map[string]any); hopSock["dialerProxy"] != nil {
		t.Fatalf("first hop should not proxy: %+v", hopSock)
	}
}

func keysOf(m map[string]map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
