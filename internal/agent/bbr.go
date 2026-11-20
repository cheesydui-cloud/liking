package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	bbrSysctlFile = "/etc/sysctl.d/99-liking-bbr.conf"
	bbrModuleFile = "/etc/modules-load.d/liking-tcp-bbr.conf"
	bbrPrevName   = "bbr-prev.json"
	bbrConfBody   = "net.core.default_qdisc = fq\nnet.ipv4.tcp_congestion_control = bbr\n"
	bbrModuleBody = "tcp_bbr\n"
	bbrKeyCC      = "net.ipv4.tcp_congestion_control"
	bbrKeyQdisc   = "net.core.default_qdisc"
	bbrDefaultCC  = "cubic"
	bbrDefaultQd  = "fq_codel"
)

type bbrPrev struct {
	Congestion string `json:"congestion"`
	Qdisc      string `json:"qdisc"`
}

type bbrResult struct {
	Congestion string
	Qdisc      string
	Changed    bool
	Assumed    bool
	Err        error
}

type bbrIO struct {
	read       func(key string) (string, error)
	write      func(key, value string) error
	available  func() (string, error)
	loadModule func() error
}

var (
	bbrGOOS      = runtime.GOOS
	bbrRead      = readProcSysctl
	bbrWrite     = writeProcSysctl
	bbrAvailable = readProcAvailable
	bbrLoad      = loadTCPBBR
)

func tuneBBR(profile, dir string) bbrResult {
	if bbrGOOS != "linux" {
		return bbrResult{Err: fmt.Errorf("需要 Linux")}
	}
	return tuneBBRAt(profile, filepath.Join(dir, bbrPrevName), bbrSysctlFile, bbrModuleFile, bbrIO{
		read: bbrRead, write: bbrWrite, available: bbrAvailable, loadModule: bbrLoad,
	})
}

// restoreOwnedBBR puts back the saved TCP settings when this agent wrote them.
// A machine that never had the liking file is left alone.
func restoreOwnedBBR(dir string) {
	if bbrGOOS != "linux" {
		return
	}
	if _, err := os.Stat(bbrSysctlFile); err != nil {
		if dir == "" {
			return
		}
		if _, err := os.Stat(filepath.Join(dir, bbrPrevName)); err != nil {
			return
		}
	}
	_ = tuneBBR("reset", dir)
}

func tuneBBRAt(profile, prevPath, sysctlPath, modulePath string, io bbrIO) bbrResult {
	profile = strings.TrimSpace(profile)
	switch profile {
	case "bbr":
		return enableBBR(prevPath, sysctlPath, modulePath, io)
	case "reset":
		return resetBBR(prevPath, sysctlPath, modulePath, io)
	default:
		return bbrResult{Err: fmt.Errorf("档位无效")}
	}
}

func enableBBR(prevPath, sysctlPath, modulePath string, io bbrIO) bbrResult {
	if err := ensureBBRAvailable(io); err != nil {
		return bbrResult{Err: err}
	}
	curCC, curQd, err := readTCP(io)
	if err != nil {
		return bbrResult{Err: err}
	}
	if _, ok := loadBBRPrev(prevPath); !ok {
		if err := saveBBRPrev(prevPath, bbrPrev{Congestion: curCC, Qdisc: curQd}); err != nil {
			return bbrResult{Congestion: curCC, Qdisc: curQd, Err: fmt.Errorf("保存原值失败: %v", err)}
		}
	}
	if err := writeTCP(io, "bbr", "fq"); err != nil {
		cc, qd, _ := readTCP(io)
		return bbrResult{Congestion: cc, Qdisc: qd, Err: err}
	}
	cc, qd, err := readTCP(io)
	if err != nil {
		return bbrResult{Err: err}
	}
	if cc != "bbr" || qd != "fq" {
		return bbrResult{Congestion: cc, Qdisc: qd, Err: fmt.Errorf("生效值是 %s / %s，和预期不符", cc, qd)}
	}
	if err := writeBBRFiles(sysctlPath, modulePath); err != nil {
		return bbrResult{Congestion: cc, Qdisc: qd, Changed: curCC != "bbr" || curQd != "fq", Err: fmt.Errorf("已生效，但没能写入开机配置: %v", err)}
	}
	return bbrResult{Congestion: cc, Qdisc: qd, Changed: curCC != "bbr" || curQd != "fq"}
}

func resetBBR(prevPath, sysctlPath, modulePath string, io bbrIO) bbrResult {
	prev, ok := loadBBRPrev(prevPath)
	assumed := !ok
	if !ok {
		prev = bbrPrev{Congestion: bbrDefaultCC, Qdisc: bbrDefaultQd}
	}
	curCC, curQd, _ := readTCP(io)
	if err := writeTCP(io, prev.Congestion, prev.Qdisc); err != nil {
		cc, qd, _ := readTCP(io)
		return bbrResult{Congestion: cc, Qdisc: qd, Assumed: assumed, Err: err}
	}
	cc, qd, err := readTCP(io)
	if err != nil {
		return bbrResult{Assumed: assumed, Err: err}
	}
	if cc != prev.Congestion || qd != prev.Qdisc {
		return bbrResult{Congestion: cc, Qdisc: qd, Assumed: assumed, Err: fmt.Errorf("生效值是 %s / %s，和预期不符", cc, qd)}
	}
	_ = os.Remove(sysctlPath)
	_ = os.Remove(modulePath)
	_ = os.Remove(prevPath)
	return bbrResult{
		Congestion: cc,
		Qdisc:      qd,
		Changed:    curCC != cc || curQd != qd,
		Assumed:    assumed,
	}
}

func ensureBBRAvailable(io bbrIO) error {
	if io.loadModule != nil {
		if err := io.loadModule(); err != nil {
			avail, aerr := io.available()
			if aerr != nil || !hasSysctlToken(avail, "bbr") {
				msg := strings.TrimSpace(err.Error())
				if msg == "" {
					msg = "加载 tcp_bbr 失败"
				}
				return fmt.Errorf("%s", msg)
			}
		}
	}
	if io.available == nil {
		return fmt.Errorf("内核不支持 bbr")
	}
	avail, err := io.available()
	if err != nil {
		return fmt.Errorf("读不到可用拥塞控制: %v", err)
	}
	if !hasSysctlToken(avail, "bbr") {
		return fmt.Errorf("内核不支持 bbr")
	}
	return nil
}

func readTCP(io bbrIO) (string, string, error) {
	cc, err := io.read(bbrKeyCC)
	if err != nil || strings.TrimSpace(cc) == "" {
		if err == nil {
			err = fmt.Errorf("空值")
		}
		return "", "", fmt.Errorf("读不到当前拥塞控制: %v", err)
	}
	qd, err := io.read(bbrKeyQdisc)
	if err != nil || strings.TrimSpace(qd) == "" {
		if err == nil {
			err = fmt.Errorf("空值")
		}
		return strings.TrimSpace(cc), "", fmt.Errorf("读不到当前队列: %v", err)
	}
	return strings.TrimSpace(cc), strings.TrimSpace(qd), nil
}

func writeTCP(io bbrIO, cc, qdisc string) error {
	if err := io.write(bbrKeyQdisc, qdisc); err != nil {
		return fmt.Errorf("设置队列失败: %v", err)
	}
	if err := io.write(bbrKeyCC, cc); err != nil {
		return fmt.Errorf("设置拥塞控制失败: %v", err)
	}
	return nil
}

func writeBBRFiles(sysctlPath, modulePath string) error {
	if err := writeFileMkdir(sysctlPath, []byte(bbrConfBody)); err != nil {
		return err
	}
	return writeFileMkdir(modulePath, []byte(bbrModuleBody))
}

func writeFileMkdir(path string, body []byte) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, body, 0644)
}

func loadBBRPrev(path string) (bbrPrev, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return bbrPrev{}, false
	}
	var p bbrPrev
	if json.Unmarshal(b, &p) != nil || strings.TrimSpace(p.Congestion) == "" || strings.TrimSpace(p.Qdisc) == "" {
		return bbrPrev{}, false
	}
	p.Congestion = strings.TrimSpace(p.Congestion)
	p.Qdisc = strings.TrimSpace(p.Qdisc)
	return p, true
}

func saveBBRPrev(path string, p bbrPrev) error {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
	}
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func hasSysctlToken(s, want string) bool {
	for _, p := range strings.Fields(s) {
		if p == want {
			return true
		}
	}
	return false
}

func procSysctlPath(key string) string {
	return "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
}

func readProcSysctl(key string) (string, error) {
	b, err := os.ReadFile(procSysctlPath(key))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(b)), nil
}

func writeProcSysctl(key, value string) error {
	return os.WriteFile(procSysctlPath(key), []byte(strings.TrimSpace(value)+"\n"), 0644)
}

func readProcAvailable() (string, error) {
	return readProcSysctl("net.ipv4.tcp_available_congestion_control")
}

func loadTCPBBR() error {
	out, err := exec.Command("modprobe", "tcp_bbr").CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("加载 tcp_bbr 失败: %s", msg)
	}
	return nil
}

func sampleTCP() (cc, qdisc string, ok bool) {
	if bbrGOOS != "linux" {
		return "", "", false
	}
	cc, qd, err := readTCP(bbrIO{read: bbrRead})
	if err != nil {
		return "", "", false
	}
	return cc, qd, true
}
