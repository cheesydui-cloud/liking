package agent

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"liking/internal/wsproto"
)

const maxAgentBin = 80 << 20

func http1Transport(dial time.Duration) *http.Transport {
	return &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dial, KeepAlive: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   dial,
		ResponseHeaderTimeout: 30 * time.Second,
		ForceAttemptHTTP2:     false,
		TLSNextProto:          map[string]func(string, *tls.Conn) http.RoundTripper{},
	}
}

func (a *Agent) httpClient() *http.Client {
	return &http.Client{
		Timeout:   40 * time.Second,
		Transport: http1Transport(15 * time.Second),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return fmt.Errorf("下载不能跳转")
		},
	}
}

func fallbackHTTPClient() *http.Client {
	return &http.Client{
		Timeout:   90 * time.Second,
		Transport: http1Transport(20 * time.Second),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("跳转过多")
			}
			if !strings.EqualFold(req.URL.Scheme, "https") {
				return fmt.Errorf("下载不能跳到明文地址")
			}
			if fallbackRedirectHostOK(req.URL.Hostname(), via[0].URL.Hostname()) {
				return nil
			}
			return fmt.Errorf("下载不能跳到 %s", req.URL.Hostname())
		},
	}
}

func fallbackRedirectHostOK(host, origin string) bool {
	host = strings.ToLower(host)
	origin = strings.ToLower(origin)
	if host == origin || strings.HasSuffix(host, "."+origin) {
		return true
	}
	switch host {
	case "github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "github-releases.githubusercontent.com":
		return true
	}
	return strings.HasSuffix(host, ".githubusercontent.com") || strings.HasSuffix(host, ".github.com")
}

var agentFallbackPath = regexp.MustCompile(`^/cheesydui-cloud/liking/releases/download/v[0-9]+\.[0-9]+\.[0-9]+/liking-agent(-linux-(amd64|arm64))?$`)

func checkAgentFallbackURL(src string) error {
	u, err := url.Parse(src)
	if err != nil || u.Host == "" || u.User != nil {
		return fmt.Errorf("备用地址无效")
	}
	if !strings.EqualFold(u.Scheme, "https") || !strings.EqualFold(u.Hostname(), "github.com") {
		return fmt.Errorf("备用地址必须来自 GitHub")
	}
	if u.RawQuery != "" || u.Fragment != "" || !agentFallbackPath.MatchString(u.EscapedPath()) {
		return fmt.Errorf("备用地址无效")
	}
	return nil
}

func (a *Agent) upgradeSources(req wsproto.Upgrade) ([]string, error) {
	primary, err := a.resolveURL(req.URL)
	if err != nil {
		return nil, err
	}
	if err := a.checkUpgradeURL(primary); err != nil {
		return nil, err
	}
	out := []string{primary}
	fb := strings.TrimSpace(req.FallbackURL)
	if fb == "" || checkAgentFallbackURL(fb) != nil {
		return out, nil
	}
	if p := githubProxy(); p != "" {
		out = append(out, p+"/"+fb)
		return out, nil
	}
	out = append(out, fb)
	return out, nil
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

func (a *Agent) checkUpgradeURL(src string) error {
	u, err := url.Parse(src)
	if err != nil || u.Host == "" {
		return fmt.Errorf("下载地址无效")
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
	case "http":
		if !a.cfg.Insecure {
			return fmt.Errorf("明文下载需要 --insecure")
		}
	default:
		return fmt.Errorf("不支持的下载协议")
	}
	base, err := url.Parse(a.cfg.ConnectURL)
	if err != nil {
		return err
	}
	if !strings.EqualFold(u.Hostname(), base.Hostname()) {
		return fmt.Errorf("下载地址与面板不一致")
	}
	return nil
}

func (a *Agent) replaceSelf(ctx context.Context, req wsproto.Upgrade) error {
	sources, err := a.upgradeSources(req)
	if err != nil {
		return err
	}
	want := strings.ToLower(strings.TrimSpace(req.SHA256))
	if want == "" {
		return fmt.Errorf("缺少校验")
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
	var last error
	for i, src := range sources {
		client := a.httpClient()
		attempt := ctx
		cancel := func() {}
		if i == 0 {
			attempt, cancel = context.WithTimeout(ctx, 40*time.Second)
		} else {
			client = fallbackHTTPClient()
		}
		err := downloadFile(attempt, client, src, tmp)
		cancel()
		if err != nil {
			last = err
			_ = os.Remove(tmp)
			continue
		}
		sum, err := fileSHA256(tmp)
		if err != nil {
			last = err
			_ = os.Remove(tmp)
			continue
		}
		if want != sum {
			last = fmt.Errorf("校验失败")
			_ = os.Remove(tmp)
			continue
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
	if last == nil {
		last = fmt.Errorf("下载失败")
	}
	if len(sources) > 1 {
		return fmt.Errorf("面板和镜像都没下到 agent: %v", last)
	}
	return last
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
	applyDisableIPv6(false)
	_ = os.Remove(ipv6SysctlPath)
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
