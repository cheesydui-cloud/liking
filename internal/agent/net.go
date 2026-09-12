package agent

import (
	"bufio"
	"bytes"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	netMu   sync.Mutex
	lastRx  int64
	lastTx  int64
	lastNet time.Time
)

func snapshotNet() (upBps, downBps int64, ok bool) {
	rx, tx, err := readNetDev()
	if err != nil {
		return 0, 0, false
	}
	now := time.Now()
	netMu.Lock()
	defer netMu.Unlock()
	if !lastNet.IsZero() {
		dt := now.Sub(lastNet).Seconds()
		if dt > 0.2 {
			if tx >= lastTx {
				upBps = int64(float64(tx-lastTx) / dt)
			}
			if rx >= lastRx {
				downBps = int64(float64(rx-lastRx) / dt)
			}
		}
	}
	lastRx, lastTx, lastNet = rx, tx, now
	return upBps, downBps, true
}

func readNetDev() (rx, tx int64, err error) {
	b, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	return parseNetDev(b)
}

func parseNetDev(b []byte) (rx, tx int64, err error) {
	sc := bufio.NewScanner(bytes.NewReader(b))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		name := strings.TrimSpace(line[:i])
		if skipIface(name) {
			continue
		}
		fields := strings.Fields(line[i+1:])
		if len(fields) < 9 {
			continue
		}
		r, e1 := strconv.ParseInt(fields[0], 10, 64)
		t, e2 := strconv.ParseInt(fields[8], 10, 64)
		if e1 != nil || e2 != nil {
			continue
		}
		rx += r
		tx += t
	}
	return rx, tx, sc.Err()
}

func skipIface(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if n == "" || n == "lo" {
		return true
	}
	for _, p := range []string{
		"lo:", "dummy", "sit", "ip6tnl", "ip6gre", "gre", "tun", "tap", "veth",
		"br-", "docker", "virbr", "wg", "fwbr", "fwpr", "fwln", "kube",
		"flannel", "cni", "vnet", "lxc", "nerdctl", "podman",
	} {
		if n == strings.TrimSuffix(p, ":") || strings.HasPrefix(n, p) {
			return true
		}
	}
	return false
}
