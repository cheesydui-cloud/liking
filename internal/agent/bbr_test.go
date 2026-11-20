package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnableBBRWritesFilesAndKeepsPrev(t *testing.T) {
	dir := t.TempDir()
	prev := filepath.Join(dir, bbrPrevName)
	sysctlPath := filepath.Join(dir, "sysctl.conf")
	modulePath := filepath.Join(dir, "modules.conf")
	st := map[string]string{
		bbrKeyCC:    "cubic",
		bbrKeyQdisc: "fq_codel",
		"available": "reno cubic bbr",
	}
	io := fakeBBR(st)
	got := tuneBBRAt("bbr", prev, sysctlPath, modulePath, io)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if !got.Changed || got.Congestion != "bbr" || got.Qdisc != "fq" {
		t.Fatalf("%+v", got)
	}
	body, err := os.ReadFile(sysctlPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != bbrConfBody {
		t.Fatalf("conf %q", body)
	}
	mod, err := os.ReadFile(modulePath)
	if err != nil || string(mod) != bbrModuleBody {
		t.Fatalf("module %q %v", mod, err)
	}
	saved, ok := loadBBRPrev(prev)
	if !ok || saved.Congestion != "cubic" || saved.Qdisc != "fq_codel" {
		t.Fatalf("prev %+v %v", saved, ok)
	}
	st[bbrKeyCC] = "reno"
	again := tuneBBRAt("bbr", prev, sysctlPath, modulePath, io)
	if again.Err != nil {
		t.Fatal(again.Err)
	}
	saved, ok = loadBBRPrev(prev)
	if !ok || saved.Congestion != "cubic" || saved.Qdisc != "fq_codel" {
		t.Fatalf("prev overwritten %+v", saved)
	}
}

func TestResetBBRRestoresSaved(t *testing.T) {
	dir := t.TempDir()
	prev := filepath.Join(dir, bbrPrevName)
	sysctlPath := filepath.Join(dir, "sysctl.conf")
	modulePath := filepath.Join(dir, "modules.conf")
	st := map[string]string{
		bbrKeyCC:    "cubic",
		bbrKeyQdisc: "pfifo_fast",
		"available": "cubic bbr",
	}
	io := fakeBBR(st)
	if got := tuneBBRAt("bbr", prev, sysctlPath, modulePath, io); got.Err != nil {
		t.Fatal(got.Err)
	}
	got := tuneBBRAt("reset", prev, sysctlPath, modulePath, io)
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if got.Assumed || got.Congestion != "cubic" || got.Qdisc != "pfifo_fast" || !got.Changed {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(sysctlPath); !os.IsNotExist(err) {
		t.Fatal("sysctl file remains")
	}
	if _, err := os.Stat(modulePath); !os.IsNotExist(err) {
		t.Fatal("module file remains")
	}
	if _, err := os.Stat(prev); !os.IsNotExist(err) {
		t.Fatal("prev remains")
	}
}

func TestResetBBRWithoutPrevUsesDistroDefault(t *testing.T) {
	dir := t.TempDir()
	st := map[string]string{bbrKeyCC: "bbr", bbrKeyQdisc: "fq", "available": "bbr"}
	got := tuneBBRAt("reset", filepath.Join(dir, bbrPrevName), filepath.Join(dir, "s.conf"), filepath.Join(dir, "m.conf"), fakeBBR(st))
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if !got.Assumed || got.Congestion != bbrDefaultCC || got.Qdisc != bbrDefaultQd {
		t.Fatalf("%+v", got)
	}
}

func TestEnableBBRRefusesUnsupportedKernel(t *testing.T) {
	dir := t.TempDir()
	sysctlPath := filepath.Join(dir, "sysctl.conf")
	st := map[string]string{bbrKeyCC: "cubic", bbrKeyQdisc: "fq_codel", "available": "reno cubic"}
	io := fakeBBR(st)
	io.loadModule = func() error { return bbrErr("加载 tcp_bbr 失败: not found") }
	got := tuneBBRAt("bbr", filepath.Join(dir, bbrPrevName), sysctlPath, filepath.Join(dir, "m.conf"), io)
	if got.Err == nil || !strings.Contains(got.Err.Error(), "tcp_bbr") {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(sysctlPath); !os.IsNotExist(err) {
		t.Fatal("wrote conf without bbr")
	}
	if _, err := os.Stat(filepath.Join(dir, bbrPrevName)); !os.IsNotExist(err) {
		t.Fatal("saved prev before support check")
	}
}

func TestEnableBBRReadbackMismatch(t *testing.T) {
	dir := t.TempDir()
	sysctlPath := filepath.Join(dir, "sysctl.conf")
	st := map[string]string{bbrKeyCC: "cubic", bbrKeyQdisc: "fq_codel", "available": "bbr"}
	io := fakeBBR(st)
	io.write = func(key, value string) error { return nil }
	got := tuneBBRAt("bbr", filepath.Join(dir, bbrPrevName), sysctlPath, filepath.Join(dir, "m.conf"), io)
	if got.Err == nil || !strings.Contains(got.Err.Error(), "和预期不符") {
		t.Fatalf("%+v", got)
	}
	if _, err := os.Stat(sysctlPath); !os.IsNotExist(err) {
		t.Fatal("wrote conf after mismatch")
	}
}

func TestTuneBBRRejectsUnknownProfile(t *testing.T) {
	got := tuneBBRAt("high", "p", "s", "m", fakeBBR(nil))
	if got.Err == nil {
		t.Fatal("accepted unknown profile")
	}
}

type bbrErr string

func (e bbrErr) Error() string { return string(e) }

func fakeBBR(st map[string]string) bbrIO {
	if st == nil {
		st = map[string]string{}
	}
	return bbrIO{
		read: func(key string) (string, error) {
			return st[key], nil
		},
		write: func(key, value string) error {
			st[key] = value
			return nil
		},
		available: func() (string, error) {
			return st["available"], nil
		},
		loadModule: func() error { return nil },
	}
}
