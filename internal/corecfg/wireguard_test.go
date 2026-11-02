package corecfg

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestWireGuardKeyClamp(t *testing.T) {
	priv, pub, err := GenerateWireGuardKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(priv)
	if err != nil || len(raw) != 32 {
		t.Fatalf("priv %s", priv)
	}
	if raw[0]&7 != 0 || raw[31]&128 != 0 || raw[31]&64 == 0 {
		t.Fatalf("unclamped %x", raw)
	}
	again, err := wireGuardPublic(priv)
	if err != nil || again != pub {
		t.Fatalf("pub %s %s %v", again, pub, err)
	}
}

func TestNormalizeWireGuardKeepsKeys(t *testing.T) {
	in := &db.Inbound{Name: "us", Profile: ProfileWireGuard, Port: 51820, LineKind: "direct", Settings: "{}"}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if in.Core != CoreWireGuard || in.Protocol != "wireguard" || in.Network != "udp" || in.RejectCN {
		t.Fatalf("spec %+v", in)
	}
	st := ParseSettings(in.Settings)
	priv, exit := st.String("private_key"), st.String("exit_private_key")
	probe := st.String("probe_private_key")
	if priv == "" || exit == "" || priv == exit || st.String("public_key") == "" || probe == "" || probe == priv || probe == exit {
		t.Fatal("keys")
	}
	if st.String("probe_address") != WGProbeAddress || st.String("probe_public_key") == "" {
		t.Fatal("probe")
	}
	in.Settings = mustSettings(st)
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	st2 := ParseSettings(in.Settings)
	if st2.String("private_key") != priv || st2.String("exit_private_key") != exit || st2.String("probe_private_key") != probe {
		t.Fatal("rotated")
	}
	if st2.String("network") != WGDefaultNetwork || st2.String("address") != WGDefaultAddress || st2.String("exit_address") != WGDefaultExit {
		t.Fatalf("addr %+v", st2)
	}
	chain := &db.Inbound{Name: "c", Profile: ProfileWireGuard, Port: 51820, LineKind: "chain", Settings: in.Settings}
	if err := Normalize(chain, nil); err == nil {
		t.Fatal("chain should fail")
	}
	wide := &db.Inbound{Name: "w", Profile: ProfileWireGuard, Port: 51820, LineKind: "direct", Settings: `{"network":"10.0.0.0/8","address":"10.0.0.1/8","exit_address":"10.0.0.2/32"}`}
	if err := Normalize(wide, nil); err == nil {
		t.Fatal("wide network")
	}
}

func TestWireGuardAddressesAndConf(t *testing.T) {
	in := &db.Inbound{
		Name: "home", Profile: ProfileWireGuard, Port: 51820, LineKind: "direct",
		ServerHost: "cm.example", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	c1, err := NewWireGuardClient(&db.User{ID: 1}, in, nil)
	if err != nil {
		t.Fatal(err)
	}
	if c1.UUID != "10.7.0.10/32" || c1.Password == "" || c1.Username == "" || c1.Email != "u1.i0" {
		t.Fatalf("c1 %+v", c1)
	}
	c2, err := NewWireGuardClient(&db.User{ID: 2}, in, []*db.Client{c1})
	if err != nil {
		t.Fatal(err)
	}
	if c2.UUID != "10.7.0.11/32" || c2.Username == c1.Username {
		t.Fatalf("c2 %+v", c2)
	}
	c1.Enabled = true
	c2.Enabled = true
	hub, peers, err := HubConfig(in, []*db.Client{c2, c1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hub, "Table = off") || !strings.Contains(hub, "FwMark = 51820") {
		t.Fatalf("hub flags\n%s", hub)
	}
	if !strings.Contains(hub, "from 10.7.0.1 lookup main priority 50") || !strings.Contains(hub, "from 10.7.0.2 lookup main priority 50") {
		t.Fatalf("exempt\n%s", hub)
	}
	if !strings.Contains(hub, "from 10.7.0.0/24 lookup 100 priority 100") || !strings.Contains(hub, "ip route replace default dev %i table 100") {
		t.Fatalf("policy\n%s", hub)
	}
	if !strings.Contains(hub, "net.ipv4.conf.%i.rp_filter=2") {
		t.Fatalf("rp_filter\n%s", hub)
	}
	if !strings.Contains(hub, "AllowedIPs = 0.0.0.0/0") || !strings.Contains(hub, "AllowedIPs = 10.7.0.10/32") || !strings.Contains(hub, "AllowedIPs = 10.7.0.11/32") {
		t.Fatalf("peers\n%s", hub)
	}
	if !strings.Contains(hub, "TCPMSS --set-mss 1240") || !strings.Contains(hub, "AllowedIPs = "+WGProbeAddress) {
		t.Fatalf("mss or probe\n%s", hub)
	}
	probePub := ParseSettings(in.Settings).String("probe_public_key")
	for _, p := range peers {
		if p.PublicKey == probePub || p.AllowedIPs == WGProbeAddress {
			t.Fatal("probe peer is billed")
		}
	}
	if len(peers) != 3 || !peers[0].Exit {
		t.Fatalf("peer order %+v", peers)
	}
	home, err := HomeConfig(in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(home, "Address = 10.7.0.2/32") || !strings.Contains(home, "Table = off") {
		t.Fatalf("home\n%s", home)
	}
	if !strings.Contains(home, "AllowedIPs = 10.7.0.0/24") || !strings.Contains(home, "PersistentKeepalive = 25") {
		t.Fatalf("home peer\n%s", home)
	}
	if strings.Contains(home, "AllowedIPs = 0.0.0.0/0") {
		t.Fatalf("home took the default route\n%s", home)
	}
	if !strings.Contains(home, "MASQUERADE") || !strings.Contains(home, "ip route replace 10.7.0.0/24 dev %i") || !strings.Contains(home, "PostDown") {
		t.Fatalf("home nat\n%s", home)
	}
	if !strings.Contains(home, "iptables -I FORWARD 1 -i %i -j ACCEPT") || !strings.Contains(home, "TCPMSS --set-mss 1240") {
		t.Fatalf("home forward\n%s", home)
	}
	if !strings.Contains(home, "Endpoint = cm.example:51820") {
		t.Fatalf("endpoint\n%s", home)
	}
	st := ParseSettings(in.Settings)
	if strings.Contains(home, st.String("private_key")) || strings.Contains(hub, st.String("exit_private_key")) {
		t.Fatal("private key leaked to the other side")
	}
	uri, err := ShareURI(in, c1)
	if err != nil || !strings.HasPrefix(uri, "wireguard://") || strings.Contains(uri, st.String("exit_private_key")) {
		t.Fatalf("uri %s %v", uri, err)
	}
	if !strings.Contains(uri, "address=10.7.0.10") || !strings.Contains(uri, "mtu=1280") {
		t.Fatalf("uri %s", uri)
	}
	_, yaml, err := ClashProxyYAML(in, c1)
	if err != nil || !strings.Contains(yaml, "type: wireguard") || !strings.Contains(yaml, "ip: \"10.7.0.10\"") || !strings.Contains(yaml, "0.0.0.0/0") || strings.Contains(yaml, "::/0") || !strings.Contains(yaml, "remote-dns-resolve: true") {
		t.Fatalf("clash %v\n%s", err, yaml)
	}
	ob, err := SingboxOutbound(in, c1)
	if err != nil {
		t.Fatal(err)
	}
	ep := SingboxEndpoint(ob)
	if ep == nil || ep["type"] != "wireguard" || ep["tag"] != "home" {
		t.Fatalf("endpoint %+v", ep)
	}
	conf, err := WireGuardClientConf(in, c1)
	if err != nil || !strings.Contains(conf, "AllowedIPs = 0.0.0.0/0\n") || strings.Contains(conf, "::/0") || strings.Contains(conf, "Table = off") {
		t.Fatalf("client\n%s %v", conf, err)
	}
	epPeers, _ := ep["peers"].([]any)
	if len(epPeers) != 1 {
		t.Fatalf("endpoint peers %+v", ep["peers"])
	}
	one, _ := epPeers[0].(map[string]any)
	ips, _ := one["allowed_ips"].([]string)
	if len(ips) != 1 || ips[0] != "0.0.0.0/0" {
		t.Fatalf("singbox allowed %v", one["allowed_ips"])
	}
}

func TestBuildWireGuardRev(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "cm", "203.0.113.8", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "wg", Profile: ProfileWireGuard, Port: 51820,
		Enabled: true, LineKind: "direct", Settings: "{}", ServerHost: "203.0.113.8",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	b1, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b1.Apply.WireGuard), "Table = off") || strings.Contains(string(b1.Apply.WireGuard), `"email"`) {
		t.Fatalf("empty peers %s", b1.Apply.WireGuard)
	}
	u, err := db.CreateUser(d, "alice", "hash", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := NewWireGuardClient(u, created, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.InboundID = created.ID
	c.Enabled = true
	if err := db.UpsertClient(d, c); err != nil {
		t.Fatal(err)
	}
	b2, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if b1.Rev == b2.Rev || !strings.Contains(string(b2.Apply.WireGuard), c.Email) || !strings.Contains(string(b2.Apply.WireGuard), "10.7.0.10/32") {
		t.Fatalf("rev %s %s\n%s", b1.Rev, b2.Rev, b2.Apply.WireGuard)
	}
	if len(b2.Apply.Xray) > 0 {
		t.Fatalf("xray should stay empty %s", b2.Apply.Xray)
	}
	second := &db.Inbound{
		ServerID: srv.ID, Name: "wg2", Profile: ProfileWireGuard, Port: 51821,
		Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(second, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, second); err != nil {
		t.Fatal(err)
	}
	if _, err := Build(d, srv.ID); err == nil {
		t.Fatal("two wireguard inbounds")
	}
}

func TestBuildWireGuardHomeExit(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	transit, err := db.CreateServer(d, "cm", "203.0.113.8", "tok1")
	if err != nil {
		t.Fatal(err)
	}
	home, err := db.CreateServer(d, "home", "198.51.100.9", "tok2")
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateServer(d, "other", "192.0.2.8", "tok3")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: transit.ID, Name: "wg", Profile: ProfileWireGuard, Port: 51820,
		Enabled: true, LineKind: "direct",
		Settings: fmt.Sprintf(`{"exit_server_id":%d}`, home.ID),
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if WGExitServerID(in) != home.ID {
		t.Fatalf("exit server %d", WGExitServerID(in))
	}
	st := ParseSettings(in.Settings)
	exitPriv := st.String("exit_private_key")
	if exitPriv == "" {
		t.Fatal("missing exit key")
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	hub, err := Build(d, transit.ID)
	if err != nil {
		t.Fatal(err)
	}
	hubConf := string(hub.Apply.WireGuard)
	if !strings.Contains(hubConf, "ListenPort = 51820") || strings.Contains(hubConf, exitPriv) {
		t.Fatalf("hub\n%s", hubConf)
	}
	if !strings.Contains(hubConf, "TCPMSS --set-mss 1240") || !strings.Contains(hubConf, WGProbeAddress) {
		t.Fatalf("hub mss\n%s", hubConf)
	}
	pkg, err := db.CreatePackage(d, "ce", 0, 30, 2, "twoway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{transit.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "ces-user", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, err = db.GetUser(d, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := ProvisionUser(d, u)
	if err != nil {
		t.Fatal(err)
	}
	seenSrv := map[int64]bool{}
	for _, id := range ids {
		seenSrv[id] = true
	}
	if !seenSrv[transit.ID] || !seenSrv[home.ID] {
		t.Fatalf("sync targets %v", ids)
	}
	homeB, err := Build(d, home.ID)
	if err != nil {
		t.Fatal(err)
	}
	homeConf := string(homeB.Apply.WireGuard)
	if !strings.Contains(homeConf, exitPriv) || !strings.Contains(homeConf, "Endpoint = 203.0.113.8:51820") || !strings.Contains(homeConf, "AllowedIPs = 10.7.0.0/24") || strings.Contains(homeConf, "0.0.0.0/0") || strings.Contains(homeConf, `"email"`) {
		t.Fatalf("home\n%s", homeConf)
	}
	idle, err := Build(d, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(idle.Apply.WireGuard) != 0 {
		t.Fatalf("unrelated %s", idle.Apply.WireGuard)
	}
	local := &db.Inbound{
		ServerID: home.ID, Name: "bad", Profile: ProfileWireGuard, Port: 51821,
		Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(local, nil); err != nil {
		t.Fatal(err)
	}
	all, err := db.ListInbounds(d)
	if err != nil {
		t.Fatal(err)
	}
	if err := CheckWireGuardExit(local, append(all, local)); err == nil {
		t.Fatal("home machine accepted its own wireguard node")
	}
	created.Enabled = false
	if err := db.UpdateInbound(d, created); err != nil {
		t.Fatal(err)
	}
	down, err := Build(d, home.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(down.Apply.WireGuard) != 0 {
		t.Fatalf("disabled exit should leave home empty %s", down.Apply.WireGuard)
	}
}

func mustSettings(st Settings) string {
	b, err := json.Marshal(st)
	if err != nil {
		panic(err)
	}
	return string(b)
}
