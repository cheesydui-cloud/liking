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

var ErrTGChatTaken = errors.New("这个 Telegram 已经绑定了别的账号")

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
