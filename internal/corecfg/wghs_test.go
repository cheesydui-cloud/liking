package corecfg

import (
	"encoding/binary"
	"testing"
	"time"
)

func TestWireGuardInitiationRoundTrip(t *testing.T) {
	serverPriv, serverPub, err := GenerateWireGuardKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	peerPriv, peerPub, err := GenerateWireGuardKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	msg, err := WireGuardInitiation(peerPriv, serverPub)
	if err != nil {
		t.Fatal(err)
	}
	if len(msg) != 148 || binary.LittleEndian.Uint32(msg[:4]) != 1 {
		t.Fatalf("msg %d %d", len(msg), binary.LittleEndian.Uint32(msg[:4]))
	}
	if !wgMAC1OK(serverPub, msg) {
		t.Fatal("mac1")
	}
	var mac2 byte
	for _, b := range msg[132:] {
		mac2 |= b
	}
	if mac2 != 0 {
		t.Fatal("mac2 set")
	}
	got, err := wgInitiationPeer(serverPriv, msg)
	if err != nil || got != peerPub {
		t.Fatalf("peer %s %v", got, err)
	}
	bad := append([]byte(nil), msg...)
	bad[50] ^= 0xff
	if _, err := wgInitiationPeer(serverPriv, bad); err == nil {
		t.Fatal("tampered initiation opened")
	}
	reply := make([]byte, 92)
	binary.LittleEndian.PutUint32(reply, 2)
	if !WireGuardReplyOK(reply) {
		t.Fatal("response")
	}
	cookie := make([]byte, 64)
	binary.LittleEndian.PutUint32(cookie, 3)
	if !WireGuardReplyOK(cookie) {
		t.Fatal("cookie")
	}
	if WireGuardReplyOK([]byte{4, 0, 0, 0}) {
		t.Fatal("transport is not a listen reply")
	}
	ts := wgTimestamp(time.Unix(1_700_000_000, 0))
	if len(ts) != 12 || binary.BigEndian.Uint64(ts[:8]) != wgTAI64Base+1_700_000_000 {
		t.Fatalf("tai64 %x", ts)
	}
}
