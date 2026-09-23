package server

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestAgentBinDownloadURLRelative(t *testing.T) {
	raw := agentBinDownloadURL("linux", "amd64")
	if raw == "" || raw[0] != '/' {
		t.Fatalf("want relative path, got %q", raw)
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Host != "" {
		t.Fatalf("host %q", u.Host)
	}
	if u.Path != "/v1/agent-bin" {
		t.Fatalf("path %q", u.Path)
	}
	if u.Query().Get("os") != "linux" || u.Query().Get("arch") != "amd64" {
		t.Fatalf("query %s", u.RawQuery)
	}
}

func TestBytesContainVersion(t *testing.T) {
	blob := []byte("prefix 0.2.21 tail v0.2.22+dirty and 0.2.220")
	if !bytesContainVersion(blob, "0.2.21") {
		t.Fatal("0.2.21")
	}
	if !bytesContainVersion(blob, "0.2.22") {
		t.Fatal("0.2.22 inside v0.2.22+dirty")
	}
	if bytesContainVersion(blob, "0.2.2") {
		t.Fatal("0.2.2 must not match 0.2.21 or 0.2.22")
	}
	if bytesContainVersion(blob, "0.2.23") {
		t.Fatal("missing")
	}
}

func TestAgentBinaryHasVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "liking-agent-linux-amd64")
	if err := os.WriteFile(path, []byte("elf 0.2.22 leftover"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok, err := agentBinaryHasVersion(path, "0.2.22")
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	ok, err = agentBinaryHasVersion(path, "0.2.23")
	if err != nil || ok {
		t.Fatalf("stale ok=%v err=%v", ok, err)
	}
}
