package db

import "testing"

func TestCreateUserConfiguredRollsBackInvalidPackage(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := CreateUserConfigured(d, "alice", "hash", "plain", "user", "", 0, &Package{ID: 999}); err == nil {
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
	if _, err := CreatePackageConfigured(d, p, PackageRelationsServer, []int64{999}, nil); err == nil {
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
	if err := UpdatePackageConfigured(d, good, PackageRelationsServer, []int64{999}, nil); err == nil {
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
