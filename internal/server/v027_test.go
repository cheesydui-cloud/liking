package server

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"testing"
)

func TestBillBytesRejectsOverflow(t *testing.T) {
	if _, ok := billBytes(-1, 1); ok {
		t.Fatal("negative")
	}
	if _, ok := billBytes(1<<41, 1); ok {
		t.Fatal("huge sample")
	}
	if _, ok := billBytes(math.MaxInt64, 2); ok {
		t.Fatal("overflow product")
	}
	got, ok := billBytes(100, 1.5)
	if !ok || got != 150 {
		t.Fatalf("got %d %v", got, ok)
	}
}

func TestAdminPasswordEndpointRefused(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	res, err := c.Post(ts.URL+"/api/users/1/password", "application/json", bytes.NewReader([]byte(`{"password":"newpass1"}`)))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
}

func TestBackupDownloadNeedsLoginPassword(t *testing.T) {
	d, ts, c := setupAdminPanel(t)
	if err := putJSON(t, c, ts.URL+"/api/settings", map[string]any{
		"backup_password":  "backup-pw",
		"current_password": "secret12",
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"password": "backup-pw"})
	res, err := c.Post(ts.URL+"/api/backup", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("backup password must not authorize download: %d %s", res.StatusCode, b)
	}
	body, _ = json.Marshal(map[string]string{"password": "secret12"})
	res, err = c.Post(ts.URL+"/api/backup", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		b, _ = io.ReadAll(res.Body)
		t.Fatalf("login password download: %d %s", res.StatusCode, b)
	}
	_ = d
}

func TestSettingsBackupPasswordRequiresCurrent(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/settings", bytes.NewReader([]byte(`{"backup_password":"backup-pw"}`)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("missing current password: %d %s", res.StatusCode, b)
	}
}

func putJSON(t *testing.T, c *http.Client, url string, v any) error {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("PUT %s %d %s", url, res.StatusCode, b)
	}
	return nil
}
