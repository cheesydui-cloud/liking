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
	if len(home) == 0 || !strings.Contains(home[len(home)-1], "alice") || !strings.Contains(home[len(home)-1], "还剩") {
		t.Fatalf("home %v", home)
	}
	if strings.Contains(home[len(home)-1], alice.SubToken) {
		t.Fatalf("home leaked token %s", home[len(home)-1])
	}
	body := cap.raw()
	for _, label := range []string{`"text":"首页"`, `"text":"节点"`, `"text":"订阅"`, `"text":"近7日"`} {
		if !strings.Contains(body, label) {
			t.Fatalf("missing %s in %s", label, body)
		}
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

	before := len(cap.texts())
	srv.tgHandleUpdate(tgUpdate{Message: &tgMessage{Text: "节点", Chat: tgChat{ID: 200, Type: "private"}}})
	nodes := strings.Join(cap.texts()[before:], "\n")
	if !strings.Contains(nodes, "203.0.113.21:39198") || !strings.Contains(nodes, "在线") || !strings.Contains(nodes, "停用") {
		t.Fatalf("nodes %s", nodes)
	}
	if strings.Contains(nodes, "mierus://") || strings.Contains(nodes, "nodepass-9") || strings.Contains(nodes, alice.SubToken) {
		t.Fatalf("node list leaked %s", nodes)
	}
	if !strings.Contains(cap.raw(), "这条链接") {
		t.Fatal("missing link button")
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
	if !strings.Contains(revealed[len(revealed)-1], "mierus://") || !strings.Contains(revealed[len(revealed)-1], "nodepass-9") {
		t.Fatalf("link tap %s", revealed[len(revealed)-1])
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
	if !strings.Contains(cap.raw(), "查看账号") {
		t.Fatal("missing account button")
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
	if !strings.Contains(cap.raw(), "查看机器") {
		t.Fatal("missing machine button")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	srv.tgRememberBot(ctx)
	if !strings.Contains(cap.raw(), "setMyCommands") {
		t.Fatal("commands not registered")
	}
}

