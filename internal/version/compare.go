package version

import (
	"strconv"
	"strings"
)

// MinRemoteUpgrade is the first agent that understands upgrade/uninstall RPC.
const MinRemoteUpgrade = "0.1.32"

func parseVer(s string) []int {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ".")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return nil
		}
		out = append(out, n)
	}
	return out
}

// Compare returns -1 if a<b, 0 if equal, 1 if a>b. Unparseable is less than a valid version.
func Compare(a, b string) int {
	pa, pb := parseVer(a), parseVer(b)
	if pa == nil && pb == nil {
		return 0
	}
	if pa == nil {
		return -1
	}
	if pb == nil {
		return 1
	}
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(pa) {
			x = pa[i]
		}
		if i < len(pb) {
			y = pb[i]
		}
		if x < y {
			return -1
		}
		if x > y {
			return 1
		}
	}
	return 0
}

func OlderThan(got, min string) bool {
	return Compare(got, min) < 0
}

// CanRemoteUpgrade is true when the agent already speaks upgrade/uninstall RPC.
func CanRemoteUpgrade(ver string) bool {
	return !OlderThan(strings.TrimSpace(ver), MinRemoteUpgrade)
}
