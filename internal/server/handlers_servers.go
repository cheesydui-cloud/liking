package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/version"
)

func corecfgCatalog() []corecfg.Meta { return corecfg.Catalog() }

func (s *Server) reconcileOnline(list []*db.Server) {
	for _, x := range list {
		if s.Hub.IsOnline(x.ID) {
			x.Online = 1
		} else if x.Online == 1 {
			x.Online = 0
			_ = db.MarkServerOffline(s.DB, x.ID)
		}
	}
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListServers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.reconcileOnline(list)
	if list == nil {
		list = []*db.Server{}
	}
	for _, x := range list {
		x.Token = ""
	}
	jsonOK(w, map[string]any{"servers": list})
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string `json:"name"`
		PublicHost string `json:"public_host"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		jsonErr(w, http.StatusBadRequest, "需要名称")
		return
	}
	tok, err := db.RandomHex(20)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	srv, err := db.CreateServer(s.DB, strings.TrimSpace(req.Name), strings.TrimSpace(req.PublicHost), tok)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	u := userFromCtx(r.Context())
	db.AddAudit(s.DB, &u.ID, "server.create", srv.Name)
	jsonOK(w, map[string]any{"server": srv, "install": s.installCmd(r, srv)})
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	var req struct {
		Name       string `json:"name"`
		PublicHost string `json:"public_host"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if strings.TrimSpace(req.Name) != "" {
		srv.Name = strings.TrimSpace(req.Name)
	}
	srv.PublicHost = strings.TrimSpace(req.PublicHost)
	if err := db.UpdateServer(s.DB, srv); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.syncServers(id)
	jsonOK(w, map[string]any{"server": srv})
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	s.Hub.Drop(id)
	if err := db.DeleteServer(s.DB, id); err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleSyncServer(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	if err := s.pushServer(id); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	jsonOK(w, map[string]any{"ok": true})
}

func (s *Server) handleServerInstall(w http.ResponseWriter, r *http.Request) {
	id, err := chiID(r, "id")
	if err != nil {
		jsonErr(w, http.StatusBadRequest, "无效 ID")
		return
	}
	srv, err := db.GetServer(s.DB, id)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "服务器不存在")
		return
	}
	jsonOK(w, map[string]any{"command": s.installCmd(r, srv), "token": srv.Token})
}

func (s *Server) installCmd(r *http.Request, srv *db.Server) string {
	base := panelURL(s.DB, r)
	insecure := ""
	if strings.HasPrefix(base, "http://") {
		insecure = " --insecure"
	}
	return fmt.Sprintf("curl -fsSL %s/v1/install-agent | bash -s -- --token %s%s", base, srv.Token, insecure)
}

func (s *Server) handleAgentBin(w http.ResponseWriter, r *http.Request) {
	arch := r.URL.Query().Get("arch")
	if arch == "" {
		arch = r.URL.Query().Get("GOARCH")
	}
	arch = normalizeArch(arch)
	osName := r.URL.Query().Get("os")
	if osName == "" {
		osName = "linux"
	}
	p := findAgentBinary(osName, arch)
	if p == "" {
		jsonErr(w, http.StatusNotFound, "面板没有对应架构的 agent 二进制，请在面板机执行 make dist")
		return
	}
	f, err := os.Open(p)
	if err != nil {
		jsonErr(w, http.StatusNotFound, "读取 agent 失败")
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		jsonErr(w, http.StatusNotFound, "读取 agent 失败")
		return
	}
	sum, err := sha256File(f)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, "计算校验失败")
		return
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		jsonErr(w, http.StatusInternalServerError, "读取 agent 失败")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=liking-agent")
	w.Header().Set("X-SHA256", sum)
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = io.Copy(w, f)
}

func sha256File(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func normalizeArch(a string) string {
	switch strings.ToLower(strings.TrimSpace(a)) {
	case "x86_64", "x64", "amd64":
		return "amd64"
	case "aarch64", "arm64":
		return "arm64"
	case "":
		return runtime.GOARCH
	default:
		return a
	}
}

func findAgentBinary(goos, arch string) string {
	name := "liking-agent-" + goos + "-" + arch
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, name))
		if runtime.GOOS == goos && runtime.GOARCH == arch {
			candidates = append(candidates, filepath.Join(dir, "liking-agent"))
		}
	}
	candidates = append(candidates, filepath.Join("bin", name), filepath.Join(".", name))
	if runtime.GOOS == goos && runtime.GOARCH == arch {
		candidates = append(candidates, filepath.Join("bin", "liking-agent"), filepath.Join(".", "liking-agent"))
	}
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err == nil && st.Size() > 1<<20 && !st.IsDir() {
			return p
		}
	}
	return ""
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListAudit(s.DB, 200)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.Audit{}
	}
	jsonOK(w, map[string]any{"audit": list, "version": version.Version})
}
