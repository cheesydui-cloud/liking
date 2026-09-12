package db

import (
	"testing"
	"time"
)

func TestOpenMigrateAndCRUD(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	u, err := CreateUser(d, "admin", "hash", "admin", "")
	if err != nil {
		t.Fatal(err)
	}
	if u.SubToken == "" {
		t.Fatal("sub token")
	}
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s, err := CreateServer(d, "n1", "1.2.3.4", tok)
	if err != nil {
		t.Fatal(err)
	}
	if s.LastError != "" || s.LastErrorAt != 0 {
		t.Fatalf("last_error %+v", s)
	}
	if s.Cores != "" {
		t.Fatalf("cores %q", s.Cores)
	}
	if !ServerHasCore(s, "mita") {
		t.Fatal("empty cores should allow all")
	}
	if err := MarkServerOnline(d, s.ID, "0.1.6", "linux", "amd64", "1.2.3.4", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	s, _ = GetServer(d, s.ID)
	if s.Cores != "xray" || !ServerHasCore(s, "xray") || ServerHasCore(s, "mita") {
		t.Fatalf("cores %+v", s)
	}
	if err := SetServerCores(d, s.ID, []string{"xray", "mita"}); err != nil {
		t.Fatal(err)
	}
	s, _ = GetServer(d, s.ID)
	if !ServerHasCore(s, "mita") {
		t.Fatalf("set cores %+v", s)
	}
	if err := SetServerApplyError(d, s.ID, simpleErr("xray 未安装")); err != nil {
		t.Fatal(err)
	}
	s, _ = GetServer(d, s.ID)
	if s.LastError != "xray 未安装" || s.LastErrorAt == 0 {
		t.Fatalf("last_error set %+v", s)
	}
	if err := SetServerApplyError(d, s.ID, nil); err != nil {
		t.Fatal(err)
	}
	s, _ = GetServer(d, s.ID)
	if s.LastError != "" {
		t.Fatalf("last_error clear %q", s.LastError)
	}
	in := &Inbound{
		ServerID: s.ID, Name: "a", Profile: "vless-reality", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	}
	got, err := CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	if got.ServerHost != "1.2.3.4" {
		t.Fatalf("host %s", got.ServerHost)
	}
	n, err := DisableInboundsOnPorts(d, s.ID, []int{443})
	if err != nil || n != 1 {
		t.Fatalf("disable %d %v", n, err)
	}
	got, _ = GetInbound(d, got.ID)
	if got.Enabled {
		t.Fatal("expected disabled")
	}
	if _, err := d.Exec(`UPDATE inbounds SET enabled=1 WHERE id=?`, got.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = GetInbound(d, got.ID)

	p, err := CreatePackage(d, "std", 100, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPackageInbounds(d, p.ID, []int64{got.ID}, []float64{1.5}); err != nil {
		t.Fatal(err)
	}
	p, _ = GetPackage(d, p.ID)
	if PackageMultiplier(p, got.ID) != 1.5 {
		t.Fatalf("mult %v", p.Multipliers)
	}

	user, err := CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().Add(24 * time.Hour).Unix()
	if err := BindUserPackage(d, user.ID, p.ID, exp); err != nil {
		t.Fatal(err)
	}
	user, _ = GetUser(d, user.ID)
	if !UserAccessOK(user, p) {
		t.Fatal("expected access")
	}
	lim := int64(10)
	user.TrafficLimit = &lim
	user.UsedUp = 6
	user.UsedDown = 5
	if UserAccessOK(user, p) {
		t.Fatal("over quota")
	}

	cert, err := CreateCert(d, &Certificate{
		Name: "c1", CertPEM: "-----BEGIN CERTIFICATE-----\nA\n-----END CERTIFICATE-----",
		KeyPEM:  "-----BEGIN PRIVATE KEY-----\nB\n-----END PRIVATE KEY-----",
		Domains: "a.example", Source: "acme-cf", ExpiresAt: now() + 10, AutoRenew: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cert.Source != "acme-cf" || !cert.AutoRenew {
		t.Fatalf("%+v", cert)
	}
	due, err := ListCertIDsDueRenew(d, now()+100)
	if err != nil || len(due) != 1 || due[0] != cert.ID {
		t.Fatalf("due %v %v", due, err)
	}
	list, err := ListCerts(d)
	if err != nil || len(list) != 1 || list[0].KeyPEM != "" || list[0].ExpiresAt == 0 {
		t.Fatalf("list %+v %v", list, err)
	}
}

func TestPackageServersSelectInbounds(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	tok2, _ := RandomHex(8)
	s2, err := CreateServer(d, "n2", "2.2.2.2", tok2)
	if err != nil {
		t.Fatal(err)
	}
	mkIn := func(sid int64, name string, port int) *Inbound {
		t.Helper()
		in, err := CreateInbound(d, &Inbound{
			ServerID: sid, Name: name, Profile: "vless-reality", Protocol: "vless",
			Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
			Port: port, Enabled: true, Settings: "{}", LineKind: "direct",
		})
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	a := mkIn(s1.ID, "a", 443)
	b := mkIn(s1.ID, "b", 8443)
	c := mkIn(s2.ID, "c", 443)

	p, err := CreatePackage(d, "nodes", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPackageServers(d, p.ID, []int64{s1.ID}); err != nil {
		t.Fatal(err)
	}
	p, err = GetPackage(d, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ServerIDs) != 1 || p.ServerIDs[0] != s1.ID {
		t.Fatalf("server_ids %+v", p.ServerIDs)
	}
	ids, err := PackageInboundIDs(d, p)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != a.ID || ids[1] != b.ID {
		t.Fatalf("ids %+v want %d,%d not %d", ids, a.ID, b.ID, c.ID)
	}

	user, err := CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := BindUserPackage(d, user.ID, p.ID, time.Now().Add(24*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	user, _ = GetUser(d, user.ID)
	got, err := InboundIDsForUser(d, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != a.ID || got[1] != b.ID {
		t.Fatalf("user ids %+v", got)
	}
}

func TestCreatePackageZeroCycle(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p, err := CreatePackage(d, "open", 0, 0, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if p.CycleDays != 0 || p.TrafficBytes != 0 {
		t.Fatalf("%+v", p)
	}
}

func TestPackageExplicitInbounds(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, _ := RandomHex(8)
	s1, err := CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	mkIn := func(name string, port int) *Inbound {
		t.Helper()
		in, err := CreateInbound(d, &Inbound{
			ServerID: s1.ID, Name: name, Profile: "vless-reality", Protocol: "vless",
			Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
			Port: port, Enabled: true, Settings: "{}", LineKind: "direct",
		})
		if err != nil {
			t.Fatal(err)
		}
		return in
	}
	a := mkIn("a", 443)
	b := mkIn("b", 8443)
	p, err := CreatePackage(d, "pick", 0, 0, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPackageInbounds(d, p.ID, []int64{a.ID}, nil); err != nil {
		t.Fatal(err)
	}
	p, err = GetPackage(d, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	ids, err := PackageInboundIDs(d, p)
	if err != nil || len(ids) != 1 || ids[0] != a.ID {
		t.Fatalf("ids %+v want %d not %d %v", ids, a.ID, b.ID, err)
	}
}

func TestListUsersDoesNotDeadlock(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := CreateUser(d, "admin", "h", "admin", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := CreateUser(d, "alice", "h", "user", ""); err != nil {
		t.Fatal(err)
	}
	list, err := ListUsers(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d", len(list))
	}
}

type simpleErr string

func (e simpleErr) Error() string { return string(e) }

func TestUserAccessExpired(t *testing.T) {
	u := &User{Enabled: true, Role: "user", ExpiresAt: time.Now().Unix() - 10}
	pid := int64(1)
	u.PackageID = &pid
	if UserAccessOK(u, &Package{}) {
		t.Fatal("expired")
	}
}

func TestMarkServerOnlineFillsEmptyPublicHost(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s, err := CreateServer(d, "jp", "", tok)
	if err != nil {
		t.Fatal(err)
	}
	if s.PublicHost != "" {
		t.Fatalf("public_host %q", s.PublicHost)
	}
	if err := MarkServerOnline(d, s.ID, "0.1.9", "linux", "amd64", "177.5.54.5", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	s, err = GetServer(d, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.PublicHost != "177.5.54.5" || s.ConnectIP != "177.5.54.5" {
		t.Fatalf("fill %+v", s)
	}

	in, err := CreateInbound(d, &Inbound{
		ServerID: s.ID, Name: "ss", Profile: "ss2022", Protocol: "shadowsocks",
		Network: "tcp", Security: "none", Core: "xray", Listen: "0.0.0.0",
		Port: 8789, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	if in.ServerHost != "177.5.54.5" || in.ConnectIP != "177.5.54.5" {
		t.Fatalf("inbound host %q ip %q", in.ServerHost, in.ConnectIP)
	}

	if err := MarkServerOnline(d, s.ID, "0.1.9", "linux", "amd64", "9.9.9.9", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	s, _ = GetServer(d, s.ID)
	if s.PublicHost != "177.5.54.5" {
		t.Fatalf("should keep first fill %q", s.PublicHost)
	}
	if s.ConnectIP != "9.9.9.9" {
		t.Fatalf("connect_ip %q", s.ConnectIP)
	}

	tok2, _ := RandomHex(8)
	loop, err := CreateServer(d, "loop", "", tok2)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkServerOnline(d, loop.ID, "0.1.9", "linux", "amd64", "127.0.0.1", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	loop, _ = GetServer(d, loop.ID)
	if loop.PublicHost != "" {
		t.Fatalf("loopback should not fill %q", loop.PublicHost)
	}

	tok3, _ := RandomHex(8)
	named, err := CreateServer(d, "named", "jp.example", tok3)
	if err != nil {
		t.Fatal(err)
	}
	if err := MarkServerOnline(d, named.ID, "0.1.9", "linux", "amd64", "1.2.3.4", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	named, _ = GetServer(d, named.ID)
	if named.PublicHost != "jp.example" {
		t.Fatalf("named host overwritten %q", named.PublicHost)
	}
}

func TestServerTrafficTotals(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s, err := CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := CreateInbound(d, &Inbound{
		ServerID: s.ID, Name: "a", Profile: "ss2022", Protocol: "shadowsocks",
		Network: "tcp", Security: "none", Core: "xray", Listen: "0.0.0.0",
		Port: 8789, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := AddDailyTraffic(d, "2026-09-13", u.ID, in.ID, 100, 250); err != nil {
		t.Fatal(err)
	}
	if err := AddDailyTraffic(d, "2026-09-13", u.ID, in.ID, 50, 50); err != nil {
		t.Fatal(err)
	}
	totals, err := ServerTrafficTotals(d)
	if err != nil {
		t.Fatal(err)
	}
	got := totals[s.ID]
	if got.Up != 150 || got.Down != 300 {
		t.Fatalf("%+v", got)
	}
	s.TrafficLimit = 1024
	if err := UpdateServer(d, s); err != nil {
		t.Fatal(err)
	}
	s, _ = GetServer(d, s.ID)
	if s.TrafficLimit != 1024 {
		t.Fatalf("limit %d", s.TrafficLimit)
	}

	inTot, err := InboundTrafficTotals(d)
	if err != nil {
		t.Fatal(err)
	}
	if inTot[in.ID].Up != 150 || inTot[in.ID].Down != 300 {
		t.Fatalf("inbound %+v", inTot[in.ID])
	}
	series, err := TrafficSeries(d, "2026-09-13", "2026-09-13", 0, 0)
	if err != nil || len(series) != 1 || series[0].Up != 150 {
		t.Fatalf("series %+v %v", series, err)
	}
	filled := FillTrafficDays("2026-09-12", "2026-09-13", series)
	if len(filled) != 2 || filled[0].Day != "2026-09-12" || filled[1].Up != 150 {
		t.Fatalf("fill %+v", filled)
	}
	byUser, err := TrafficByUser(d, "2026-09-13", "2026-09-13")
	if err != nil || len(byUser) != 1 || byUser[0].Name != "alice" {
		t.Fatalf("by user %+v %v", byUser, err)
	}
}

func TestClientByIdentityAndBilling(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s, err := CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	in, err := CreateInbound(d, &Inbound{
		ServerID: s.ID, Name: "m", Profile: "mieru", Protocol: "mieru",
		Network: "tcp", Security: "none", Core: "mita", Listen: "0.0.0.0",
		Port: 8964, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := CreatePackage(d, "two", 1024, 0, 0, "twoway")
	if err != nil {
		t.Fatal(err)
	}
	if err := BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	if err := UpsertClient(d, &Client{
		InboundID: in.ID, UserID: u.ID, Email: EmailFor(u.ID, in.ID),
		Username: "u1", Password: "p", Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	c, err := ClientByIdentity(d, "u1")
	if err != nil || c.Email != EmailFor(u.ID, in.ID) {
		t.Fatalf("username %+v %v", c, err)
	}
	c2, err := ClientByIdentity(d, EmailFor(u.ID, in.ID))
	if err != nil || c2.ID != c.ID {
		t.Fatalf("email %+v %v", c2, err)
	}
	if err := AddUserTraffic(d, u.ID, 10, 15); err != nil {
		t.Fatal(err)
	}
	u, _ = GetUser(d, u.ID)
	if u.BilledBytes != 50 || u.Direction != "twoway" || u.TrafficCap != 1024 {
		t.Fatalf("billing %+v", u)
	}
}
