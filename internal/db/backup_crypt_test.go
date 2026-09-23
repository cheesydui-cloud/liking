package db

import (
	"bytes"
	"testing"
)

func TestEncryptBackupRoundTrip(t *testing.T) {
	plain := []byte(`{"format":"liking-backup","hello":true}`)
	enc, err := EncryptBackup(plain, "pw-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !IsEncryptedBackup(enc) {
		t.Fatal("magic")
	}
	got, err := DecryptBackup(enc, "pw-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("mismatch")
	}
	if _, err := DecryptBackup(enc, "wrong"); err == nil {
		t.Fatal("expected fail")
	}
	legacy, err := encryptBackupN(plain, "pw-secret", scryptNLegacy)
	if err != nil {
		t.Fatal(err)
	}
	got, err = DecryptBackup(legacy, "pw-secret")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatal("legacy mismatch")
	}
}
