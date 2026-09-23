package agent

import "testing"

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
