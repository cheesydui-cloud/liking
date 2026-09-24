package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"liking/internal/db"
	"liking/internal/version"
)

const panelUpgradeUnit = "liking-self-upgrade"

var panelBootedAt = time.Now()

var (
	releaseTagRe       = regexp.MustCompile(`^v\d+\.\d+\.\d+$`)
	panelUpgradeScript = detectPanelUpgradeScript
	startPanelUpgrade  = execPanelUpgrade
	fetchPanelLatest   = fetchLatestRelease
	panelUpgradeHTTP   = &http.Client{Timeout: 8 * time.Second}
)

func detectPanelUpgradeScript() string {
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "liking-upgrade"))
	}
	candidates = append(candidates, "/usr/local/sbin/liking-upgrade")
	for _, p := range candidates {
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			continue
		}
		return p
	}
	return ""
}

func (s *Server) handlePanelUpgradeInfo(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{
		"version": version.Version,
		"started": panelBootedAt.Unix(),
		"script":  panelUpgradeScript() != "",
	}
	latest, err := fetchPanelLatest()
	if err != nil {
		out["latest_error"] = "查不到最新版本，仍可直接升级"
	} else {
		out["latest"] = strings.TrimPrefix(latest, "v")
		out["update"] = version.OlderThan(version.Version, latest)
	}
	jsonOK(w, out)
}

func (s *Server) handlePanelUpgrade(w http.ResponseWriter, r *http.Request) {
	u := userFromCtx(r.Context())
	if u == nil {
		jsonErr(w, http.StatusUnauthorized, "未登录")
		return
	}
	var req struct {
		Password string `json:"password"`
		Release  string `json:"release"`
	}
	_ = decodeJSON(r, &req)
	pw := strings.TrimSpace(req.Password)
	if pw == "" {
		jsonErr(w, http.StatusBadRequest, "请填写当前密码")
		return
	}
	if !checkPassword(u.PasswordHash, pw) {
		jsonErr(w, http.StatusForbidden, "密码不对")
		return
	}
	rel := strings.TrimSpace(req.Release)
	if rel != "" && !releaseTagRe.MatchString(rel) {
		jsonErr(w, http.StatusBadRequest, "版本号无效")
		return
	}
	script := panelUpgradeScript()
	if script == "" {
		jsonErr(w, http.StatusBadRequest, "这台机器没有 liking-upgrade，不能在页面里升级")
		return
	}
	if err := startPanelUpgrade(script, rel); err != nil {
		jsonErr(w, http.StatusBadRequest, err.Error())
		return
	}
	detail := "latest"
	if rel != "" {
		detail = rel
	}
	db.AddAudit(s.DB, &u.ID, "panel.upgrade", detail)
	jsonOK(w, map[string]any{"ok": true, "version": version.Version})
}

func panelUpgradeArgs(script, release string) []string {
	// "--" 必须在脚本前面。否则 systemd-run 会把 --release 当成自己的参数。
	args := []string{"--unit=" + panelUpgradeUnit, "--collect", "--no-block", "--", script}
	if release != "" {
		args = append(args, "--release", release)
	}
	return args
}

func execPanelUpgrade(script, release string) error {
	if _, err := exec.LookPath("systemd-run"); err != nil {
		return fmt.Errorf("需要 systemd 才能在页面里升级")
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("面板不是 root 在跑，不能在页面里升级")
	}
	out, _ := exec.Command("systemctl", "is-active", panelUpgradeUnit).Output()
	switch strings.TrimSpace(string(out)) {
	case "active", "activating":
		return fmt.Errorf("升级已在进行")
	}
	cmd := exec.Command("systemd-run", panelUpgradeArgs(script, release)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("启动升级失败：%s", msg)
	}
	return nil
}

func fetchLatestRelease() (string, error) {
	rawURL := "https://api.github.com/repos/cheesydui-cloud/liking/releases/latest"
	if p := readGHProxy(); p != "" {
		rawURL = strings.TrimRight(p, "/") + "/" + rawURL
	}
	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "liking-panel")
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := panelUpgradeHTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return "", err
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github HTTP %d", res.StatusCode)
	}
	var meta struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", err
	}
	tag := strings.TrimSpace(meta.TagName)
	if !releaseTagRe.MatchString(tag) {
		return "", fmt.Errorf("版本号无效")
	}
	return tag, nil
}

func readGHProxy() string {
	b, err := os.ReadFile("/etc/liking/gh-proxy")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
