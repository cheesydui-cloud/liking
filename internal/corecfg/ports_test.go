package corecfg

import (
	"testing"

	"liking/internal/db"
)

func TestPickFreePortAvoidsUsed(t *testing.T) {
	used := map[int]struct{}{10000: {}, 10001: {}, 54321: {}}
	for i := 0; i < 32; i++ {
		p, err := PickFreePort(used)
		if err != nil {
			t.Fatal(err)
		}
		if p < DefaultPortMin || p > DefaultPortMax {
			t.Fatalf("range %d", p)
		}
		if _, taken := used[p]; taken {
			t.Fatalf("used %d", p)
		}
		used[p] = struct{}{}
	}
}

func TestPickFreePortExhausted(t *testing.T) {
	used := make(map[int]struct{}, DefaultPortMax-DefaultPortMin+1)
	for p := DefaultPortMin; p <= DefaultPortMax; p++ {
		used[p] = struct{}{}
	}
	if _, err := PickFreePort(used); err == nil {
		t.Fatal("expected error")
	}
	delete(used, 23456)
	p, err := PickFreePort(used)
	if err != nil || p != 23456 {
		t.Fatalf("got %d %v", p, err)
	}
}

func TestPickFreePortRange(t *testing.T) {
	used := map[int]struct{}{40001: {}}
	for i := 0; i < 5; i++ {
		p, err := PickFreePortRange(used, 40000, 40005)
		if err != nil {
			t.Fatal(err)
		}
		if p < 40000 || p > 40005 || p == 40001 {
			t.Fatalf("got %d", p)
		}
		used[p] = struct{}{}
	}
	if _, err := PickFreePortRange(used, 40000, 40005); err == nil {
		t.Fatal("expected exhausted")
	}
}

func TestNormalizePortRange(t *testing.T) {
	min, max, err := NormalizePortRange(0, 0)
	if err != nil || min != DefaultPortMin || max != DefaultPortMax {
		t.Fatalf("default %d %d %v", min, max, err)
	}
	min, max, err = NormalizePortRange(20000, 30000)
	if err != nil || min != 20000 || max != 30000 {
		t.Fatalf("custom %d %d %v", min, max, err)
	}
	if _, _, err := NormalizePortRange(30000, 20000); err == nil {
		t.Fatal("min>max")
	}
	if _, _, err := NormalizePortRange(-1, 80); err == nil {
		t.Fatal("negative")
	}
	if _, _, err := NormalizePortRange(1, 70000); err == nil {
		t.Fatal("too high")
	}
}

func TestSocksPortOutOfUserPool(t *testing.T) {
	if SocksPort(1) != SocksPortBase+1 {
		t.Fatalf("socks %d", SocksPort(1))
	}
	if SocksPort(1) <= DefaultPortMax {
		t.Fatal("socks in user pool")
	}
	used := map[int]struct{}{}
	MarkReservedPorts(used, []*db.Inbound{{ID: 1}, {ID: 2}})
	if _, ok := used[XrayAPIPort]; !ok {
		t.Fatal("xray api")
	}
	if _, ok := used[SingboxAPIPort]; !ok {
		t.Fatal("singbox api")
	}
	if _, ok := used[SocksPort(1)]; !ok {
		t.Fatal("socks 1")
	}
	if _, ok := used[SocksPort(2)]; !ok {
		t.Fatal("socks 2")
	}
}
