package totp

import (
	"testing"
	"time"
)

func TestRoundTrip(t *testing.T) {
	sec, err := RandomSecret()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	code, err := Code(sec, now)
	if err != nil {
		t.Fatal(err)
	}
	if !Verify(sec, code) {
		t.Fatalf("verify failed for %s", code)
	}
	if Verify(sec, "000000") && code == "000000" {
		// extremely unlikely; still ok
	} else if Verify(sec, "000000") && code != "000000" {
		t.Fatal("accepted wrong code")
	}
	if URI(sec, "liking", "admin") == "" {
		t.Fatal("empty uri")
	}
}
