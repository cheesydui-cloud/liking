package server

import (
	"database/sql"
	"fmt"
	"time"

	"liking/internal/db"
)

const maxUserDays = 36500
const maxUserMonths = 1200

func expiryFromDays(days int) (int64, error) {
	if days < 0 || days > maxUserDays {
		return 0, fmt.Errorf("天数无效")
	}
	if days == 0 {
		return 0, nil
	}
	return time.Now().Add(time.Duration(days) * 24 * time.Hour).Unix(), nil
}

func expiryFromMonths(d *sql.DB, months int) (int64, error) {
	if months < 1 || months > maxUserMonths {
		return 0, fmt.Errorf("月数无效")
	}
	return db.AddMonths(db.ClockNow(d), months).Unix(), nil
}

func resolveCreateExpiry(d *sql.DB, expiresAt int64, days, months int) (int64, error) {
	if expiresAt < 0 || days < 0 || months < 0 {
		return 0, fmt.Errorf("到期时间无效")
	}
	if months > 0 && days > 0 {
		return 0, fmt.Errorf("月数和天数只能填一个")
	}
	if months > 0 && expiresAt > 0 {
		return 0, fmt.Errorf("月数和到期时间只能填一个")
	}
	if months > 0 {
		return expiryFromMonths(d, months)
	}
	if days > 0 {
		return expiryFromDays(days)
	}
	return expiresAt, nil
}

func anniversaryResetDay(d *sql.DB, pkg *db.Package) int {
	if pkg == nil || pkg.ResetMode != db.ResetModeAnniversary {
		return 0
	}
	return db.ClockNow(d).Day()
}
