package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"liking/internal/db"
)

type tgCapture struct {
	mu    sync.Mutex
	calls []string
}

func (c *tgCapture) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	c.mu.Lock()
	c.calls = append(c.calls, r.URL.Path+" "+string(body))
	c.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case strings.Contains(r.URL.Path, "getUpdates"):
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	case strings.Contains(r.URL.Path, "getMe"):
		_, _ = w.Write([]byte(`{"ok":true,"result":{"username":"liking_bot"}}`))
	case strings.Contains(r.URL.Path, "answerCallbackQuery"):
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":7}}`))
	}
}

func (c *tgCapture) texts() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, raw := range c.calls {
		if !strings.Contains(raw, "sendMessage") && !strings.Contains(raw, "editMessageText") {
			continue
		}
		i := strings.IndexByte(raw, '{')
		if i < 0 {
			continue
		}
		var body struct {
			Text string `json:"text"`
		}
		if json.Unmarshal([]byte(raw[i:]), &body) == nil && body.Text != "" {
			out = append(out, body.Text)
		}
	}
	return out
}

func (c *tgCapture) joined() string {
	return strings.Join(c.texts(), "\n")
}

func newTGPanel(t *testing.T) (*Server, *tgCapture, *httptest.Server) {
	t.Helper()
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	hash, err := HashPassword("secret12")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateUser(d, "admin", hash, "admin", ""); err != nil {
		t.Fatal(err)
	}
	cap := &tgCapture{}
	fake := httptest.NewServer(cap)
	t.Cleanup(fake.Close)
	if err := db.SetSetting(d, "tg_api_base", fake.URL); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(d, "tg_bot_token", "123456:TESTTOKEN"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(d, "tg_admin_chat", "100"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(d, "tg_enabled", "1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(d, "panel_url", "https://panel.example"); err != nil {
		t.Fatal(err)
	}
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Close)
	return srv, cap, fake
}

func TestTelegramCommandsAndAlerts(t *testing.T) {
	srv, cap, fake := newTGPanel(t)
	alice, err := db.CreateUser(srv.DB, "alice", "x", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE users SET enabled=1, traffic_limit=100, used_down=80, expires_at=? WHERE id=?`, time.Now().Add(48*time.Hour).Unix(), alice.ID); err != nil {
		t.Fatal(err)
	}
	row, err := db.CreateServer(srv.DB, "tokyo", "203.0.113.10", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE servers SET online=0, last_seen=? WHERE id=?`, time.Now().Unix(), row.ID); err != nil {
		t.Fatal(err)
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/status", Chat: tgChat{ID: 300, Type: "private"}}})
	if strings.Contains(cap.joined(), "tokyo") {
		t.Fatalf("other chat saw status %s", cap.joined())
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/user alice", Chat: tgChat{ID: 300, Type: "private"}}})
	if strings.Contains(cap.joined(), "套餐") {
		t.Fatalf("other chat saw user %s", cap.joined())
	}

	before := len(cap.texts())
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/status", Chat: tgChat{ID: -100, Type: "group"}}})
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/me", Chat: tgChat{ID: 9, Type: "supergroup"}}})
	if len(cap.texts()) != before {
		t.Fatalf("group was answered %s", cap.joined())
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/status", Chat: tgChat{ID: 100, Type: "private"}}})
	if !strings.Contains(cap.joined(), "tokyo") {
		t.Fatalf("admin status %s", cap.joined())
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/user alice", Chat: tgChat{ID: 100, Type: "private"}}})
	if !strings.Contains(cap.joined(), "alice") || strings.Contains(cap.joined(), "secret") {
		t.Fatalf("admin user text %s", cap.joined())
	}

	code, exp, err := srv.issueTGCode(alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv.tgMu.Lock()
	srv.tgCodes[alice.ID].exp = time.Now().Add(-time.Minute)
	srv.tgMu.Unlock()
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/bind " + code, Chat: tgChat{ID: 200, Type: "private"}}})
	if !strings.Contains(cap.joined(), "已过期") {
		t.Fatalf("expiry %s", cap.joined())
	}
	_ = exp

	code, _, err = srv.issueTGCode(alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/bind 000000", Chat: tgChat{ID: 200, Type: "private"}}})
	}
	if !strings.Contains(cap.joined(), "已作废") {
		t.Fatalf("fails %s", cap.joined())
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/bind " + code, Chat: tgChat{ID: 200, Type: "private"}}})
	if _, ok := srv.tgBoundUser(200); ok {
		t.Fatal("voided code still bound")
	}

	code, _, err = srv.issueTGCode(alice.ID)
	if err != nil {
		t.Fatal(err)
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/bind " + code, Chat: tgChat{ID: 200, Type: "private"}}})
	if _, ok := srv.tgBoundUser(200); !ok {
		t.Fatal("bind failed")
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/me", Chat: tgChat{ID: 300, Type: "private"}}})
	me := cap.texts()
	if me[len(me)-1] != "还没有绑定" {
		t.Fatalf("foreign /me %q", me[len(me)-1])
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/sub", Chat: tgChat{ID: 200, Type: "private"}}})
	if strings.Contains(cap.joined(), "/api/sub/") {
		t.Fatalf("link shown before button %s", cap.joined())
	}
	srv.tgHandleUpdate(tgUpdate{Callback: &tgCallback{
		ID:   "cb1",
		Data: "sub",
		Message: &tgMessage{
			MessageID: 7,
			Chat:      tgChat{ID: 9, Type: "group"},
		},
	}})
	if strings.Contains(cap.joined(), "/api/sub/") {
		t.Fatalf("group callback revealed link %s", cap.joined())
	}
	srv.tgHandleUpdate(tgUpdate{Callback: &tgCallback{
		ID:   "cb2",
		Data: "sub",
		Message: &tgMessage{
			MessageID: 7,
			Chat:      tgChat{ID: 300, Type: "private"},
		},
	}})
	if strings.Contains(cap.joined(), "/api/sub/") {
		t.Fatalf("other chat revealed link %s", cap.joined())
	}
	srv.tgHandleUpdate(tgUpdate{Callback: &tgCallback{
		ID:   "cb3",
		Data: "sub",
		Message: &tgMessage{
			MessageID: 7,
			Chat:      tgChat{ID: 200, Type: "private"},
		},
	}})
	if !strings.Contains(cap.joined(), "https://panel.example/api/sub/") {
		t.Fatalf("own reveal %s", cap.joined())
	}

	n := strings.Count(cap.joined(), "离线")
	srv.tgScan()
	if strings.Count(cap.joined(), "tokyo 离线") != 1 {
		t.Fatalf("offline %s", cap.joined())
	}
	srv.tgScan()
	if strings.Count(cap.joined(), "tokyo 离线") != 1 {
		t.Fatalf("offline repeated %s", cap.joined())
	}
	srv.Hub.mu.Lock()
	srv.Hub.conns[row.ID] = &agentConn{closed: make(chan struct{})}
	srv.Hub.mu.Unlock()
	t.Cleanup(func() {
		srv.Hub.mu.Lock()
		delete(srv.Hub.conns, row.ID)
		srv.Hub.mu.Unlock()
	})
	srv.tgScan()
	if !strings.Contains(cap.joined(), "tokyo 已恢复在线") {
		t.Fatalf("recovery %s", cap.joined())
	}
	srv.tgScan()
	if strings.Count(cap.joined(), "已恢复在线") != 1 {
		t.Fatalf("recovery repeated %s", cap.joined())
	}
	_ = n

	if strings.Count(cap.joined(), "将于 2 天后到期") != 1 && strings.Count(cap.joined(), "将于 1 天后到期") != 1 && strings.Count(cap.joined(), "将于 3 天后到期") != 1 {
		// 48h can be 1 or 2 calendar days depending on the clock.
		if !strings.Contains(cap.joined(), "天后到期") {
			t.Fatalf("expire %s", cap.joined())
		}
	}
	expireN := strings.Count(cap.joined(), "天后到期") + strings.Count(cap.joined(), "今天到期")
	srv.tgScan()
	if strings.Count(cap.joined(), "天后到期")+strings.Count(cap.joined(), "今天到期") != expireN {
		t.Fatalf("expire repeated %s", cap.joined())
	}
	if !strings.Contains(cap.joined(), "流量已用 80%") {
		t.Fatalf("traffic %s", cap.joined())
	}

	jar, _ := cookiejar.New(nil)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	c := &http.Client{Jar: jar}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytesReader(login))
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
	raw, _ := json.Marshal(settings)
	if strings.Contains(string(raw), "TESTTOKEN") {
		t.Fatalf("token leaked %s", raw)
	}
	res, err = c.Post(ts.URL+"/api/settings/telegram/test", "application/json", bytesReader([]byte(`{}`)))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	if !strings.Contains(cap.joined(), "liking 测试消息") {
		t.Fatalf("test message missing %s", cap.joined())
	}
	if !strings.Contains(fake.URL, "127.0.0.1") && !strings.Contains(fake.URL, "localhost") {
		t.Fatalf("fake url %s", fake.URL)
	}
}

func bytesReader(b []byte) *strings.Reader {
	return strings.NewReader(string(b))
}
