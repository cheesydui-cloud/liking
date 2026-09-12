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
