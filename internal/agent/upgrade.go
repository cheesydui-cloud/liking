package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"liking/internal/wsproto"
)

const maxAgentBin = 80 << 20

func (a *Agent) httpClient() *http.Client {
	tr := &http.Transport{}
	if a.cfg.Insecure {
		tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec
	}
	return &http.Client{Transport: tr, Timeout: 3 * time.Minute}
}

func (a *Agent) resolveURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("缺少下载地址")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	if u.IsAbs() {
		return u.String(), nil
	}
	base, err := url.Parse(a.cfg.ConnectURL)
	if err != nil {
		return "", err
	}
	base.Path = ""
	base.RawQuery = ""
	base.Fragment = ""
	switch base.Scheme {
	case "wss":
		base.Scheme = "https"
	case "ws":
		base.Scheme = "http"
	}
	return base.ResolveReference(u).String(), nil
}

func (a *Agent) replaceSelf(ctx context.Context, req wsproto.Upgrade) error {
	src, err := a.resolveURL(req.URL)
	if err != nil {
		return err
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return err
	}
	tmp := exe + ".new"
	if err := downloadFile(ctx, a.httpClient(), src, tmp); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	sum, err := fileSHA256(tmp)
	if err != nil {
		_ = os.Remove(tmp)
		return err
	}
	want := strings.ToLower(strings.TrimSpace(req.SHA256))
	if want != "" && want != sum {
		_ = os.Remove(tmp)
		return fmt.Errorf("校验失败")
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, exe); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func downloadFile(ctx context.Context, client *http.Client, src, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src, nil)
	if err != nil {
		return err
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败 HTTP %d", res.StatusCode)
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, io.LimitReader(res.Body, maxAgentBin+1)); err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		return err
	}
	if st.Size() < 1<<20 {
		return fmt.Errorf("下载的文件太小")
	}
	if st.Size() > maxAgentBin {
		return fmt.Errorf("下载的文件太大")
	}
	return f.Close()
}

func colocatedPanel() bool {
	if _, err := os.Stat("/etc/systemd/system/liking-server.service"); err == nil {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	dir := filepath.Dir(exe)
	if st, err := os.Stat(filepath.Join(dir, "liking-server")); err == nil && !st.IsDir() {
		return true
	}
	return false
}

func (a *Agent) uninstall() error {
	a.cores.StopAll()
	_ = exec.Command("systemctl", "disable", "--now", "liking-agent.service").Run()
	_ = os.Remove("/etc/systemd/system/liking-agent.service")
	_ = exec.Command("systemctl", "daemon-reload").Run()
	if a.cfg.Dir != "" && a.cfg.Dir != "/" {
		_ = os.RemoveAll(a.cfg.Dir)
	}
	if a.cfg.TokenFile != "" {
		_ = os.Remove(a.cfg.TokenFile)
	}
	if !colocatedPanel() {
		if exe, err := os.Executable(); err == nil {
			if exe, err = filepath.EvalSymlinks(exe); err == nil {
				_ = os.Remove(exe)
			}
		}
	}
	return nil
}
