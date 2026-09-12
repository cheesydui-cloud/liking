package version

import "testing"

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.9", "0.1.32", -1},
		{"0.1.32", "0.1.32", 0},
		{"v0.1.32", "0.1.32", 0},
		{"0.1.33", "0.1.32", 1},
		{"0.2.0", "0.1.99", 1},
		{"", "0.1.32", -1},
		{"0.1.32-dirty", "0.1.32", 0},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Fatalf("Compare(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
	if CanRemoteUpgrade("0.1.9") {
		t.Fatal("0.1.9 should not remote-upgrade")
	}
	if CanRemoteUpgrade("") {
		t.Fatal("empty should not remote-upgrade")
	}
	if !CanRemoteUpgrade("0.1.32") || !CanRemoteUpgrade("0.1.33") {
		t.Fatal("0.1.32+ should remote-upgrade")
	}
}
