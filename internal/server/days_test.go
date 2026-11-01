package server

import "testing"

func TestResolveCreateExpiryRejectsMixed(t *testing.T) {
	if _, err := resolveCreateExpiry(nil, 0, 30, 3); err == nil {
		t.Fatal("months and days")
	}
	if _, err := resolveCreateExpiry(nil, 10, 0, 3); err == nil {
		t.Fatal("months and expires_at")
	}
	if _, err := resolveCreateExpiry(nil, 0, 0, 1201); err == nil {
		t.Fatal("months too large")
	}
	got, err := resolveCreateExpiry(nil, 0, 0, 0)
	if err != nil || got != 0 {
		t.Fatalf("empty %d %v", got, err)
	}
	if anniversaryResetDay(nil, nil) != 0 {
		t.Fatal("nil package")
	}
}
