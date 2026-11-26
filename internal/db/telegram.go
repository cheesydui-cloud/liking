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

func TGBoundIDs(d *sql.DB) (map[int64]struct{}, error) {
	rows, err := d.Query(`SELECT user_id FROM tg_binds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]struct{}{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id > 0 {
			out[id] = struct{}{}
		}
	}
	return out, rows.Err()
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

// IssueTGCode returns the code that should go in the Telegram link. An unexpired
// code for this user and kind is reused. Replacing it would make the /start
// payload Telegram already cached look wrong.
func IssueTGCode(d *sql.DB, userID int64, kind, code string, expiresAt, issuedAt int64) (string, int64, error) {
	tx, err := d.Begin()
	if err != nil {
		return "", 0, err
	}
	defer tx.Rollback()
	var existing string
	var exp int64
	err = tx.QueryRow(`SELECT code, expires_at FROM tg_codes WHERE user_id=? AND kind=?`, userID, kind).Scan(&existing, &exp)
	if err == nil && existing != "" && exp > issuedAt {
		if err = tx.Commit(); err != nil {
			return "", 0, err
		}
		return existing, exp, nil
	}
	if err != nil && err != sql.ErrNoRows {
		return "", 0, err
	}
	if _, err = tx.Exec(`DELETE FROM tg_codes WHERE user_id=? AND kind=?`, userID, kind); err != nil {
		return "", 0, err
	}
	var n int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM tg_codes WHERE code=?`, code).Scan(&n); err != nil {
		return "", 0, err
	}
	if n > 0 {
		return "", 0, ErrTGCodeTaken
	}
	if _, err = tx.Exec(`INSERT INTO tg_codes(code, user_id, kind, expires_at, fails, issued_at) VALUES(?,?,?,?,0,?)`, code, userID, kind, expiresAt, issuedAt); err != nil {
		return "", 0, err
	}
	if err = tx.Commit(); err != nil {
		return "", 0, err
	}
	return code, expiresAt, nil
}

// TakeTGCode consumes a matching code. A wrong code leaves other outstanding
// codes alone. Voiding the newest one made a stale /start payload destroy the
// code the panel had just shown.
func TakeTGCode(d *sql.DB, code string, now int64) (userID int64, kind, msg string, err error) {
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
	if err = tx.Commit(); err != nil {
		return 0, "", "", err
	}
	return 0, "", "绑定码不对", nil
}
