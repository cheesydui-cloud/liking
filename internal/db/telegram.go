package db

import (
	"database/sql"
	"errors"
	"strings"
)

func SettingOn(d *sql.DB, key string, def bool) bool {
	v, err := GetSetting(d, key)
	if err != nil {
		return def
	}
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true")
}

var (
	ErrTGChatTaken = errors.New("这个 Telegram 已经绑定了别的账号")
	ErrTGCodeTaken = errors.New("绑定码重复")
)

const (
	TGKindBind  = "bind"
	TGKindAdmin = "admin"
)

func TGBind(d *sql.DB, userID, chatID int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var other int64
	err = tx.QueryRow(`SELECT user_id FROM tg_binds WHERE chat_id=?`, chatID).Scan(&other)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && other != userID {
		return ErrTGChatTaken
	}
	if _, err := tx.Exec(`DELETE FROM tg_binds WHERE user_id=? OR chat_id=?`, userID, chatID); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO tg_binds(user_id, chat_id, bound_at) VALUES(?,?,?)`, userID, chatID, now()); err != nil {
		return err
	}
	return tx.Commit()
}

func TGUnbindUser(d *sql.DB, userID int64) error {
	_, err := d.Exec(`DELETE FROM tg_binds WHERE user_id=?`, userID)
	return err
}

func TGUserByChat(d *sql.DB, chatID int64) (int64, error) {
	var id int64
	err := d.QueryRow(`SELECT user_id FROM tg_binds WHERE chat_id=?`, chatID).Scan(&id)
	return id, err
}

func TGChatByUser(d *sql.DB, userID int64) (int64, bool, error) {
	var id int64
	err := d.QueryRow(`SELECT chat_id FROM tg_binds WHERE user_id=?`, userID).Scan(&id)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return id, true, nil
}

func TGNotifyGet(d *sql.DB, key string) (last int64, state string, ok bool, err error) {
	err = d.QueryRow(`SELECT last_sent, state FROM tg_notify_state WHERE key=?`, key).Scan(&last, &state)
	if err == sql.ErrNoRows {
		return 0, "", false, nil
	}
	if err != nil {
		return 0, "", false, err
	}
	return last, state, true, nil
}

func TGNotifyPut(d *sql.DB, key string, last int64, state string) error {
	_, err := d.Exec(`INSERT INTO tg_notify_state(key, last_sent, state) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET last_sent=excluded.last_sent, state=excluded.state`, key, last, state)
	return err
}

// IssueTGCode replaces the previous code for this user and kind. The code must
// be unique among codes that are still stored.
func IssueTGCode(d *sql.DB, userID int64, kind, code string, expiresAt, issuedAt int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM tg_codes WHERE user_id=? AND kind=?`, userID, kind); err != nil {
		return err
	}
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE code=?`, code).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrTGCodeTaken
	}
	if _, err := tx.Exec(`INSERT INTO tg_codes(code, user_id, kind, expires_at, fails, issued_at) VALUES(?,?,?,?,0,?)`, code, userID, kind, expiresAt, issuedAt); err != nil {
		return err
	}
	return tx.Commit()
}

// TakeTGCode consumes a matching code. A wrong code counts against the newest
// outstanding code, and five failures void it.
func TakeTGCode(d *sql.DB, code string, now int64, maxFail int) (userID int64, kind, msg string, err error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, "", "", err
	}
	defer tx.Rollback()
	var exp int64
	err = tx.QueryRow(`SELECT user_id, kind, expires_at FROM tg_codes WHERE code=?`, code).Scan(&userID, &kind, &exp)
	if err == nil {
		if now > exp {
			if _, err = tx.Exec(`DELETE FROM tg_codes WHERE code=?`, code); err != nil {
				return 0, "", "", err
			}
			if err = tx.Commit(); err != nil {
				return 0, "", "", err
			}
			return 0, "", "绑定码已过期。回到面板，重新点「用 Telegram 打开」。", nil
		}
		if _, err = tx.Exec(`DELETE FROM tg_codes WHERE code=?`, code); err != nil {
			return 0, "", "", err
		}
		if err = tx.Commit(); err != nil {
			return 0, "", "", err
		}
		return userID, kind, "", nil
	}
	if err != sql.ErrNoRows {
		return 0, "", "", err
	}
	var newest string
	var fails int
	err = tx.QueryRow(`SELECT code, fails FROM tg_codes ORDER BY issued_at DESC, code DESC LIMIT 1`).Scan(&newest, &fails)
	if err == sql.ErrNoRows {
		if err = tx.Commit(); err != nil {
			return 0, "", "", err
		}
		return 0, "", "绑定码不对", nil
	}
	if err != nil {
		return 0, "", "", err
	}
	fails++
	if fails >= maxFail {
		if _, err = tx.Exec(`DELETE FROM tg_codes WHERE code=?`, newest); err != nil {
			return 0, "", "", err
		}
		if err = tx.Commit(); err != nil {
			return 0, "", "", err
		}
		return 0, "", "绑定码已作废。回到面板，重新点「用 Telegram 打开」。", nil
	}
	if _, err = tx.Exec(`UPDATE tg_codes SET fails=? WHERE code=?`, fails, newest); err != nil {
		return 0, "", "", err
	}
	if err = tx.Commit(); err != nil {
		return 0, "", "", err
	}
	return 0, "", "绑定码不对", nil
}
