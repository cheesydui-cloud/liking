package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"liking/internal/version"
)

func TestPanelUpgradeArgsStopSystemdParsing(t *testing.T) {
	args := panelUpgradeArgs("/usr/local/sbin/liking-upgrade", "v0.2.27")
	dash := -1
	for i, a := range args {
		if a == "--" {
			dash = i
		}
	}
	if dash < 0 || dash+1 >= len(args) || args[dash+1] != "/usr/local/sbin/liking-upgrade" {
		t.Fatalf("%v", args)
	}
	if args[len(args)-2] != "--release" || args[len(args)-1] != "v0.2.27" {
		t.Fatalf("%v", args)
	}
	plain := panelUpgradeArgs("/usr/local/sbin/liking-upgrade", "")
	if plain[len(plain)-1] != "/usr/local/sbin/liking-upgrade" {
		t.Fatalf("%v", plain)
	}
}

func TestPanelUpgradeRequiresPassword(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	res, err := c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty %d %s", res.StatusCode, b)
	}

	body, _ := json.Marshal(map[string]string{"password": "nope"})
	res, err = c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("wrong %d %s", res.StatusCode, b)
	}
}

func TestPanelUpgradeRejectsBadRelease(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	oldScript := panelUpgradeScript
	oldStart := startPanelUpgrade
	t.Cleanup(func() {
		panelUpgradeScript = oldScript
		startPanelUpgrade = oldStart
	})
	panelUpgradeScript = func() string { return "/usr/local/sbin/liking-upgrade" }
	startPanelUpgrade = func(script, release string) error {
		t.Fatal("should not start")
		return nil
	}
	body, _ := json.Marshal(map[string]string{"password": "secret12", "release": "v0.2.26;rm"})
	res, err := c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad release %d %s", res.StatusCode, b)
	}
}

func TestPanelUpgradeStartsScript(t *testing.T) {
	d, ts, c := setupAdminPanel(t)
	oldScript := panelUpgradeScript
	oldStart := startPanelUpgrade
	t.Cleanup(func() {
		panelUpgradeScript = oldScript
		startPanelUpgrade = oldStart
	})
	var gotScript, gotRelease string
	panelUpgradeScript = func() string { return "/usr/local/sbin/liking-upgrade" }
	startPanelUpgrade = func(script, release string) error {
		gotScript, gotRelease = script, release
		return nil
	}
	body, _ := json.Marshal(map[string]string{"password": "secret12", "release": "v0.2.27"})
	res, err := c.Post(ts.URL+"/api/panel/upgrade", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("start %d %s", res.StatusCode, b)
	}
	if gotScript != "/usr/local/sbin/liking-upgrade" || gotRelease != "v0.2.27" {
		t.Fatalf("started %s %s", gotScript, gotRelease)
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

func TestPanelUpgradeInfo(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	oldLatest := fetchPanelLatest
	oldScript := panelUpgradeScript
	t.Cleanup(func() {
		fetchPanelLatest = oldLatest
		panelUpgradeScript = oldScript
	})
	fetchPanelLatest = func() (string, error) { return "v9.9.9", nil }
	panelUpgradeScript = func() string { return "/usr/local/sbin/liking-upgrade" }
	res, err := c.Get(ts.URL + "/api/panel/upgrade")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Version string `json:"version"`
		Started int64  `json:"started"`
		Latest  string `json:"latest"`
		Update  bool   `json:"update"`
		Script  bool   `json:"script"`
	}
	decodeRes(t, res, &out)
	if out.Version != version.Version || out.Latest != "9.9.9" || !out.Update || !out.Script || out.Started == 0 {
		t.Fatalf("%+v", out)
	}
}
