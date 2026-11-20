package db

import "testing"

func TestPackageLeakProtectRoundTrip(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	saved, err := CreatePackageConfigured(d, &Package{Name: "lp", Direction: "oneway", LeakProtect: true}, PackageRelationsServer, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.LeakProtect {
		t.Fatal("create did not save leak_protect")
	}
	plain, err := CreatePackage(d, "plain", 0, 0, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if plain.LeakProtect {
		t.Fatal("default should be off")
	}
	saved.LeakProtect = false
	if err := UpdatePackage(d, saved); err != nil {
		t.Fatal(err)
	}
	got, err := GetPackage(d, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LeakProtect {
		t.Fatal("update did not clear leak_protect")
	}
	u := &User{SpeedLimit: 1}
	CopyPackagePolicy(u, &Package{SpeedLimit: 9, LeakProtect: true})
	if u.SpeedLimit != 9 {
		t.Fatalf("policy copy %d", u.SpeedLimit)
	}
}
