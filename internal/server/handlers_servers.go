package server

import (
	"context"
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
	"time"

	"liking/internal/certs"
	"liking/internal/corecfg"
	"liking/internal/db"
	"liking/internal/version"
	"liking/internal/wsproto"
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

func (s *Server) decorateServers(list []*db.Server) {
	s.reconcileOnline(list)
	totals, _ := db.ServerQuotaTotals(s.DB, list, db.ClockNow(s.DB))
	for _, x := range list {
		x.Token = ""
		if t, ok := totals[x.ID]; ok {
			x.UsedUp = t.Up
			x.UsedDown = t.Down
		}
		x.OverQuota = db.ServerOverQuota(x, x.UsedUp+x.UsedDown)
		x.NeedsUpgrade = x.Online == 1 && x.AgentVer != "" && x.AgentVer != version.Version
		x.NeedsReinstall = x.Online == 1 && !version.CanRemoteUpgrade(x.AgentVer)
		x.CanPushCores = s.Hub.HasCap(x.ID, wsproto.CapCores)
		if up, down, ok := s.Hub.Live(x.ID); ok {
			x.NetUpBps = up
			x.NetDownBps = down
		}
		if h, ok := s.Hub.Health(x.ID); ok {
			x.DiskFree = h.DiskFree
			x.DiskTotal = h.DiskTotal
			x.MemAvail = h.MemAvail
			x.MemTotal = h.MemTotal
			x.LoadMilli = h.LoadMilli
			x.Conns = h.Conns
			if len(h.CoresRunning) > 0 {
				x.CoresRunning = strings.Join(h.CoresRunning, ",")
			}
		}
	}
}

func (s *Server) serverOverMap() map[int64]bool {
	list, err := db.ListServers(s.DB)
	if err != nil {
		return nil
	}
	totals, _ := db.ServerQuotaTotals(s.DB, list, db.ClockNow(s.DB))
	out := map[int64]bool{}
	for _, x := range list {
		t := totals[x.ID]
		out[x.ID] = db.ServerOverQuota(x, t.Up+t.Down)
	}
	return out
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	list, err := db.ListServers(s.DB)
	if err != nil {
		jsonErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if list == nil {
		list = []*db.Server{}
	}
	s.decorateServers(list)
	jsonOK(w, map[string]any{"servers": list})
}

func applyServerPortRange(srv *db.Server, minPtr, maxPtr *int) error {
	if minPtr == nil && maxPtr == nil {
		return nil
	}
	min, max := srv.PortMin, srv.PortMax
	if minPtr != nil {
		min = *minPtr
	}
	if maxPtr != nil {
		max = *maxPtr
	}
	nmin, nmax, err := corecfg.NormalizePortRange(min, max)
	if err != nil {
		return err
	}
	srv.PortMin, srv.PortMax = nmin, nmax
	return nil
}

func applyServerExpiry(srv *db.Server, expPtr *int64, resetPtr *int) error {
	if expPtr != nil {
		if *expPtr < 0 {
			return fmt.Errorf("到期时间无效")
		}
		srv.ExpiresAt = *expPtr
	}
	if resetPtr != nil {
		if *resetPtr < 0 || *resetPtr > 31 {
			return fmt.Errorf("流量重置日 0–31")
		}
		srv.TrafficResetDay = *resetPtr
	}
	return nil
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name            string `json:"name"`
		PublicHost      string `json:"public_host"`
		PortMin         *int   `json:"port_min"`
		PortMax         *int   `json:"port_max"`
		ExpiresAt       *int64 `json:"expires_at"`
		TrafficResetDay *int   `json:"traffic_reset_day"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Name) == "" {
		jsonErr(w, http.StatusBadRequest, "需要名称")
		return
	}
	dummy := &db.Server{PortMin: corecfg.DefaultPortMin, PortMax: corecfg.DefaultPortMax}
	if err := applyServerPortRange(dummy, req.PortMin, req.PortMax); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyServerExpiry(dummy, req.ExpiresAt, req.TrafficResetDay); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
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
	need := srv.PortMin != dummy.PortMin || srv.PortMax != dummy.PortMax || srv.ExpiresAt != dummy.ExpiresAt || srv.TrafficResetDay != dummy.TrafficResetDay
	if need {
		srv.PortMin, srv.PortMax = dummy.PortMin, dummy.PortMax
		srv.ExpiresAt, srv.TrafficResetDay = dummy.ExpiresAt, dummy.TrafficResetDay
		if err := db.UpdateServer(s.DB, srv); err != nil {
			jsonErr(w, http.StatusInternalServerError, err.Error())
			return
		}
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
		Name            *string `json:"name"`
		PublicHost      *string `json:"public_host"`
		TrafficLimit    *int64  `json:"traffic_limit"`
		PortMin         *int    `json:"port_min"`
		PortMax         *int    `json:"port_max"`
		ExpiresAt       *int64  `json:"expires_at"`
		TrafficResetDay *int    `json:"traffic_reset_day"`
	}
	if err := decodeJSON(r, &req); err != nil {
		jsonErr(w, http.StatusBadRequest, "无效请求")
		return
	}
	if req.Name != nil && strings.TrimSpace(*req.Name) != "" {
		srv.Name = strings.TrimSpace(*req.Name)
	}
	if req.PublicHost != nil {
		srv.PublicHost = strings.TrimSpace(*req.PublicHost)
	}
	if req.TrafficLimit != nil {
		if *req.TrafficLimit < 0 {
			jsonErr(w, http.StatusBadRequest, "流量上限无效")
			return
		}
		srv.TrafficLimit = *req.TrafficLimit
	}
	if err := applyServerPortRange(srv, req.PortMin, req.PortMax); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := applyServerExpiry(srv, req.ExpiresAt, req.TrafficResetDay); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
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

func (s *Server) handleCFDomains(w http.ResponseWriter, r *http.Request) {
	s.writeCFDomains(w, r, "", "")
}

func (s *Server) handleServerCFDomains(w http.ResponseWriter, r *http.Request) {
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
	s.writeCFDomains(w, r, srv.ConnectIP, srv.PublicHost)
}

func (s *Server) writeCFDomains(w http.ResponseWriter, r *http.Request, connectIP, publicHost string) {
	token, _ := db.GetSetting(s.DB, "cf_api_token")
	if strings.TrimSpace(token) == "" {
		jsonErr(w, http.StatusBadRequest, "请先在设置里保存 Cloudflare API Token")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()
	cf := &certs.Cloudflare{Token: token, API: s.CFAPI}
	domains, err := cf.ListHostNames(ctx, connectIP, publicHost)
	if err != nil {
		jsonErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if domains == nil {
		domains = []certs.HostName{}
	}
	serverIP := strings.TrimSpace(connectIP)
	if serverIP == "" {
		serverIP = strings.TrimSpace(publicHost)
	}
	jsonOK(w, map[string]any{
		"server_ip": serverIP,
		"domains":   domains,
	})
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
