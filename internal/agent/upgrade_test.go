package agent

import (
	"testing"

	"liking/internal/wsproto"
)

func TestCheckUpgradeURLHost(t *testing.T) {
	a := &Agent{cfg: Config{ConnectURL: "wss://liking.example/v1/agents"}}
	if err := a.checkUpgradeURL("https://evil.example/v1/agent-bin"); err == nil {
		t.Fatal("foreign host")
	}
	if err := a.checkUpgradeURL("https://liking.example/v1/agent-bin?os=linux&arch=amd64"); err != nil {
		t.Fatal(err)
	}
	if err := a.checkUpgradeURL("file:///tmp/agent"); err == nil {
		t.Fatal("file scheme")
	}
	if err := a.checkUpgradeURL("http://liking.example/v1/agent-bin"); err == nil {
		t.Fatal("plain http without insecure")
	}
	a.cfg.Insecure = true
	if err := a.checkUpgradeURL("http://liking.example/v1/agent-bin"); err != nil {
		t.Fatal(err)
	}
}

func TestRelativeUpgradeURLUsesConnectHost(t *testing.T) {
	a := &Agent{cfg: Config{ConnectURL: "wss://liking.example/v1/agents"}}
	src, err := a.resolveURL("/v1/agent-bin?arch=amd64&os=linux")
	if err != nil {
		t.Fatal(err)
	}
	if src != "https://liking.example/v1/agent-bin?arch=amd64&os=linux" {
		t.Fatalf("src %s", src)
	}
	if err := a.checkUpgradeURL(src); err != nil {
		t.Fatal(err)
	}

	a = &Agent{cfg: Config{ConnectURL: "ws://10.0.0.8:8899/v1/agents", Insecure: true}}
	src, err = a.resolveURL("/v1/agent-bin?arch=amd64&os=linux")
	if err != nil {
		t.Fatal(err)
	}
	if src != "http://10.0.0.8:8899/v1/agent-bin?arch=amd64&os=linux" {
		t.Fatalf("src %s", src)
	}
	if err := a.checkUpgradeURL(src); err != nil {
		t.Fatal(err)
	}
}

func TestUpgradeSourcesFallback(t *testing.T) {
	a := &Agent{cfg: Config{ConnectURL: "wss://liking.example/v1/agents"}}
	req := wsproto.Upgrade{
		URL:         "/v1/agent-bin?os=linux&arch=amd64",
		FallbackURL: "https://github.com/cheesydui-cloud/liking/releases/download/v0.3.44/liking-agent-linux-amd64",
	}
	src, err := a.upgradeSources(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(src) != 2 || src[1] != req.FallbackURL {
		t.Fatalf("direct fallback %+v", src)
	}
	t.Setenv("LIKING_GITHUB_PROXY", "https://gh-proxy.example")
	src, err = a.upgradeSources(req)
	if err != nil {
		t.Fatal(err)
	}
	want := "https://gh-proxy.example/https://github.com/cheesydui-cloud/liking/releases/download/v0.3.44/liking-agent-linux-amd64"
	if len(src) != 2 || src[0] != "https://liking.example/v1/agent-bin?os=linux&arch=amd64" || src[1] != want {
		t.Fatalf("proxied %+v", src)
	}
	req.FallbackURL = "https://evil.example/agent"
	src, err = a.upgradeSources(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(src) != 1 {
		t.Fatalf("evil fallback kept %+v", src)
	}
	if err := checkAgentFallbackURL("https://github.com/cheesydui-cloud/liking/releases/download/v0.3.44/liking-agent-linux-amd64?x=1"); err == nil {
		t.Fatal("query allowed")
	}
	if !fallbackRedirectHostOK("objects.githubusercontent.com", "github.com") {
		t.Fatal("github asset host")
	}
	if fallbackRedirectHostOK("evil.example", "gh-proxy.example") {
		t.Fatal("foreign redirect")
	}
}
