package server

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"liking/internal/db"
	"liking/internal/version"
)

const maxBackupUpload = 32 << 20

func (s *Server) handleBackupSummary(w http.ResponseWriter, r *http.Request) {
	sum, err := db.LiveSummary(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, sum)
}

func (s *Server) handleBackupDownload(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Password        string `json:"password"`
		EncryptPassword string `json:"encrypt_password"`
	}
	_ = decodeJSON(r, &req)
	confirm := strings.TrimSpace(req.Password)
	if confirm == "" {
		jsonErr(w, http.StatusBadRequest, "请填写当前密码")
		return
	}
	if !checkPassword(u.PasswordHash, confirm) {
		jsonErr(w, http.StatusForbidden, "密码不对")
		return
	}
	encrypt := strings.TrimSpace(req.EncryptPassword)
	bak, _ := db.GetSetting(s.DB, "backup_password")
	bak = strings.TrimSpace(bak)
	if encrypt == "" {
		encrypt = bak
	}
	if encrypt == "" {
		jsonErr(w, http.StatusBadRequest, "请设置备份加密密码，不能下载明文备份")
		return
	}
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	raw, name, err := s.encodeBackup(encrypt)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	db.AddAudit(s.DB, &u.ID, "backup.download", fmt.Sprintf("v%s %d bytes", version.Version, len(raw)))
	ct := "application/gzip"
	if encrypt != "" {
		ct = "application/octet-stream"
		if !strings.HasSuffix(name, ".lkbak") {
			name += ".lkbak"
		}
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Backup-Filename", name)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) encodeBackup(password string) ([]byte, string, error) {
	snap, err := db.Export(s.DB, version.Version)
	if err != nil {
		return nil, "", fmt.Errorf("导出失败：%s", err.Error())
	}
	raw, err := snap.EncodeGzip()
	if err != nil {
		return nil, "", fmt.Errorf("压缩失败")
	}
	name := "liking-backup-" + time.Now().UTC().Format("2006-01-02") + ".lkbak"
	if password != "" {
		enc, err := db.EncryptBackup(raw, password)
		if err != nil {
			return nil, "", err
		}
		raw = enc
		name = "liking-backup-" + time.Now().UTC().Format("2006-01-02") + ".lkb1"
	}
	return raw, name, nil
}

func (s *Server) readUploadedBackup(r *http.Request) (*db.Snapshot, error) {
	ct := r.Header.Get("Content-Type")
	password := strings.TrimSpace(r.Header.Get("X-Liking-Backup-Password"))
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(maxBackupUpload); err != nil {
			return nil, fmt.Errorf("读备份失败")
		}
		if v := strings.TrimSpace(r.FormValue("file_password")); v != "" {
			password = v
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("请选择备份文件")
		}
		defer f.Close()
		return db.DecodeSnapshotMaybeEncrypted(io.LimitReader(f, maxBackupUpload), password)
	}
	return db.DecodeSnapshotMaybeEncrypted(io.LimitReader(r.Body, maxBackupUpload), password)
}

func (s *Server) handleBackupPreview(w http.ResponseWriter, r *http.Request) {
	snap, err := s.readUploadedBackup(r)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.ValidateSnapshot(snap); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, snap.Summary())
}

func (s *Server) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}

	s.backupMu.Lock()
	defer s.backupMu.Unlock()

	var password string
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(maxBackupUpload); err != nil {
			jsonErr(w, http.StatusBadRequest, "读备份失败")
			return
		}
		password = strings.TrimSpace(r.FormValue("password"))
		f, _, err := r.FormFile("file")
		if err != nil {
			jsonErr(w, http.StatusBadRequest, "请选择备份文件")
			return
		}
		defer f.Close()
		s.restoreFrom(w, r, u, password, f)
		return
	}
	password = strings.TrimSpace(r.Header.Get("X-Liking-Password"))
	s.restoreFrom(w, r, u, password, io.LimitReader(r.Body, maxBackupUpload))
}

func (s *Server) restoreFrom(w http.ResponseWriter, r *http.Request, u *db.User, password string, body io.Reader) {
	if password == "" {
		jsonErr(w, http.StatusBadRequest, "请填写当前管理员密码")
		return
	}
	if !checkPassword(u.PasswordHash, password) {
		jsonErr(w, http.StatusForbidden, "密码不对")
		return
	}
	filePass := strings.TrimSpace(r.Header.Get("X-Liking-Backup-Password"))
	if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "multipart/") {
		if v := strings.TrimSpace(r.FormValue("file_password")); v != "" {
			filePass = v
		}
	}
	snap, err := db.DecodeSnapshotMaybeEncrypted(body, filePass)
	if err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := db.ValidateSnapshot(snap); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.Hub.DropAll()
	if err := db.Import(s.DB, snap); err != nil {
		jsonErr(w, http.StatusBadRequest, "恢复失败："+err.Error())
		return
	}
	_ = db.MarkAllServersOffline(s.DB)
	db.AddAudit(s.DB, nil, "backup.restore", fmt.Sprintf("from %s at %d", snap.AppVersion, snap.CreatedAt))
	http.SetCookie(w, newSessionCookie(r, "", -1))
	jsonOK(w, map[string]any{"ok": true, "relogin": true})
}

func (s *Server) backupLoop() {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	s.maybeScheduledBackup()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.maybeScheduledBackup()
		}
	}
}

func (s *Server) maybeScheduledBackup() {
	hourRaw, _ := db.GetSetting(s.DB, "backup_hour")
	hourRaw = strings.TrimSpace(hourRaw)
	if hourRaw == "off" || hourRaw == "-" {
		return
	}
	hour := 3
	if hourRaw != "" {
		n, err := strconv.Atoi(hourRaw)
		if err != nil || n < 0 || n > 23 {
			return
		}
		hour = n
	}
	now := db.ClockNow(s.DB)
	if now.Hour() != hour {
		return
	}
	day := now.Format("2006-01-02")
	last, _ := db.GetSetting(s.DB, "backup_last_day")
	if last == day {
		return
	}
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	last, _ = db.GetSetting(s.DB, "backup_last_day")
	if last == day {
		return
	}
	dir := filepath.Join(s.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		log.Printf("scheduled backup: mkdir: %v", err)
		return
	}
	pw, _ := db.GetSetting(s.DB, "backup_password")
	pw = strings.TrimSpace(pw)
	if pw == "" {
		log.Printf("scheduled backup: skipped, backup password is empty")
		return
	}
	raw, name, err := s.encodeBackup(pw)
	if err != nil {
		log.Printf("scheduled backup: %v", err)
		return
	}
	path := filepath.Join(dir, "auto-"+name)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		log.Printf("scheduled backup write: %v", err)
		return
	}
	_ = db.SetSetting(s.DB, "backup_last_day", day)
	pruneAutoBackups(dir, db.SettingInt(s.DB, "backup_keep", 7))
}

func pruneAutoBackups(dir string, keep int) {
	if keep < 1 {
		keep = 1
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasPrefix(n, "auto-liking-backup-") {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	if len(names) <= keep {
		return
	}
	for _, n := range names[:len(names)-keep] {
		_ = os.Remove(filepath.Join(dir, n))
	}
}
