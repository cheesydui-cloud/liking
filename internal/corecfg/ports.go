package corecfg

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

const (
	DefaultPortMin = 10000
	DefaultPortMax = 59999
)

// NormalizePortRange returns a valid inclusive listen-port range.
// 0,0 means the panel default (10000–59999).
func NormalizePortRange(min, max int) (int, int, error) {
	if min == 0 && max == 0 {
		return DefaultPortMin, DefaultPortMax, nil
	}
	if min < 1 || min > 65535 || max < 1 || max > 65535 {
		return 0, 0, fmt.Errorf("端口区间 1–65535")
	}
	if min > max {
		return 0, 0, fmt.Errorf("起始端口不能大于结束端口")
	}
	return min, max, nil
}

// PickFreePort returns a random port in 10000–59999 that is not in used.
func PickFreePort(used map[int]struct{}) (int, error) {
	return PickFreePortRange(used, DefaultPortMin, DefaultPortMax)
}

// PickFreePortRange returns a random unused port in [min, max].
func PickFreePortRange(used map[int]struct{}, min, max int) (int, error) {
	min, max, err := NormalizePortRange(min, max)
	if err != nil {
		return 0, err
	}
	if used == nil {
		used = map[int]struct{}{}
	}
	span := uint32(max - min + 1)
	var buf [4]byte
	for i := 0; i < 64; i++ {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		p := min + int(binary.BigEndian.Uint32(buf[:])%span)
		if _, taken := used[p]; !taken {
			return p, nil
		}
	}
	for p := min; p <= max; p++ {
		if _, taken := used[p]; !taken {
			return p, nil
		}
	}
	return 0, fmt.Errorf("没有可用端口")
}
