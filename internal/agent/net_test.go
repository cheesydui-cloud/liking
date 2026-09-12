package agent

import "testing"

func TestParseNetDevSkipsVirtual(t *testing.T) {
	raw := []byte(`Inter-|   Receive                                                |  Transmit
 face |bytes    packets errs drop fifo frame compressed multicast|bytes    packets errs drop fifo colls carrier compressed
    lo: 9999 1 0 0 0 0 0 0 9999 1 0 0 0 0 0 0
  eth0: 1000 2 0 0 0 0 0 0 2500 3 0 0 0 0 0 0
  ens3: 400 0 0 0 0 0 0 0 500 0 0 0 0 0 0 0
  veth0: 888 0 0 0 0 0 0 0 888 0 0 0 0 0 0 0
docker0: 777 0 0 0 0 0 0 0 777 0 0 0 0 0 0 0
`)
	rx, tx, err := parseNetDev(raw)
	if err != nil {
		t.Fatal(err)
	}
	if rx != 1400 || tx != 3000 {
		t.Fatalf("rx=%d tx=%d", rx, tx)
	}
}

func TestSkipIface(t *testing.T) {
	if !skipIface("lo") || !skipIface("veth123") || !skipIface("docker0") {
		t.Fatal("expected skip")
	}
	if skipIface("eth0") || skipIface("ens18") || skipIface("enp1s0") || skipIface("venet0") {
		t.Fatal("should count physical")
	}
}
