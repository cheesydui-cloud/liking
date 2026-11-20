package agent

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// clockStepThreshold stays under the 30 second window SS2022 uses to reject
// a handshake. A machine this far off is stepped immediately.
const clockStepThreshold = 5 * time.Second

const timeSyncInstallGap = 30 * time.Minute

var (
	timeSyncMu         sync.Mutex
	lastTimeInstall    time.Time
	timeSyncDidInstall bool

	clockNow         = time.Now
	queryClock       = defaultQueryClock
	setSystemClock   = defaultSetSystemClock
	ntpSynchronized  = defaultNTPSynchronized
	enableNTP        = defaultEnableNTP
	installTimeSync  = defaultInstallTimeSync
	clockGOOS        = runtime.GOOS
	timeSyncManagers = defaultTimeSyncManagers
)

// ClockReport is one alignment attempt. Skew is local minus the reference
// clock before any step. After is the skew after a step, or Skew when the
// clock was already close enough.
type ClockReport struct {
	Measured bool
	Skew     time.Duration
	After    time.Duration
	Stepped  bool
	NTP      bool
	Err      error
}

// AlignClock measures the machine clock and steps it when it is far enough
// from a reference to break SS2022. It also turns on a persistent NTP
// service when one can be enabled or installed. force retries a package
// install even if a recent attempt failed.
func AlignClock(ctx context.Context, force bool) ClockReport {
	timeSyncMu.Lock()
	defer timeSyncMu.Unlock()
	return alignClockLocked(ctx, force)
}

func alignClockLocked(ctx context.Context, force bool) ClockReport {
	if clockGOOS != "linux" {
		return ClockReport{Err: fmt.Errorf("只能在 Linux 上对齐时间")}
	}
	remote, err := queryClock(ctx)
	if err != nil {
		return ClockReport{Err: fmt.Errorf("读不到标准时间: %w", err)}
	}
	skew := clockNow().Sub(remote)
	rep := ClockReport{Measured: true, Skew: skew, After: skew}
	if skew < -clockStepThreshold || skew > clockStepThreshold {
		if err := setSystemClock(remote); err != nil {
			rep.Err = err
			rep.NTP = ntpSynchronized()
			return rep
		}
		rep.Stepped = true
		if again, err := queryClock(ctx); err == nil {
			rep.After = clockNow().Sub(again)
		} else {
			rep.After = 0
		}
	}
	if !ntpSynchronized() {
		_ = enableNTP()
	}
	if !ntpSynchronized() && shouldInstallTimeSync(force) {
		lastTimeInstall = clockNow()
		if err := installTimeSync(); err != nil {
			rep.NTP = ntpSynchronized()
			if rep.Err == nil && (rep.After < -clockStepThreshold || rep.After > clockStepThreshold) {
				rep.Err = err
			}
			return rep
		}
		timeSyncDidInstall = true
		_ = enableNTP()
	}
	rep.NTP = ntpSynchronized()
	return rep
}

func shouldInstallTimeSync(force bool) bool {
	if timeSyncDidInstall {
		return false
	}
	if force || lastTimeInstall.IsZero() {
		return true
	}
	return clockNow().Sub(lastTimeInstall) >= timeSyncInstallGap
}

func defaultQueryClock(ctx context.Context) (time.Time, error) {
	var last error
	for _, host := range []string{"time.cloudflare.com", "time.google.com", "ntp.aliyun.com"} {
		t, err := queryNTP(ctx, host)
		if err == nil {
			return t, nil
		}
		last = err
	}
	for _, rawURL := range []string{"https://www.cloudflare.com", "https://www.google.com"} {
		t, err := queryHTTPDate(ctx, rawURL)
		if err == nil {
			return t, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("没有可用的时间源")
	}
	return time.Time{}, last
}

func queryNTP(ctx context.Context, host string) (time.Time, error) {
	d := net.Dialer{Timeout: 4 * time.Second}
	conn, err := d.DialContext(ctx, "udp", net.JoinHostPort(host, "123"))
	if err != nil {
		return time.Time{}, err
	}
	defer conn.Close()
	deadline := time.Now().Add(4 * time.Second)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)
	req := make([]byte, 48)
	req[0] = 0x1b
	t0 := time.Now()
	if _, err := conn.Write(req); err != nil {
		return time.Time{}, err
	}
	buf := make([]byte, 48)
	if _, err := io.ReadFull(conn, buf); err != nil {
		return time.Time{}, err
	}
	when, err := ntpTransmitTime(buf)
	if err != nil {
		return time.Time{}, err
	}
	return when.Add(time.Since(t0) / 2), nil
}

func ntpTransmitTime(pkt []byte) (time.Time, error) {
	if len(pkt) < 48 {
		return time.Time{}, fmt.Errorf("NTP 应答太短")
	}
	sec := binary.BigEndian.Uint32(pkt[40:44])
	frac := binary.BigEndian.Uint32(pkt[44:48])
	if sec == 0 {
		return time.Time{}, fmt.Errorf("NTP 应答没有时间")
	}
	unix := int64(sec) - 2208988800
	nsec := (int64(frac) * int64(time.Second)) >> 32
	return time.Unix(unix, nsec).UTC(), nil
}

func queryHTTPDate(ctx context.Context, rawURL string) (time.Time, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, rawURL, nil)
	if err != nil {
		return time.Time{}, err
	}
	req.Header.Set("User-Agent", "liking-agent")
	client := &http.Client{Timeout: 8 * time.Second}
	t0 := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	elapsed := time.Since(t0)
	raw := resp.Header.Get("Date")
	if raw == "" {
		return time.Time{}, fmt.Errorf("%s 没有 Date", rawURL)
	}
	when, err := http.ParseTime(raw)
	if err != nil {
		return time.Time{}, err
	}
	return when.Add(elapsed / 2), nil
}

func defaultSetSystemClock(t time.Time) error {
	if clockGOOS != "linux" {
		return fmt.Errorf("只能在 Linux 上对齐时间")
	}
	out, err := exec.Command("date", "-u", "-s", "@"+strconv.FormatInt(t.Unix(), 10)).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("设置系统时间: %s", msg)
	}
	_ = exec.Command("hwclock", "--systohc").Run()
	return nil
}

func defaultNTPSynchronized() bool {
	out, err := exec.Command("timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	return err == nil && strings.TrimSpace(string(out)) == "yes"
}

func defaultEnableNTP() error {
	out, err := exec.Command("timedatectl", "set-ntp", "true").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func defaultTimeSyncManagers() []nftPkgMgr {
	if p := lookBin("apt-get", "apt"); p != "" {
		return []nftPkgMgr{{Bin: p, Pre: []string{"update", "-qq"}, Args: []string{"install", "-y", "systemd-timesyncd"}}}
	}
	if p := lookBin("dnf"); p != "" {
		return []nftPkgMgr{{Bin: p, Args: []string{"install", "-y", "chrony"}}}
	}
	if p := lookBin("yum"); p != "" {
		return []nftPkgMgr{{Bin: p, Args: []string{"install", "-y", "chrony"}}}
	}
	if p := lookBin("apk"); p != "" {
		return []nftPkgMgr{{Bin: p, Args: []string{"add", "chrony"}}}
	}
	if p := lookBin("pacman"); p != "" {
		return []nftPkgMgr{{Bin: p, Args: []string{"-Sy", "--noconfirm", "chrony"}}}
	}
	if p := lookBin("zypper"); p != "" {
		return []nftPkgMgr{{Bin: p, Args: []string{"install", "-y", "chrony"}}}
	}
	return nil
}

func defaultInstallTimeSync() error {
	if clockGOOS != "linux" {
		return fmt.Errorf("需要 Linux 才能安装对时服务")
	}
	mgrs := timeSyncManagers()
	if len(mgrs) == 0 {
		return fmt.Errorf("没有可用的包管理器，无法安装对时服务")
	}
	var last error
	for _, m := range mgrs {
		if len(m.Pre) > 0 {
			_, _ = nftPkgRun(m.Bin, m.Pre...)
		}
		out, err := nftPkgRun(m.Bin, m.Args...)
		if err != nil {
			msg := strings.TrimSpace(string(out))
			if msg == "" {
				msg = err.Error()
			}
			last = fmt.Errorf("安装对时服务: %s", msg)
			continue
		}
		startTimeSyncUnit()
		return nil
	}
	if last == nil {
		last = fmt.Errorf("安装对时服务失败")
	}
	return last
}

func startTimeSyncUnit() {
	for _, unit := range []string{"systemd-timesyncd", "chronyd", "chrony"} {
		_ = exec.Command("systemctl", "enable", "--now", unit).Run()
	}
	_ = exec.Command("timedatectl", "set-ntp", "true").Run()
}
