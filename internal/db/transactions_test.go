package db

import "testing"

func TestCreateUserConfiguredRollsBackInvalidPackage(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := CreateUserConfigured(d, "alice", "hash", "plain", "user", "", 0, &Package{ID: 999}, 0); err == nil {
		t.Fatal("expected invalid package error")
	}
	n, err := CountUsers(d)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("partial user remained: %d", n)
	}
}

func TestConfiguredPackageWritesAreAtomic(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	p := &Package{Name: "bad", Direction: "oneway", CycleDays: 30}
	if _, err := CreatePackageConfigured(d, p, PackageRelationsServer, []int64{999}, nil, nil); err == nil {
		t.Fatal("expected invalid server error")
	}
	list, err := ListPackages(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("partial package remained: %d", len(list))
	}

	good, err := CreatePackage(d, "original", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	good.Name = "changed"
	if err := UpdatePackageConfigured(d, good, PackageRelationsServer, []int64{999}, nil, nil); err == nil {
		t.Fatal("expected invalid server error")
	}
	fresh, err := GetPackage(d, good.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Name != "original" {
		t.Fatalf("partial update remained: %q", fresh.Name)
	}
}

func TestPackageResetModeRoundTrip(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	pkg, err := CreatePackage(d, "monthly", 100, 0, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if pkg.ResetMode != "" {
		t.Fatalf("existing default %q", pkg.ResetMode)
	}
	pkg.ResetMode = ResetModeAnniversary
	if err := UpdatePackage(d, pkg); err != nil {
		t.Fatal(err)
	}
	fresh, err := GetPackage(d, pkg.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.ResetMode != ResetModeAnniversary {
		t.Fatalf("mode %q", fresh.ResetMode)
	}
	if _, err := NormalizeResetMode("weekly"); err == nil {
		t.Fatal("expected invalid mode")
	}
	none, err := NormalizeResetMode("none")
	if err != nil || none != "" {
		t.Fatalf("none %q %v", none, err)
	}
}

func TestCreateUserConfiguredWritesResetDay(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	pkg, err := CreatePackage(d, "monthly", 100, 0, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	u, err := CreateUserConfigured(d, "a", "hash", "plain", "user", "", 0, pkg, 12)
	if err != nil {
		t.Fatal(err)
	}
	if u.TrafficResetDay != 12 {
		t.Fatalf("reset day %d", u.TrafficResetDay)
	}
	u2, err := CreateUserConfigured(d, "b", "hash", "plain", "user", "", 0, pkg, 0)
	if err != nil {
		t.Fatal(err)
	}
	if u2.TrafficResetDay != 0 {
		t.Fatalf("unset day %d", u2.TrafficResetDay)
	}
	again, err := GetUser(d, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.TrafficResetDay != 12 {
		t.Fatalf("first user rewritten: %d", again.TrafficResetDay)
	}
}
