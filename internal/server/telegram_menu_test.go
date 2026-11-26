package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"liking/internal/db"
)

func TestUserPushSkipsNodeStatus(t *testing.T) {
	srv, cap, _ := newTGPanel(t)
	alice, err := db.CreateUser(srv.DB, "alice", "x", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE users SET enabled=1, expires_at=? WHERE id=?`, time.Now().Add(48*time.Hour).Unix(), alice.ID); err != nil {
		t.Fatal(err)
	}
	if err := db.TGBind(srv.DB, alice.ID, 200); err != nil {
		t.Fatal(err)
	}
	edge, err := db.CreateServer(srv.DB, "edge", "203.0.113.21", "edge-tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE servers SET online=0, last_seen=? WHERE id=?`, time.Now().Unix(), edge.ID); err != nil {
		t.Fatal(err)
	}
	srv.tgScanServers()
	cap.mu.Lock()
	for _, raw := range cap.calls {
		if strings.Contains(raw, "sendMessage") && strings.Contains(raw, `"chat_id":200`) && (strings.Contains(raw, "离线") || strings.Contains(raw, "203.0.113.21")) {
			cap.mu.Unlock()
			t.Fatalf("server status reached the user %s", raw)
		}
	}
	cap.mu.Unlock()

	if err := db.SetSetting(srv.DB, "tg_tpl_expire_soon", "{user} 自定义到期 {days}"); err != nil {
		t.Fatal(err)
	}
	srv.tgScanUsers()
	userMsg := ""
	cap.mu.Lock()
	for _, raw := range cap.calls {
		if strings.Contains(raw, "sendMessage") && strings.Contains(raw, `"chat_id":200`) && strings.Contains(raw, "自定义到期") {
			userMsg = raw
		}
	}
	cap.mu.Unlock()
	if userMsg == "" || strings.Contains(userMsg, "节点") || strings.Contains(userMsg, "203.0.113.21") || strings.Contains(userMsg, "edge-tok") {
		t.Fatalf("user expire copy %s", userMsg)
	}

	if err := db.SetSetting(srv.DB, "tg_user_push_expire", "0"); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`DELETE FROM tg_notify_state`); err != nil {
		t.Fatal(err)
	}
	before := cap.count("sendMessage")
	srv.tgScanUsers()
	cap.mu.Lock()
	for _, raw := range cap.calls {
		if strings.Contains(raw, "sendMessage") && strings.Contains(raw, `"chat_id":200`) && strings.Contains(raw, "自定义到期") {
			// the earlier message may still be in the log; only new sends matter
		}
	}
	cap.mu.Unlock()
	if cap.count("sendMessage") != before+1 {
		t.Fatalf("expected only the admin expire send, before %d after %d\n%s", before, cap.count("sendMessage"), cap.raw())
	}
}

func TestTelegramTemplateSettings(t *testing.T) {
	srv, _, _ := newTGPanel(t)
	jar, _ := cookiejar.New(nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	c := &http.Client{Jar: jar}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	body, _ := json.Marshal(map[string]any{
		"tg_user_nodes":        true,
		"tg_user_week":         false,
		"tg_user_push_expire":  false,
		"tg_user_push_traffic": true,
		"tg_templates": map[string]string{
			"tg_tpl_backup": strings.Repeat("备", 401),
		},
	})
	res, err = httpPut(c, ts.URL+"/api/settings", body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("long template status %d", res.StatusCode)
	}
	res.Body.Close()

	body, _ = json.Marshal(map[string]any{
		"tg_user_nodes":       true,
		"tg_user_week":        false,
		"tg_user_push_expire": false,
		"tg_templates": map[string]string{
			"tg_tpl_backup": "备份失败 {error}",
		},
	})
	res, err = httpPut(c, ts.URL+"/api/settings", body)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	var settings map[string]any
	decodeRes(t, res, &settings)
	if settings["tg_user_nodes"] != true || settings["tg_user_week"] != false || settings["tg_user_push_expire"] != false {
		t.Fatalf("prefs %#v", settings)
	}
	raw, _ := json.Marshal(settings["tg_templates"])
	if !strings.Contains(string(raw), "备份失败 {error}") || !strings.Contains(string(raw), "tg_tpl_server_down") {
		t.Fatalf("templates %s", raw)
	}
}

func httpPut(c *http.Client, url string, body []byte) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.Do(req)
}
