package agent

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const (
	mitaVer     = "3.36.1"
	singboxVer  = "1.14.0"
	xrayVer     = "26.3.27"
	coreHTTPTO  = 90 * time.Second
	mitaReadyTO = 15 * time.Second
)

// ensureCore installs the named core (xray / singbox / mita) into PATH if missing.
// Tests may replace this.
var ensureCore = defaultEnsureCore

func defaultEnsureCore(name string) (string, error) {
	switch name {
	case "xray":
		if p := lookBin("xray"); p != "" {
			return p, nil
		}
		if err := installXray(); err != nil {
			return "", err
		}
		if p := lookBin("xray"); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("安装后仍找不到 xray")
	case "singbox":
		if p := lookBin("sing-box", "singbox"); p != "" {
			return p, nil
		}
		if err := installSingbox(); err != nil {
			return "", err
		}
		if p := lookBin("sing-box", "singbox"); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("安装后仍找不到 sing-box")
	case "mita":
		if p := lookBin("mita"); p != "" {
			return p, nil
		}
		if err := installMita(); err != nil {
			return "", err
		}
		if p := lookBin("mita"); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("安装后仍找不到 mita")
	default:
		return "", fmt.Errorf("未知内核 %s", name)
	}
}

func removeCoreBin(name string) error {
	name = normalizeAgentCore(name)
	switch name {
	case "xray":
		return removeOwnedBins("xray")
	case "singbox":
		return removeOwnedBins("sing-box", "singbox")
	case "mita":
		return removeMitaBin()
	default:
		return fmt.Errorf("未知内核 %s", name)
	}
}

func removeOwnedBins(names ...string) error {
	var last string
	for _, n := range names {
		for _, dir := range []string{"/usr/local/bin"} {
			p := filepath.Join(dir, n)
			if st, err := os.Stat(p); err == nil && !st.IsDir() {
				last = p
				if err := os.Remove(p); err != nil {
					return fmt.Errorf("删除 %s: %w", p, err)
				}
			}
		}
	}
	if lookBin(names...) != "" {
		if last == "" {
			return fmt.Errorf("%s 不在 /usr/local/bin，请手动卸载", names[0])
		}
		return fmt.Errorf("已删除 %s，但仍在 PATH", last)
	}
	return nil
}

func removeMitaBin() error {
	if bin := lookBin("mita"); bin != "" {
		_ = exec.Command(bin, "stop").Run()
	}
	_ = exec.Command("systemctl", "disable", "--now", "mita.service").Run()
	if hasCmd("dpkg") {
		cmd := exec.Command("dpkg", "-r", "mita")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		_ = cmd.Run()
	}
	if hasCmd("rpm") {
		_ = exec.Command("rpm", "-e", "mita").Run()
	}
	_ = os.Remove("/usr/local/bin/mita")
	_ = os.Remove("/etc/systemd/system/mita.service")
	_ = exec.Command("systemctl", "daemon-reload").Run()
	if lookBin("mita") != "" {
		return fmt.Errorf("mita 仍在 PATH，请手动卸载")
	}
	return nil
}

func linuxArch() string {
	switch runtime.GOARCH {
	case "arm64", "aarch64":
		return "arm64"
	default:
		return "amd64"
	}
}

var githubProxyFile = "/etc/liking/gh-proxy"

func githubProxy() string {
	p := strings.TrimRight(strings.TrimSpace(os.Getenv("LIKING_GITHUB_PROXY")), "/")
	if p != "" {
		return p
	}
	b, err := os.ReadFile(githubProxyFile)
	if err != nil {
		return ""
	}
	return strings.TrimRight(strings.TrimSpace(string(b)), "/")
}

func withGHProxy(u string) string {
	p := githubProxy()
	if p == "" {
		return u
	}
	return p + "/" + u
}

func mitaDebURL(arch string) string {
	a := "amd64"
	if arch == "arm64" {
		a = "arm64"
	}
	return fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita_%s_%s.deb", mitaVer, mitaVer, a)
}

func mitaRPMURL(arch string) string {
	a := "x86_64"
	if arch == "arm64" {
		a = "aarch64"
	}
	return fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita-%s-1.%s.rpm", mitaVer, mitaVer, a)
}

func mitaTarURL(arch string) string {
	a := "amd64"
	if arch == "arm64" {
		a = "arm64"
	}
	return fmt.Sprintf("https://github.com/enfein/mieru/releases/download/v%s/mita_%s_linux_%s.tar.gz", mitaVer, mitaVer, a)
}

func installMita() error {
	arch := linuxArch()
	tmp, err := os.MkdirTemp("", "liking-mita-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	switch {
	case hasCmd("dpkg"):
		deb := filepath.Join(tmp, "mita.deb")
		u := mitaDebURL(arch)
		if err := downloadVerified(u, u+".sha256.txt", deb); err != nil {
			return err
		}
		cmd := exec.Command("dpkg", "-i", deb)
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		if out, err := cmd.CombinedOutput(); err != nil {
			log.Printf("agent: dpkg mita failed, trying tarball: %v %s", err, strings.TrimSpace(string(out)))
			if err := installMitaTarball(arch, tmp); err != nil {
				return err
			}
		}
	case hasCmd("rpm"):
		rpm := filepath.Join(tmp, "mita.rpm")
		u := mitaRPMURL(arch)
		if err := downloadVerified(u, u+".sha256.txt", rpm); err != nil {
			return err
		}
		if out, err := exec.Command("rpm", "-Uvh", "--force", rpm).CombinedOutput(); err != nil {
			log.Printf("agent: rpm mita failed, trying tarball: %v %s", err, strings.TrimSpace(string(out)))
			if err := installMitaTarball(arch, tmp); err != nil {
				return err
			}
		}
	default:
		if err := installMitaTarball(arch, tmp); err != nil {
			return err
		}
	}
	return waitMitaReady()
}

func installMitaTarball(arch, tmp string) error {
	u := mitaTarURL(arch)
	tg := filepath.Join(tmp, "mita.tar.gz")
	if err := downloadVerified(u, u+".sha256.txt", tg); err != nil {
		return err
	}
	dest := "/usr/local/bin/mita"
	if err := extractNamedTarGz(tg, "mita", dest); err != nil {
		return err
	}
	if err := os.Chmod(dest, 0o755); err != nil {
		return err
	}
	_ = exec.Command("useradd", "--no-create-home", "--user-group", "mita").Run()
	for _, dir := range []string{"/etc/mita", "/var/lib/mita", "/var/run/mita"} {
		if err := os.MkdirAll(dir, 0o775); err != nil {
			return err
		}
		_ = exec.Command("chown", "-R", "mita:mita", dir).Run()
	}
	unit := `[Unit]
Description=Mieru proxy server
After=network-online.target
Wants=network-online.target

[Service]
Type=exec
User=mita
Group=mita
AmbientCapabilities=CAP_NET_BIND_SERVICE
Environment="MITA_LOG_NO_TIMESTAMP=true"
ExecStartPre=+/usr/bin/mkdir -p /var/run/mita
ExecStartPre=+/usr/bin/chown -R mita:mita /var/run/mita
ExecStartPre=+/usr/bin/chmod 775 /var/run/mita
ExecStart=/usr/local/bin/mita run
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
`
	if err := os.WriteFile("/etc/systemd/system/mita.service", []byte(unit), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "daemon-reload").Run()
	_ = exec.Command("systemctl", "enable", "--now", "mita.service").Run()
	return nil
}

func waitMitaReady() error {
	bin := lookBin("mita")
	if bin == "" {
		return fmt.Errorf("mita 不在 PATH")
	}
	deadline := time.Now().Add(mitaReadyTO)
	var last string
	for time.Now().Before(deadline) {
		out, err := exec.Command(bin, "status").CombinedOutput()
		s := strings.ToUpper(string(out))
		if err == nil && (strings.Contains(s, "IDLE") || strings.Contains(s, "RUNNING")) {
			return nil
		}
		last = strings.TrimSpace(string(out))
		if last == "" && err != nil {
			last = err.Error()
		}
		time.Sleep(300 * time.Millisecond)
	}
	return fmt.Errorf("mita 守护进程未就绪: %s", last)
}

func installSingbox() error {
	arch := linuxArch()
	name := fmt.Sprintf("sing-box-%s-linux-%s.tar.gz", singboxVer, arch)
	u := fmt.Sprintf("https://github.com/SagerNet/sing-box/releases/download/v%s/%s", singboxVer, name)
	tmp, err := os.MkdirTemp("", "liking-singbox-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	tg := filepath.Join(tmp, name)
	if err := download(u, tg); err != nil {
		return err
	}
	dest := "/usr/local/bin/sing-box"
	if err := extractNamedTarGz(tg, "sing-box", dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0o755)
}

func installXray() error {
	asset := "Xray-linux-64.zip"
	if linuxArch() == "arm64" {
		asset = "Xray-linux-arm64-v8a.zip"
	}
	u := fmt.Sprintf("https://github.com/XTLS/Xray-core/releases/download/v%s/%s", xrayVer, asset)
	tmp, err := os.MkdirTemp("", "liking-xray-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	zipPath := filepath.Join(tmp, "xray.zip")
	if err := download(u, zipPath); err != nil {
		return err
	}
	dest := "/usr/local/bin/xray"
	if err := extractNamedZip(zipPath, "xray", dest); err != nil {
		return err
	}
	return os.Chmod(dest, 0o755)
}

func hasCmd(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func downloadVerified(u, shaURL, dest string) error {
	if err := download(u, dest); err != nil {
		return err
	}
	want, err := fetchSHA256(shaURL)
	if err != nil || want == "" {
		log.Printf("agent: skip sha256 for %s: %v", u, err)
		return nil
	}
	got, err := fileSHA256(dest)
	if err != nil {
		return err
	}
	if !strings.EqualFold(got, want) {
		return fmt.Errorf("sha256 不匹配 %s", filepath.Base(dest))
	}
	return nil
}

func download(u, dest string) error {
	req, err := http.NewRequest(http.MethodGet, withGHProxy(u), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "liking-agent")
	client := &http.Client{Timeout: coreHTTPTO}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("下载 %s: %w", u, err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return fmt.Errorf("下载 %s: HTTP %d", u, res.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := io.Copy(f, res.Body); err != nil {
		return err
	}
	return nil
}

func fetchSHA256(u string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, withGHProxy(u), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "liking-agent")
	client := &http.Client{Timeout: 20 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return "", fmt.Errorf("HTTP %d", res.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty sha")
	}
	sum := fields[0]
	if len(sum) != 64 {
		return "", fmt.Errorf("bad sha %q", sum)
	}
	return sum, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func extractNamedTarGz(path, want, dest string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.FileInfo().IsDir() {
			continue
		}
		base := filepath.Base(hdr.Name)
		if base != want {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, tr)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("%s 里没有 %s", filepath.Base(path), want)
}

func extractNamedZip(path, want, dest string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		if filepath.Base(f.Name) != want {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			rc.Close()
			return err
		}
		out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
	return fmt.Errorf("%s 里没有 %s", filepath.Base(path), want)
}
