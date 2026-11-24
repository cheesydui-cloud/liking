package server

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const sampleContainerID = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestPanelBinarySkipsShellEntrypoint(t *testing.T) {
	ins := sampleInspect()
	ins.Config.Entrypoint = []string{"/bin/sh", "-c", "liking-server --addr 0.0.0.0:8899 --db /var/lib/liking/panel.db"}
	if panelBinary(ins) != "/usr/local/bin/liking-server" {
		t.Fatal(panelBinary(ins))
	}
	ins.Config.Entrypoint = []string{"/usr/bin/liking-server", "--db", "/var/lib/liking/panel.db"}
	if panelBinary(ins) != "/usr/bin/liking-server" {
		t.Fatal(panelBinary(ins))
	}
}

func TestContainerIDFromProcText(t *testing.T) {
	id := sampleContainerID
	samples := []string{
		"12:memory:/docker/" + id + "\n",
		"0::/system.slice/docker-" + id + ".scope\n",
		"100 80 8:1 /var/lib/docker/containers/" + id + "/resolv.conf /etc/resolv.conf ro - ext4 /dev/sda1 rw\n",
	}
	for _, s := range samples {
		if got := containerIDFromText(s); got != id {
			t.Fatalf("%q -> %q", s, got)
		}
	}
	if containerIDFromText("0::/\n") != "" {
		t.Fatal("empty cgroup")
	}
}

func TestReleaseURLRejectsInjection(t *testing.T) {
	if _, err := officialAssetBase("v0.3.52;rm"); err == nil {
		t.Fatal("tag")
	}
	if _, err := officialAssetBase("latest"); err == nil {
		t.Fatal("latest")
	}
	base, err := officialAssetBase("v0.3.52")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := releaseAssetURL(base, "../SHA256SUMS"); err == nil {
		t.Fatal("asset")
	}
	if _, err := releaseAssetURL("https://evil.example/releases", "SHA256SUMS"); err == nil {
		t.Fatal("host")
	}
	if _, err := withProxy("http://gh-proxy.com/", base); err == nil {
		t.Fatal("http proxy")
	}
	joined, err := withProxy("https://gh-proxy.com/", base)
	if err != nil || !strings.HasPrefix(joined, "https://gh-proxy.com/https://github.com/") {
		t.Fatalf("%s %v", joined, err)
	}
}

func TestExpectedSHA256AndMismatch(t *testing.T) {
	sums := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa  liking-server\nbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb *liking-agent\n")
	got, err := expectedSHA256(sums, "liking-server")
	if err != nil || got != strings.Repeat("a", 64) {
		t.Fatalf("%s %v", got, err)
	}
	got, err = expectedSHA256(sums, "liking-agent")
	if err != nil || got != strings.Repeat("b", 64) {
		t.Fatalf("star %s %v", got, err)
	}
	if _, err := expectedSHA256(sums, "missing"); err == nil {
		t.Fatal("missing asset")
	}
	if _, err := expectedSHA256(sums, "../x"); err == nil {
		t.Fatal("bad asset")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sha256Equal(strings.Repeat("a", 64), path); err == nil {
		t.Fatal("mismatch accepted")
	}
}

func TestCheckReleaseBinaryRejectsSmallAndNonELF(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small")
	if err := os.WriteFile(small, []byte{0x7f, 'E', 'L', 'F'}, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseBinary(small); err == nil {
		t.Fatal("small accepted")
	}
	buf := make([]byte, minReleaseBytes+1)
	buf[0], buf[1], buf[2], buf[3] = 0x7f, 'E', 'L', 'F'
	elf := filepath.Join(dir, "elf")
	if err := os.WriteFile(elf, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseBinary(elf); err != nil {
		t.Fatal(err)
	}
	buf[0] = 0
	raw := filepath.Join(dir, "raw")
	if err := os.WriteFile(raw, buf, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := checkReleaseBinary(raw); err == nil {
		t.Fatal("non-elf accepted")
	}
}

func TestDockerContextSkipsLog(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "upgrade.log"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liking-server.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeDockerContext(dir, &buf); err != nil {
		t.Fatal(err)
	}
	names, err := readTarNames(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 1 || names[0] != "Dockerfile" {
		t.Fatalf("%v", names)
	}
}

func TestRecreateBodyKeepsBindsAndLocalPort(t *testing.T) {
	body, err := recreateCreateBody(sampleInspect(), "liking:panel")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(body)
	var out struct {
		Image      string   `json:"Image"`
		Hostname   string   `json:"Hostname"`
		Entrypoint []string `json:"Entrypoint"`
		Labels     map[string]string
		HostConfig struct {
			Binds         []string                 `json:"Binds"`
			NetworkMode   string                   `json:"NetworkMode"`
			AutoRemove    bool                     `json:"AutoRemove"`
			RestartPolicy struct{ Name string }    `json:"RestartPolicy"`
			PortBindings  map[string][]portBinding `json:"PortBindings"`
		} `json:"HostConfig"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Image != "liking:panel" || out.Hostname != "" {
		t.Fatalf("image/host %+v", out)
	}
	if !strings.Contains(strings.Join(out.Entrypoint, " "), "--db /var/lib/liking/panel.db") {
		t.Fatalf("entrypoint %v", out.Entrypoint)
	}
	if out.Labels["com.docker.compose.service"] != "liking" {
		t.Fatalf("labels %v", out.Labels)
	}
	if out.HostConfig.NetworkMode != "liking-docker_default" || out.HostConfig.RestartPolicy.Name != "unless-stopped" || out.HostConfig.AutoRemove {
		t.Fatalf("host %+v", out.HostConfig)
	}
	joined := strings.Join(out.HostConfig.Binds, ",")
	for _, want := range []string{"/var/lib/liking:/var/lib/liking", "/var/run/docker.sock:/var/run/docker.sock", "/opt/liking-docker:/opt/liking-docker"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("binds %s", joined)
		}
	}
	ports := out.HostConfig.PortBindings["8899/tcp"]
	if len(ports) != 1 || ports[0].HostIP != "127.0.0.1" || ports[0].HostPort != "8899" {
		t.Fatalf("ports %+v", ports)
	}
}

func TestRecreateBodyRejectsMissingData(t *testing.T) {
	ins := sampleInspect()
	ins.HostConfig.Binds = []string{"/var/run/docker.sock:/var/run/docker.sock"}
	if _, err := recreateCreateBody(ins, "liking:panel"); err == nil {
		t.Fatal("missing data bind")
	}
	ins = sampleInspect()
	ins.Config.Entrypoint = []string{"/usr/local/bin/liking-server"}
	ins.Config.Cmd = nil
	if _, err := recreateCreateBody(ins, "liking:panel"); err == nil {
		t.Fatal("missing db arg")
	}
	ins = sampleInspect()
	ins.Config.Hostname = "panel"
	body, err := recreateCreateBody(ins, "liking:panel")
	if err != nil {
		t.Fatal(err)
	}
	if body["Hostname"] != "panel" {
		t.Fatalf("hostname %v", body["Hostname"])
	}
}

func TestRecreateDoesNotDeleteRunningContainerOnCreateFailure(t *testing.T) {
	ins := sampleInspect()
	var calls []string
	api := scriptedDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.Contains(r.URL.Path, "/liking-previous/json") {
			http.Error(w, `{"message":"no such container"}`, http.StatusNotFound)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/create") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"create failed"}`))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	err := recreateContainer(context.Background(), api, ins, "liking:panel", &upgradeLogger{})
	if err == nil {
		t.Fatal("expected create failure")
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "DELETE") {
		t.Fatalf("deleted during rollback:\n%s", joined)
	}
	if !strings.Contains(joined, "/stop?t=20") {
		t.Fatalf("no stop:\n%s", joined)
	}
	if !strings.Contains(joined, "rename?name=liking-previous") || !strings.Contains(joined, "rename?name=liking") {
		t.Fatalf("rename rollback:\n%s", joined)
	}
	if !strings.Contains(joined, "/"+sampleContainerID+"/start") {
		t.Fatalf("old container not started:\n%s", joined)
	}
}

func TestRecreateKeepsVolumesFlagOff(t *testing.T) {
	ins := sampleInspect()
	var calls []string
	var created []byte
	api := scriptedDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		body, _ := io.ReadAll(r.Body)
		if strings.HasSuffix(r.URL.Path, "/create") {
			created = body
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"Id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`))
			return
		}
		if strings.Contains(r.URL.Path, "/liking-previous/json") {
			http.Error(w, `{"message":"no such container"}`, http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	if err := recreateContainer(context.Background(), api, ins, "liking:panel", &upgradeLogger{}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "DELETE /v1.41/containers/liking-previous?force=1&v=0") {
		t.Fatalf("remove:\n%s", joined)
	}
	if strings.Contains(joined, "v=1") {
		t.Fatalf("volume delete:\n%s", joined)
	}
	if !strings.Contains(string(created), "127.0.0.1") || !strings.Contains(string(created), "/var/lib/liking") {
		t.Fatalf("create body %s", created)
	}
}

func TestDockerUpgradeFailureDoesNotStopContainer(t *testing.T) {
	oldStage := stageReleaseFn
	oldSleep := sleepBeforeDockerStop
	t.Cleanup(func() {
		stageReleaseFn = oldStage
		sleepBeforeDockerStop = oldSleep
	})
	stageReleaseFn = func(context.Context, string, string, string, *upgradeLogger) error {
		return errString("checksum")
	}
	sleepBeforeDockerStop = func() { t.Fatal("slept before stop") }
	var calls []string
	api := scriptedDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		if strings.HasSuffix(r.URL.Path, "/json") {
			_ = json.NewEncoder(w).Encode(sampleInspect())
			return
		}
		t.Fatalf("unexpected %s", r.URL.Path)
	})
	err := runDockerUpgrade(context.Background(), sampleContainerID, "v0.3.52", t.TempDir(), "amd64", &upgradeLogger{}, api)
	if err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("%v", err)
	}
	for _, c := range calls {
		if strings.Contains(c, "/stop") || strings.Contains(c, "/create") {
			t.Fatalf("mutated container: %v", calls)
		}
	}
}

func TestDockerUpgradeBuildFailureDoesNotStopContainer(t *testing.T) {
	oldStage := stageReleaseFn
	oldSleep := sleepBeforeDockerStop
	t.Cleanup(func() {
		stageReleaseFn = oldStage
		sleepBeforeDockerStop = oldSleep
	})
	stageReleaseFn = func(context.Context, string, string, string, *upgradeLogger) error { return nil }
	sleepBeforeDockerStop = func() { t.Fatal("slept before stop") }
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\nCOPY liking-server /liking-server\nCOPY ca-certificates.crt /ca-certificates.crt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca-certificates.crt"), []byte("cert"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "liking-server"), []byte{0x7f, 'E', 'L', 'F'}, 0o755); err != nil {
		t.Fatal(err)
	}
	var calls []string
	api := scriptedDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		_, _ = io.Copy(io.Discard, r.Body)
		if strings.HasSuffix(r.URL.Path, "/json") {
			_ = json.NewEncoder(w).Encode(sampleInspect())
			return
		}
		if strings.Contains(r.URL.Path, "/build") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"message":"build failed"}`))
			return
		}
		t.Fatalf("unexpected %s", r.URL.Path)
	})
	err := runDockerUpgrade(context.Background(), sampleContainerID, "v0.3.52", dir, "amd64", &upgradeLogger{}, api)
	if err == nil || !strings.Contains(err.Error(), "构建失败") {
		t.Fatalf("%v", err)
	}
	for _, c := range calls {
		if strings.Contains(c, "/stop") {
			t.Fatalf("stopped after build failure: %v", calls)
		}
	}
}

func TestPanelUpgradeStartsDocker(t *testing.T) {
	d, ts, c := setupAdminPanel(t)
	restoreUpgradeHooks(t)
	var got string
	panelUpgradeScript = func() string { return "" }
	dockerUpgradeReadyFn = func() bool { return true }
	panelInContainerFn = func() bool { return true }
	startDockerUpgradeFn = func(release string) error {
		got = release
		return nil
	}
	body, _ := json.Marshal(map[string]string{"password": "secret12", "release": "v0.3.52"})
	res, err := c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start %d %s", res.StatusCode, b)
	}
	if got != "v0.3.52" {
		t.Fatalf("release %s", got)
	}
	rows, err := d.Query(`SELECT action FROM audit WHERE action='panel.upgrade'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("no audit")
	}
}

func TestPanelUpgradePrefersSystemd(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	restoreUpgradeHooks(t)
	panelUpgradeScript = func() string { return "/usr/local/sbin/liking-upgrade" }
	dockerUpgradeReadyFn = func() bool { return true }
	startPanelUpgrade = func(script, release string) error { return nil }
	startDockerUpgradeFn = func(string) error {
		t.Fatal("docker path used")
		return nil
	}
	body, _ := json.Marshal(map[string]string{"password": "secret12", "release": "v0.3.52"})
	res, err := c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start %d %s", res.StatusCode, b)
	}
}

func TestPanelUpgradeContainerNeedsMounts(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	restoreUpgradeHooks(t)
	panelUpgradeScript = func() string { return "" }
	dockerUpgradeReadyFn = func() bool { return false }
	panelInContainerFn = func() bool { return true }
	startDockerUpgradeFn = func(string) error {
		t.Fatal("docker path used")
		return nil
	}
	body, _ := json.Marshal(map[string]string{"password": "secret12"})
	res, err := c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest || !strings.Contains(string(b), "/var/run/docker.sock") {
		t.Fatalf("mounts %d %s", res.StatusCode, b)
	}
}

func TestPanelUpgradeInfoDocker(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	restoreUpgradeHooks(t)
	oldLatest := fetchPanelLatest
	t.Cleanup(func() { fetchPanelLatest = oldLatest })
	fetchPanelLatest = func() (string, error) { return "v9.9.9", nil }
	panelUpgradeScript = func() string { return "" }
	dockerUpgradeReadyFn = func() bool { return true }
	res, err := c.Get(ts.URL + "/api/panel/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Script bool   `json:"script"`
		Mode   string `json:"mode"`
	}
	decodeRes(t, res, &out)
	if !out.Script || out.Mode != "docker" {
		t.Fatalf("%+v", out)
	}
}

func restoreUpgradeHooks(t *testing.T) {
	t.Helper()
	oldScript := panelUpgradeScript
	oldStart := startPanelUpgrade
	oldDocker := startDockerUpgradeFn
	oldReady := dockerUpgradeReadyFn
	oldIn := panelInContainerFn
	t.Cleanup(func() {
		panelUpgradeScript = oldScript
		startPanelUpgrade = oldStart
		startDockerUpgradeFn = oldDocker
		dockerUpgradeReadyFn = oldReady
		panelInContainerFn = oldIn
	})
}

type errString string

func (e errString) Error() string { return string(e) }

func scriptedDocker(t *testing.T, fn http.HandlerFunc) *dockerAPI {
	t.Helper()
	srv := httptest.NewServer(fn)
	t.Cleanup(srv.Close)
	return &dockerAPI{base: srv.URL, do: srv.Client().Do}
}

func sampleInspect() dockerInspect {
	var ins dockerInspect
	ins.ID = sampleContainerID
	ins.Name = "/liking"
	ins.Image = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ins.Config.Hostname = "0123456789ab"
	ins.Config.Image = "liking:panel"
	ins.Config.Entrypoint = []string{"/usr/local/bin/liking-server", "--addr", "0.0.0.0:8899", "--db", "/var/lib/liking/panel.db"}
	ins.Config.Labels = map[string]string{"com.docker.compose.service": "liking"}
	ins.Config.ExposedPorts = map[string]struct{}{"8899/tcp": {}}
	ins.HostConfig.Binds = []string{
		"/var/lib/liking:/var/lib/liking",
		"/var/run/docker.sock:/var/run/docker.sock",
		"/opt/liking-docker:/opt/liking-docker",
	}
	ins.HostConfig.PortBindings = map[string][]portBinding{
		"8899/tcp": {{HostIP: "127.0.0.1", HostPort: "8899"}},
	}
	ins.HostConfig.RestartPolicy.Name = "unless-stopped"
	ins.HostConfig.NetworkMode = "liking-docker_default"
	return ins
}

func readTarNames(r io.Reader) ([]string, error) {
	tr := tar.NewReader(r)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return names, nil
		}
		if err != nil {
			return nil, err
		}
		names = append(names, hdr.Name)
	}
}
