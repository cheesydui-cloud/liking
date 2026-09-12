package corecfg

import "testing"

func TestPickFreePortAvoidsUsed(t *testing.T) {
	used := map[int]struct{}{10000: {}, 10001: {}, 54321: {}}
	for i := 0; i < 32; i++ {
		p, err := PickFreePort(used)
		if err != nil {
			t.Fatal(err)
		}
		if p < 10000 || p > 59999 {
			t.Fatalf("range %d", p)
		}
		if _, taken := used[p]; taken {
			t.Fatalf("used %d", p)
		}
		used[p] = struct{}{}
	}
}

func TestPickFreePortExhausted(t *testing.T) {
	used := make(map[int]struct{}, freePortMax-freePortMin+1)
	for p := freePortMin; p <= freePortMax; p++ {
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
