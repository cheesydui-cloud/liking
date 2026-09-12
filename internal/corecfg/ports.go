package corecfg

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

const (
	freePortMin = 10000
	freePortMax = 59999
)

// PickFreePort returns a random port in 10000–59999 that is not in used.
func PickFreePort(used map[int]struct{}) (int, error) {
	if used == nil {
		used = map[int]struct{}{}
	}
	span := uint32(freePortMax - freePortMin + 1)
	var buf [4]byte
	for i := 0; i < 64; i++ {
		if _, err := rand.Read(buf[:]); err != nil {
			return 0, err
		}
		p := freePortMin + int(binary.BigEndian.Uint32(buf[:])%span)
		if _, taken := used[p]; !taken {
			return p, nil
		}
	}
	for p := freePortMin; p <= freePortMax; p++ {
		if _, taken := used[p]; !taken {
			return p, nil
		}
	}
	return 0, fmt.Errorf("没有可用端口")
}
