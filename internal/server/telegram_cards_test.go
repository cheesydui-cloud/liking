package server

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func (c *tgCapture) raw() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return strings.Join(c.calls, "\n")
}

func TestUserSubClearsReplyKeyboard(t *testing.T) {
	prev := tgKeyboardClearHold
	tgKeyboardClearHold = 0
	t.Cleanup(func() { tgKeyboardClearHold = prev })
	srv, cap, _ := newTGPanel(t)
	alice, err := db.CreateUser(srv.DB, "alice", "x", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE users SET enabled=1 WHERE id=?`, alice.ID); err != nil {
		t.Fatal(err)
	}
	code, _, err := srv.issueTGCode(alice.ID, db.TGKindBind)
	if err != nil {
		t.Fatal(err)
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/bind " + code, Chat: tgChat{ID: 200, Type: "private"}}})
	before := cap.count("sendMessage")
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/sub", Chat: tgChat{ID: 200, Type: "private"}}})
	if cap.count("sendMessage") != before+1 {
		t.Fatalf("sub sends %d", cap.count("sendMessage")-before)
	}
	raw := cap.raw()
	if !strings.Contains(raw, `"remove_keyboard":true`) {
		t.Fatalf("sub left the reply keyboard %s", raw)
	}
	if !strings.Contains(raw, "editMessageReplyMarkup") || !strings.Contains(raw, "通用") {
		t.Fatalf("sub buttons %s", raw)
	}
	for _, label := range []string{`"text":"首页"`, `"text":"节点"`, `"text":"订阅"`, `"text":"近7日"`} {
		if strings.Contains(raw, label) {
			t.Fatalf("reply key %s still sent %s", label, raw)
		}
	}

	sent := cap.count("sendMessage")
	srv.tgClearUserReplyKeyboards()
	if cap.count("sendMessage") != sent+1 || !strings.Contains(cap.raw(), "底部按钮已收起") || !strings.Contains(cap.raw(), "deleteMessage") {
		t.Fatalf("clear %d %s", cap.count("sendMessage")-sent, cap.raw())
	}
	for _, call := range strings.Split(cap.raw(), "\n") {
		if strings.Contains(call, "底部按钮已收起") && strings.Contains(call, `"chat_id":100`) {
			t.Fatalf("cleared the admin keyboard %s", call)
		}
	}
	srv.tgClearUserReplyKeyboards()
	if cap.count("sendMessage") != sent+1 {
		t.Fatalf("cleared again %d", cap.count("sendMessage")-sent)
	}
}

func TestTelegramCards(t *testing.T) {
	srv, cap, _ := newTGPanel(t)
	alice, err := db.CreateUser(srv.DB, "alice", "x", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE users SET enabled=1, expires_at=? WHERE id=?`, time.Now().Add(10*24*time.Hour).Unix(), alice.ID); err != nil {
		t.Fatal(err)
	}
	alice, err = db.GetUser(srv.DB, alice.ID)
	if err != nil || alice == nil || alice.SubToken == "" {
		t.Fatal("missing sub token")
	}
	code, _, err := srv.issueTGCode(alice.ID, db.TGKindBind)
	if err != nil {
		t.Fatal(err)
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/bind " + code, Chat: tgChat{ID: 200, Type: "private"}}})
	home := cap.texts()
	if len(home) == 0 || !strings.Contains(home[len(home)-1], "alice") || !strings.Contains(home[len(home)-1], "还剩") || !strings.Contains(home[len(home)-1], "/traffic") {
		t.Fatalf("bind %v", home)
	}
	if strings.Contains(home[len(home)-1], alice.SubToken) || strings.Contains(home[len(home)-1], "节点 ") || strings.Contains(home[len(home)-1], "近 7 日") {
		t.Fatalf("bind leaked extras %s", home[len(home)-1])
	}
	body := cap.raw()
	if !strings.Contains(body, `"remove_keyboard":true`) {
		t.Fatalf("user bind kept the reply keyboard %s", body)
	}
	for _, label := range []string{`"text":"节点"`, `"text":"订阅"`, `"text":"近7日"`} {
		if strings.Contains(body, label) {
			t.Fatalf("user keyboard still has %s in %s", label, body)
		}
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/traffic", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); !strings.Contains(got[len(got)-1], "流量") || !strings.Contains(got[len(got)-1], "还剩") || strings.Contains(got[len(got)-1], "节点 ") || strings.Contains(got[len(got)-1], alice.SubToken) {
		t.Fatalf("traffic %s", got[len(got)-1])
	}
	alice.Direction = "oneway"
	if text := srv.tgTrafficText(alice); !strings.Contains(text, "单向") || strings.Contains(text, "节点 ") {
		t.Fatalf("traffic direction %s", text)
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/start", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); !strings.Contains(got[len(got)-1], "/sub") || !strings.Contains(got[len(got)-1], "/unbind") || strings.Contains(got[len(got)-1], "节点 ") {
		t.Fatalf("bare start %s", got[len(got)-1])
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "/nope", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); !strings.Contains(got[len(got)-1], "/traffic") || strings.Contains(got[len(got)-1], "/me") || strings.Contains(got[len(got)-1], "/start") {
		t.Fatalf("unknown command %s", got[len(got)-1])
	}

	edge, err := db.CreateServer(srv.DB, "edge", "203.0.113.21", "edge-tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE servers SET online=1, last_seen=? WHERE id=?`, time.Now().Unix(), edge.ID); err != nil {
		t.Fatal(err)
	}
	proto, network, security := corecfg.Spec(corecfg.ProfileMieru)
	live, err := db.CreateInbound(srv.DB, &db.Inbound{
		ServerID:    edge.ID,
		Name:        "香港",
		Profile:     corecfg.ProfileMieru,
		Protocol:    proto,
		Network:     network,
		Security:    security,
		Core:        corecfg.CoreMita,
		Port:        39198,
		Enabled:     true,
		Settings:    `{"transport":"UDP"}`,
		OwnerUserID: alice.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(srv.DB, &db.Inbound{
		ServerID:    edge.ID,
		Name:        "备用",
		Profile:     corecfg.ProfileMieru,
		Protocol:    proto,
		Network:     network,
		Security:    security,
		Core:        corecfg.CoreMita,
		Port:        39199,
		Enabled:     false,
		Settings:    `{"transport":"UDP"}`,
		OwnerUserID: alice.ID,
	}); err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(alice.ID, live.ID)
	if err := db.UpsertClient(srv.DB, &db.Client{
		InboundID: live.ID,
		UserID:    alice.ID,
		Email:     email,
		Username:  email,
		Password:  "nodepass-9",
		Enabled:   true,
	}); err != nil {
		t.Fatal(err)
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "节点", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); !strings.Contains(got[len(got)-1], "/sub") || strings.Contains(got[len(got)-1], "203.0.113.21") || strings.Contains(got[len(got)-1], "在线") {
		t.Fatalf("nodes should stay closed %s", got[len(got)-1])
	}
	if err := db.SetSetting(srv.DB, "tg_user_nodes", "1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(srv.DB, "tg_user_week", "1"); err != nil {
		t.Fatal(err)
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "节点", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); strings.Contains(got[len(got)-1], "203.0.113.21") || strings.Contains(got[len(got)-1], "在线") || strings.Contains(got[len(got)-1], "mierus://") || strings.Contains(got[len(got)-1], "nodepass-9") {
		t.Fatalf("node setting must not list lines %s", got[len(got)-1])
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "近7日", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); strings.Contains(got[len(got)-1], "近 7 日") || strings.Contains(got[len(got)-1], "203.0.113.21") {
		t.Fatalf("week must stay hidden %s", got[len(got)-1])
	}
	srv.tgHandleUpdate(tgUpdate{Callback: &tgCallback{
		ID:   "n1",
		Data: "nlink:" + strconv.FormatInt(live.ID, 10),
		Message: &tgMessage{
			MessageID: 7,
			Chat:      tgChat{ID: 200, Type: "private"},
		},
	}})
	revealed := cap.texts()
	if strings.Contains(revealed[len(revealed)-1], "mierus://") || strings.Contains(revealed[len(revealed)-1], "nodepass-9") || strings.Contains(revealed[len(revealed)-1], "203.0.113.21") {
		t.Fatalf("link tap %s", revealed[len(revealed)-1])
	}
	srv.tgHandleUpdate(tgUpdate{Callback: &tgCallback{
		ID:   "user-self",
		Data: "user:" + strconv.FormatInt(alice.ID, 10),
		Message: &tgMessage{
			MessageID: 8,
			Chat:      tgChat{ID: 200, Type: "private"},
		},
	}})
	if got := cap.texts(); !strings.Contains(got[len(got)-1], "流量") || strings.Contains(got[len(got)-1], "203.0.113.21") || strings.Contains(got[len(got)-1], "近 7 日") || strings.Contains(got[len(got)-1], alice.SubToken) {
		t.Fatalf("user account button %s", got[len(got)-1])
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "机器", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); got[len(got)-1] != "没有权限" || strings.Contains(got[len(got)-1], "edge-tok") {
		t.Fatalf("machine deny %q", got[len(got)-1])
	}
	carol, err := db.CreateUser(srv.DB, "carol", "x", "user", "carol-note-hidden")
	if err != nil {
		t.Fatal(err)
	}
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "carol", Chat: tgChat{ID: 200, Type: "private"}}})
	if got := cap.texts(); got[len(got)-1] != "没有权限" || strings.Contains(got[len(got)-1], carol.Remark) || strings.Contains(got[len(got)-1], "套餐") {
		t.Fatalf("name deny %q", got[len(got)-1])
	}

	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "alice", Chat: tgChat{ID: 100, Type: "private"}}})
	adminCard := cap.texts()
	if !strings.Contains(adminCard[len(adminCard)-1], "alice") || !strings.Contains(adminCard[len(adminCard)-1], "还剩") || strings.Contains(adminCard[len(adminCard)-1], alice.SubToken) {
		t.Fatalf("admin name %s", adminCard[len(adminCard)-1])
	}
	last := ""
	cap.mu.Lock()
	for i := len(cap.calls) - 1; i >= 0; i-- {
		if strings.Contains(cap.calls[i], "sendMessage") && strings.Contains(cap.calls[i], "alice") {
			last = cap.calls[i]
			break
		}
	}
	cap.mu.Unlock()
	if !strings.Contains(last, "回总览") || !strings.Contains(last, `"callback_data":"overview"`) {
		t.Fatalf("account button %s", last)
	}
	if strings.Contains(last, "查看账号") || strings.Contains(last, `"callback_data":"user:`) {
		t.Fatalf("account loop %s", last)
	}
	_, alertMarkup := srv.tgExpireAlert(alice, 2)
	if alertMarkup == nil || len(alertMarkup.Keyboard) == 0 || len(alertMarkup.Keyboard[0]) == 0 || alertMarkup.Keyboard[0][0].Text != "查看账号" {
		t.Fatal("alert should keep 查看账号")
	}

	cap.editUnchanged = true
	beforeSend := cap.count("sendMessage")
	srv.tgHandleUpdate(tgUpdate{Callback: &tgCallback{
		ID:   "home-refresh",
		Data: "home",
		Message: &tgMessage{
			MessageID: 9,
			Chat:      tgChat{ID: 100, Type: "private"},
		},
	}})
	if cap.count("sendMessage") != beforeSend {
		t.Fatalf("unchanged refresh duplicated a card\n%s", cap.raw())
	}
	if cap.count("editMessageText") == 0 {
		t.Fatal("refresh did not edit")
	}

	kyoto, err := db.CreateServer(srv.DB, "kyoto", "203.0.113.30", "kyo-tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.DB.Exec(`UPDATE servers SET online=0, last_seen=? WHERE id=?`, time.Now().Unix(), kyoto.ID); err != nil {
		t.Fatal(err)
	}
	srv.tgScanServers()
	if !strings.Contains(cap.joined(), "kyoto 离线") || strings.Contains(cap.joined(), "kyo-tok") {
		t.Fatalf("offline alert %s", cap.joined())
	}
	cap.mu.Lock()
	for _, raw := range cap.calls {
		if strings.Contains(raw, "sendMessage") && strings.Contains(raw, "离线") && strings.Contains(raw, `"chat_id":200`) {
			cap.mu.Unlock()
			t.Fatalf("node status pushed to user %s", raw)
		}
	}
	cap.mu.Unlock()
	if !strings.Contains(cap.raw(), "查看机器") {
		t.Fatal("missing machine button")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.tgRememberBot(ctx)
	raw := cap.raw()
	if !strings.Contains(raw, "setMyCommands") {
		t.Fatal("commands not registered")
	}
	def := ""
	cap.mu.Lock()
	for _, call := range cap.calls {
		if strings.Contains(call, "setMyCommands") && strings.Contains(call, `"type":"default"`) {
			def = call
			break
		}
	}
	cap.mu.Unlock()
	if def == "" {
		t.Fatal("default command scope missing")
	}
	for _, keep := range []string{`"command":"sub"`, `"description":"订阅链接"`, `"command":"traffic"`, `"description":"流量查询"`, `"command":"unbind"`, `"description":"解除绑定"`} {
		if !strings.Contains(def, keep) {
			t.Fatalf("default menu missing %s in %s", keep, def)
		}
	}
	for _, drop := range []string{`"command":"start"`, `"command":"me"`, `"command":"help"`, `"command":"status"`, `"command":"user"`, `"command":"bind"`} {
		if strings.Contains(def, drop) {
			t.Fatalf("default menu still has %s in %s", drop, def)
		}
	}
	adminMenu := ""
	cap.mu.Lock()
	for _, call := range cap.calls {
		if strings.Contains(call, "setMyCommands") && strings.Contains(call, `"type":"chat"`) {
			adminMenu = call
			break
		}
	}
	cap.mu.Unlock()
	if adminMenu == "" || !strings.Contains(adminMenu, `"command":"status"`) || !strings.Contains(adminMenu, `"command":"user"`) {
		t.Fatalf("admin menu %s", adminMenu)
	}
	if strings.Contains(adminMenu, `"command":"sub"`) || strings.Contains(adminMenu, `"command":"traffic"`) {
		t.Fatalf("admin menu copied the user list %s", adminMenu)
	}
}
