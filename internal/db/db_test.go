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

func TestUserAccessExpired(t *testing.T) {
	u := &User{Enabled: true, Role: "user", ExpiresAt: time.Now().Unix() - 10}
	pid := int64(1)
	u.PackageID = &pid
	if UserAccessOK(u, &Package{}) {
		t.Fatal("expired")
	}
}
