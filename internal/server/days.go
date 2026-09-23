package server

import (
	"fmt"
	"time"
)

const maxUserDays = 36500

func expiryFromDays(days int) (int64, error) {
	if days < 0 || days > maxUserDays {
		return 0, fmt.Errorf("天数无效")
	}
	if days == 0 {
		return 0, nil
	}
	return time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix(), nil
}
