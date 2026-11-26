package db

import "testing"

func TestPackageRemarkRoundTrip(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()

	saved, err := CreatePackageConfigured(d, &Package{Name: "noted", Direction: "oneway", Remark: "月付家宽"}, PackageRelationsServer, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Remark != "月付家宽" {
		t.Fatalf("create remark %q", saved.Remark)
	}
	plain, err := CreatePackage(d, "plain", 0, 0, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if plain.Remark != "" {
		t.Fatalf("default remark %q", plain.Remark)
	}
	saved.Name = "noted-2"
	saved.Remark = "只给管理员看"
	if err := UpdatePackage(d, saved); err != nil {
		t.Fatal(err)
	}
	got, err := GetPackage(d, saved.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Remark != "只给管理员看" || got.Name != "noted-2" {
		t.Fatalf("update %+v", got)
	}
	u := &User{Remark: "用户自己的"}
	CopyPackagePolicy(u, &Package{Remark: "套餐备注", SpeedLimit: 9})
	if u.Remark != "用户自己的" || u.SpeedLimit != 9 {
		t.Fatalf("policy copied remark %q speed %d", u.Remark, u.SpeedLimit)
	}
}
