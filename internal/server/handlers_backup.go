package server

import (
	"fmt"
	"io"
	"net/http"
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
	s.backupMu.Lock()
	defer s.backupMu.Unlock()
	snap, err := db.Export(s.DB, version.Version)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "导出失败："+err.Error())
		return
	}
	raw, err := snap.EncodeGzip()
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "压缩失败")
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "backup.download", fmt.Sprintf("v%s %d bytes", version.Version, len(raw)))
	name := "liking-backup-" + time.Now().UTC().Format("2006-01-02") + ".lkbak"
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	w.Header().Set("X-Backup-Filename", name)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(raw)
}

func (s *Server) readUploadedBackup(r *http.Request) (*db.Snapshot, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/") {
		if err := r.ParseMultipartForm(maxBackupUpload); err != nil {
			return nil, fmt.Errorf("读备份失败")
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("请选择备份文件")
		}
		defer f.Close()
		return db.DecodeSnapshot(io.LimitReader(f, maxBackupUpload))
	}
	return db.DecodeSnapshot(io.LimitReader(r.Body, maxBackupUpload))
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
	snap, err := db.DecodeSnapshot(body)
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
