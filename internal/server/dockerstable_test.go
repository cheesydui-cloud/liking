package server

import (
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagedDockerfileMatchesDeployFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "deploy", "docker", "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != managedDockerfile {
		t.Fatalf("deploy/docker/Dockerfile drifted from the embedded image")
	}
	for _, needle := range []string{
		managedDockerfileMarker,
		"FROM scratch",
		"ca-certificates.crt",
		"/var/lib/liking/panel.db",
		"--health",
		"http://127.0.0.1:8899/healthz",
	} {
		if !strings.Contains(managedDockerfile, needle) {
			t.Fatalf("missing %s", needle)
		}
	}
}

func TestEnsureManagedDockerfileLeavesCustomFile(t *testing.T) {
	dir := t.TempDir()
	custom := []byte("FROM scratch\nCOPY liking-server /usr/local/bin/liking-server\n")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), custom, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureManagedDockerfile(dir); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(custom) {
		t.Fatalf("custom Dockerfile overwritten: %s", got)
	}
	marked := []byte("# liking-managed-dockerfile\nFROM scratch\n")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), marked, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ensureManagedDockerfile(dir); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != managedDockerfile {
		t.Fatal("marked Dockerfile was not refreshed")
	}
}

func TestDockerContextWhitelist(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Dockerfile", "liking-server", "ca-certificates.crt", "bootstrap-password", "docker-compose.yml", "upgrade.log"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var buf strings.Builder
	if err := writeDockerContext(dir, &buf); err != nil {
		t.Fatal(err)
	}
	names, err := readTarNames(strings.NewReader(buf.String()))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(names, ",")
	for _, want := range []string{"Dockerfile", "liking-server", "ca-certificates.crt"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	for _, ban := range []string{"bootstrap-password", "docker-compose.yml", "upgrade.log"} {
		if strings.Contains(got, ban) {
			t.Fatalf("context included %s: %s", ban, got)
		}
	}
}

func TestRecreateKeepsLimitsAndLocalTrust(t *testing.T) {
	ins := sampleInspect()
	ins.Config.Entrypoint = []string{"/usr/local/bin/liking-server", "--addr", "127.0.0.1:8899", "--db", "/var/lib/liking/panel.db"}
	ins.HostConfig.CapDrop = []string{"ALL"}
	ins.HostConfig.SecurityOpt = []string{"no-new-privileges:true"}
	limit := int64(512)
	ins.HostConfig.PidsLimit = &limit
	ins.HostConfig.LogConfig.Type = "json-file"
	body, err := recreateCreateBody(ins, "liking:panel")
	if err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, body)
	if !strings.Contains(raw, `"CapDrop":["ALL"]`) || !strings.Contains(raw, "no-new-privileges:true") {
		t.Fatalf("limits dropped: %s", raw)
	}
	if !strings.Contains(raw, "LIKING_TRUST_DOCKER_GATEWAY=1") {
		t.Fatalf("local publish did not enable gateway trust: %s", raw)
	}
	if !strings.Contains(raw, "0.0.0.0:8899") || strings.Contains(raw, "127.0.0.1:8899") {
		t.Fatalf("listen addr: %s", raw)
	}
	ins.HostConfig.PortBindings = map[string][]portBinding{
		"8899/tcp": {{HostIP: "0.0.0.0", HostPort: "8899"}},
	}
	body, err = recreateCreateBody(ins, "liking:panel")
	if err != nil {
		t.Fatal(err)
	}
	raw = mustJSON(t, body)
	if strings.Contains(raw, "LIKING_TRUST_DOCKER_GATEWAY") {
		t.Fatalf("public publish trusted gateway: %s", raw)
	}
}

func TestRecreateDropsShellEntrypoint(t *testing.T) {
	ins := sampleInspect()
	ins.Config.Entrypoint = []string{"/bin/sh", "-c", "/usr/local/bin/liking-server --db /var/lib/liking/panel.db"}
	ins.Config.Cmd = nil
	body, err := recreateCreateBody(ins, "liking:panel")
	if err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, body)
	if strings.Contains(raw, "/bin/sh") {
		t.Fatalf("shell entrypoint kept: %s", raw)
	}
}

func TestParseProcRouteGateway(t *testing.T) {
	text := "Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT\n" +
		"eth0 00000000 010011AC 0003 0 0 0 00000000 0 0 0\n"
	got := parseProcRouteGateway(text)
	if got == nil || got.String() != "172.17.0.1" {
		t.Fatalf("%v", got)
	}
	if parseProcRouteGateway("Iface Destination Gateway\n") != nil {
		t.Fatal("empty route")
	}
}

func TestDockerGatewayTrustRewritesMatchingPeer(t *testing.T) {
	oldTrust := trustDockerGateway
	oldGW := defaultGateway
	t.Cleanup(func() {
		trustDockerGateway = oldTrust
		defaultGateway = oldGW
	})
	trustDockerGateway = func() bool { return true }
	defaultGateway = func() net.IP { return net.ParseIP("172.17.0.1") }

	var got string
	h := trustedForwarded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
		if !isSecureRequest(r) {
			t.Fatal("https proto ignored")
		}
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.17.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("X-Forwarded-Proto", "https")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(got, "203.0.113.9:") {
		t.Fatalf("gateway peer: %s", got)
	}

	trustDockerGateway = func() bool { return false }
	got = ""
	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "172.17.0.1:1234"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	h = trustedForwarded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
		w.WriteHeader(http.StatusOK)
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(got, "172.17.0.1:") {
		t.Fatalf("trust off still rewrote: %s", got)
	}
}

func TestCheckHealthURL(t *testing.T) {
	for _, raw := range []string{
		"http://example.com/healthz",
		"https://127.0.0.1/healthz",
		"http://127.0.0.1/other",
		"http://127.0.0.1:8899/healthz?x=1",
	} {
		if err := CheckHealthURL(raw); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.NotFound(w, r)
	})}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	if err := CheckHealthURL("http://" + ln.Addr().String() + "/healthz"); err != nil {
		t.Fatal(err)
	}
}

func TestFindAgentBinaryInDockerDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LIKING_AGENT_DIR", dir)
	name := filepath.Join(dir, "liking-agent-linux-amd64")
	buf := make([]byte, 1<<20+8)
	if err := os.WriteFile(name, buf, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := findAgentBinary("linux", "amd64"); got != name {
		t.Fatalf("got %q", got)
	}
	t.Setenv("LIKING_AGENT_DIR", "relative/../x")
	if got := findAgentBinary("linux", "amd64"); got == name {
		t.Fatal("relative dir accepted")
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
