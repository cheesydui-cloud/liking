package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"liking/internal/db"
)

const (
	tgDefaultBase   = "https://api.telegram.org"
	tgRepeat        = 6 * time.Hour
	tgBindTTL       = 10 * time.Minute
	tgBindMaxFail   = 5
	tgScanEvery     = 10 * time.Minute
	tgPollSleep     = 15 * time.Second
	tgCertSoon      = 30 * 24 * time.Hour
	tgExpireSoonDay = 3
	tgLastErrorKey  = "tg_last_error"
)

const tgOpenHint = "回到面板，点「用 Telegram 打开」。"

type tgUpdate struct {
	UpdateID int64       `json:"update_id"`
	Message  *tgMessage  `json:"message"`
	Callback *tgCallback `json:"callback_query"`
}

type tgMessage struct {
	MessageID int64  `json:"message_id"`
	Text      string `json:"text"`
	Chat      tgChat `json:"chat"`
}

type tgChat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

type tgCallback struct {
	ID      string     `json:"id"`
	Data    string     `json:"data"`
	Message *tgMessage `json:"message"`
}

type tgMarkup struct {
	Keyboard [][]tgButton `json:"inline_keyboard"`
}

type tgButton struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

func (s *Server) telegramPollLoop() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		<-s.stop
		cancel()
	}()
	dropped := ""
	for {
		if ctx.Err() != nil {
			return
		}
		if !s.tgEnabled() {
			dropped = ""
			if !sleepCtx(ctx, tgPollSleep) {
				return
			}
			continue
		}
		token := s.tgToken()
		if dropped != token {
			s.tgDropWebhook()
			dropped = token
		}
		s.tgPoll(ctx)
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *Server) telegramScanLoop() {
	t := time.NewTicker(tgScanEvery)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.tgScan()
		}
	}
}

func (s *Server) tgEnabled() bool {
	return db.SettingOn(s.DB, "tg_enabled", false) && s.tgToken() != ""
}

func (s *Server) tgToken() string {
	v, _ := db.GetSetting(s.DB, "tg_bot_token")
	return normalizeTGToken(v)
}

func (s *Server) tgBase() string {
	v, _ := db.GetSetting(s.DB, "tg_api_base")
	v = strings.TrimSpace(v)
	if v == "" {
		return tgDefaultBase
	}
	return strings.TrimRight(v, "/")
}

func (s *Server) tgAdminChat() (int64, bool) {
	v, _ := db.GetSetting(s.DB, "tg_admin_chat")
	id, err := parseTGChat(v)
	return id, err == nil
}

func (s *Server) tgAlert(key string) bool {
	return db.SettingOn(s.DB, key, true)
}

func (s *Server) tgPoll(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("telegram poll: %v", rec)
			sleepCtx(ctx, tgPollSleep)
		}
	}()
	offset := int64(0)
	if raw, _ := db.GetSetting(s.DB, "tg_update_offset"); raw != "" {
		offset, _ = strconv.ParseInt(raw, 10, 64)
	}
	tokenAtStart := s.tgToken()
	callCtx, cancel := context.WithTimeout(ctx, 40*time.Second)
	started := time.Now()
	var updates []tgUpdate
	err := s.tgCall(callCtx, "getUpdates", map[string]any{
		"timeout":         25,
		"offset":          offset,
		"allowed_updates": []string{"message", "callback_query"},
	}, &updates)
	cancel()
	if err != nil {
		if ctx.Err() == nil {
			if tgWebhookConflict(err) {
				s.tgDropWebhook()
			}
			s.tgNoteErr(err)
			sleepCtx(ctx, tgPollSleep)
		}
		return
	}
	s.tgNoteErr(nil)
	if len(updates) == 0 {
		// A proxy that ignores the long-poll timeout would otherwise spin.
		if time.Since(started) < time.Second {
			sleepCtx(ctx, tgPollSleep)
		}
		return
	}
	var last int64
	for _, up := range updates {
		if up.UpdateID > last {
			last = up.UpdateID
		}
		s.tgHandleUpdate(up)
	}
	if last > 0 && s.tgToken() == tokenAtStart {
		_ = db.SetSetting(s.DB, "tg_update_offset", strconv.FormatInt(last+1, 10))
	}
}

func (s *Server) tgHandleUpdate(up tgUpdate) {
	if up.Callback != nil {
		s.tgHandleCallback(up.Callback)
		return
	}
	if up.Message == nil {
		return
	}
	msg := up.Message
	if !tgPrivate(msg.Chat) {
		return
	}
	text := strings.TrimSpace(msg.Text)
	if text == "" {
		return
	}
	fields := strings.Fields(text)
	cmd := strings.ToLower(strings.SplitN(fields[0], "@", 2)[0])
	arg := ""
	if len(fields) > 1 {
		arg = strings.TrimSpace(strings.Join(fields[1:], " "))
	}
	switch cmd {
	case "/start", "/help":
		if cmd == "/start" && arg != "" {
			s.tgApplyCode(msg.Chat.ID, arg)
			return
		}
		s.tgReply(msg.Chat.ID, s.tgHelp(msg.Chat.ID))
	case "/status":
		s.tgCmdStatus(msg.Chat.ID)
	case "/user":
		s.tgCmdUser(msg.Chat.ID, arg)
	case "/bind":
		if arg == "" {
			s.tgReply(msg.Chat.ID, tgOpenHint+"打不开时发送 /bind 和 6 位绑定码。")
			return
		}
		s.tgApplyCode(msg.Chat.ID, arg)
	case "/me":
		s.tgCmdMe(msg.Chat.ID)
	case "/sub":
		s.tgCmdSub(msg.Chat.ID)
	case "/unbind":
		s.tgCmdUnbind(msg.Chat.ID)
	default:
		if tgDigits(text) {
			s.tgApplyCode(msg.Chat.ID, text)
			return
		}
		if strings.HasPrefix(cmd, "/") {
			s.tgReply(msg.Chat.ID, "不认识这个命令。发送 /help 查看可用命令。")
		}
	}
}

func (s *Server) tgHandleCallback(cb *tgCallback) {
	if cb == nil {
		return
	}
	chatID := int64(0)
	msgID := int64(0)
	private := false
	if cb.Message != nil {
		chatID = cb.Message.Chat.ID
		msgID = cb.Message.MessageID
		private = tgPrivate(cb.Message.Chat)
	}
	if !private {
		_ = s.tgAnswer(cb.ID, "只在私聊里使用")
		return
	}
	if cb.Data != "sub" {
		_ = s.tgAnswer(cb.ID, "不认识这个按钮")
		return
	}
	u, ok := s.tgBoundUser(chatID)
	if !ok {
		_ = s.tgAnswer(cb.ID, "还没有绑定")
		return
	}
	link, err := s.tgSubLink(u)
	if err != nil {
		_ = s.tgAnswer(cb.ID, err.Error())
		return
	}
	_ = s.tgAnswer(cb.ID, "已显示")
	if err := s.tgEdit(chatID, msgID, "订阅链接：\n"+link, &tgMarkup{Keyboard: [][]tgButton{}}); err != nil {
		s.tgReply(chatID, "订阅链接：\n"+link)
	}
	db.AddAudit(s.DB, &u.ID, "tg.sub", u.Username)
}

func tgPrivate(c tgChat) bool {
	return c.Type == "private" && c.ID > 0
}

func (s *Server) tgHelp(chatID int64) string {
	admin := s.tgIsAdmin(chatID)
	_, bound := s.tgBoundUser(chatID)
	if !admin && !bound {
		return tgOpenHint
	}
	var b strings.Builder
	if admin {
		b.WriteString("管理员\n/status  在线机器\n/user 用户名  查看账号")
	}
	if bound {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("/me  流量和到期\n/sub  订阅链接\n/unbind  解除绑定")
	}
	return b.String()
}

func (s *Server) tgIsAdmin(chatID int64) bool {
	id, ok := s.tgAdminChat()
	return ok && id == chatID
}

func (s *Server) tgCmdStatus(chatID int64) {
	if !s.tgIsAdmin(chatID) {
		s.tgReply(chatID, "这个对话还不是管理员。"+tgOpenHint)
		return
	}
	list, err := db.ListServers(s.DB)
	if err != nil {
		s.tgReply(chatID, "查询失败")
		return
	}
	s.reconcileOnline(list)
	online := 0
	var off []string
	for _, x := range list {
		if x == nil {
			continue
		}
		if x.Online == 1 {
			online++
			continue
		}
		if x.LastSeen > 0 {
			off = append(off, x.Name)
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "在线 %d / 共 %d 台", online, len(list))
	if len(off) == 0 {
		b.WriteString("\n没有离线机器")
	} else {
		b.WriteString("\n离线：")
		n := len(off)
		if n > 30 {
			off = off[:30]
		}
		for _, name := range off {
			b.WriteString("\n- " + name)
		}
		if n > 30 {
			fmt.Fprintf(&b, "\n还有 %d 台", n-30)
		}
	}
	s.tgReply(chatID, b.String())
}

func (s *Server) tgCmdUser(chatID int64, name string) {
	if !s.tgIsAdmin(chatID) {
		s.tgReply(chatID, "这个对话还不是管理员。"+tgOpenHint)
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		s.tgReply(chatID, "用法：/user 用户名")
		return
	}
	u, err := db.GetUserByName(s.DB, name)
	if err != nil || u == nil || u.Role == "admin" {
		s.tgReply(chatID, "没有这个用户")
		return
	}
	s.tgReply(chatID, s.tgUserText(u))
}

func (s *Server) tgCmdMe(chatID int64) {
	u, ok := s.tgBoundUser(chatID)
	if !ok {
		s.tgReply(chatID, "还没有绑定")
		return
	}
	s.tgReply(chatID, s.tgUserText(u))
}

func (s *Server) tgCmdSub(chatID int64) {
	if _, ok := s.tgBoundUser(chatID); !ok {
		s.tgReply(chatID, "还没有绑定")
		return
	}
	s.tgSend(chatID, "订阅链接已隐藏。点按钮后只在这条私聊里显示。", &tgMarkup{
		Keyboard: [][]tgButton{{{Text: "显示订阅链接", Data: "sub"}}},
	})
}

func (s *Server) tgApplyCode(chatID int64, code string) {
	code = strings.TrimSpace(code)
	if code == "" {
		s.tgReply(chatID, tgOpenHint)
		return
	}
	userID, kind, msg, err := db.TakeTGCode(s.DB, code, time.Now().Unix(), tgBindMaxFail)
	if err != nil {
		s.tgReply(chatID, "绑定失败")
		return
	}
	if userID == 0 {
		s.tgReply(chatID, msg)
		return
	}
	if kind == db.TGKindAdmin {
		s.tgClaimAdmin(chatID, userID)
		return
	}
	s.tgFinishBind(chatID, userID)
}

func (s *Server) tgClaimAdmin(chatID, userID int64) {
	prev, had := s.tgAdminChat()
	if err := db.SetSetting(s.DB, "tg_admin_chat", strconv.FormatInt(chatID, 10)); err != nil {
		s.tgReply(chatID, "连接失败")
		return
	}
	if had && prev != chatID {
		s.tgReply(prev, "管理员已改到别的 Telegram。")
	}
	if userID > 0 {
		db.AddAudit(s.DB, &userID, "tg.admin", strconv.FormatInt(chatID, 10))
	}
	s.tgReply(chatID, "已连接。\n"+s.tgHelp(chatID))
}

func (s *Server) tgFinishBind(chatID, userID int64) {
	prev, had, _ := db.TGChatByUser(s.DB, userID)
	if err := db.TGBind(s.DB, userID, chatID); err != nil {
		if err == db.ErrTGChatTaken {
			s.tgReply(chatID, err.Error())
			return
		}
		s.tgReply(chatID, "绑定失败")
		return
	}
	u, err := db.GetUser(s.DB, userID)
	name := ""
	if err == nil && u != nil {
		name = u.Username
		db.AddAudit(s.DB, &u.ID, "tg.bind", u.Username)
	}
	if had && prev != chatID {
		s.tgReply(prev, "这个账号已改绑到别的 Telegram。")
	}
	if name == "" {
		s.tgReply(chatID, "已绑定")
		return
	}
	s.tgReply(chatID, "已绑定 "+name)
}

func (s *Server) tgCmdUnbind(chatID int64) {
	u, ok := s.tgBoundUser(chatID)
	if !ok {
		s.tgReply(chatID, "还没有绑定")
		return
	}
	if err := db.TGUnbindUser(s.DB, u.ID); err != nil {
		s.tgReply(chatID, "解除失败")
		return
	}
	db.AddAudit(s.DB, &u.ID, "tg.unbind", u.Username)
	s.tgReply(chatID, "已解除绑定")
}

func (s *Server) tgBoundUser(chatID int64) (*db.User, bool) {
	id, err := db.TGUserByChat(s.DB, chatID)
	if err != nil {
		return nil, false
	}
	u, err := db.GetUser(s.DB, id)
	if err != nil || u == nil || u.Role == "admin" {
		return nil, false
	}
	return u, true
}

func (s *Server) tgUserText(u *db.User) string {
	if u == nil {
		return "没有这个用户"
	}
	name := strings.TrimSpace(u.PackageName)
	if name == "" {
		name = "未绑定套餐"
	}
	switch u.Direction {
	case "oneway":
		name += " · 单向"
	case "twoway":
		name += " · 双向"
	}
	exp := soonerExpiry(u.ExpiresAt, u.PkgExpires)
	expText := "不限期"
	if exp > 0 {
		now := db.ClockNow(s.DB)
		expText = time.Unix(exp, 0).In(now.Location()).Format("2006-01-02")
	}
	used := tgBytes(u.BilledBytes)
	traffic := "流量：" + used + "，不限额度"
	if u.TrafficCap > 0 {
		traffic = fmt.Sprintf("流量：%s / %s（%d%%）", used, tgBytes(u.TrafficCap), u.QuotaRatio)
	}
	return u.Username + "\n套餐：" + name + "\n到期：" + expText + "\n" + traffic
}

func (s *Server) tgSubLink(u *db.User) (string, error) {
	if u == nil || strings.TrimSpace(u.SubToken) == "" {
		return "", fmt.Errorf("还没有订阅链接")
	}
	raw, _ := db.GetSetting(s.DB, "panel_url")
	base, err := normalizePanelURL(raw)
	if err != nil || base == "" {
		return "", fmt.Errorf("请先在面板设置里填写面板 URL")
	}
	return base + "/api/sub/" + u.SubToken, nil
}

func (s *Server) issueTGCode(userID int64, kind string) (string, time.Time, error) {
	exp := time.Now().Add(tgBindTTL)
	issued := time.Now()
	var last error
	for i := 0; i < 5; i++ {
		code, err := randomTGCode()
		if err != nil {
			return "", time.Time{}, err
		}
		err = db.IssueTGCode(s.DB, userID, kind, code, exp.Unix(), issued.Unix())
		if err == nil {
			return code, exp, nil
		}
		if !errors.Is(err, db.ErrTGCodeTaken) {
			return "", time.Time{}, err
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("生成绑定码失败")
	}
	return "", time.Time{}, last
}

func randomTGCode() (string, error) {
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	for i := range raw {
		raw[i] = '0' + raw[i]%10
	}
	return string(raw), nil
}

func (s *Server) tgScan() {
	if !s.tgEnabled() {
		return
	}
	s.tgScanServers()
	s.tgScanCerts()
	s.tgScanUsers()
}

func (s *Server) tgScanServers() {
	if !s.tgAlert("tg_alert_server") {
		return
	}
	list, err := db.ListServers(s.DB)
	if err != nil {
		return
	}
	s.reconcileOnline(list)
	now := time.Now().Unix()
	for _, x := range list {
		if x == nil || x.LastSeen <= 0 {
			continue
		}
		key := fmt.Sprintf("server.online:%d", x.ID)
		last, state, ok, err := db.TGNotifyGet(s.DB, key)
		if err != nil {
			continue
		}
		down := x.Online != 1
		if down {
			if ok && state == "down" && now-last < int64(tgRepeat/time.Second) {
				continue
			}
			if s.tgAdmin(x.Name+" 离线") == nil {
				_ = db.TGNotifyPut(s.DB, key, now, "down")
			}
			continue
		}
		if state == "down" {
			if s.tgAdmin(x.Name+" 已恢复在线") == nil {
				_ = db.TGNotifyPut(s.DB, key, now, "up")
			}
			continue
		}
		if !ok || state != "up" {
			_ = db.TGNotifyPut(s.DB, key, now, "up")
		}
	}
}

func (s *Server) tgScanCerts() {
	if !s.tgAlert("tg_alert_cert") {
		return
	}
	certs, err := db.ListCerts(s.DB)
	if err != nil {
		return
	}
	now := time.Now().Unix()
	soon := now + int64(tgCertSoon/time.Second)
	for _, c := range certs {
		if c == nil || c.ExpiresAt <= 0 || c.ExpiresAt > soon {
			continue
		}
		sig := strconv.FormatInt(c.ExpiresAt, 10)
		key := fmt.Sprintf("cert.expire:%d", c.ID)
		_, state, ok, err := db.TGNotifyGet(s.DB, key)
		if err != nil || (ok && state == sig) {
			continue
		}
		text := "证书 " + c.Name + " 即将到期"
		if c.ExpiresAt < now {
			text = "证书 " + c.Name + " 已过期"
		}
		if s.tgAdmin(text) == nil {
			_ = db.TGNotifyPut(s.DB, key, now, sig)
		}
	}
}

func (s *Server) tgScanUsers() {
	users, err := db.ListUsers(s.DB)
	if err != nil {
		return
	}
	nowT := db.ClockNow(s.DB)
	now := nowT.Unix()
	expireOn := s.tgAlert("tg_alert_expire")
	trafficOn := s.tgAlert("tg_alert_traffic")
	for _, u := range users {
		if u == nil || u.Role == "admin" || !u.Enabled {
			continue
		}
		if expireOn {
			s.tgScanExpire(u, nowT, now)
		}
		if trafficOn {
			s.tgScanTraffic(u, now)
		}
	}
}

func (s *Server) tgScanExpire(u *db.User, nowT time.Time, now int64) {
	exp := soonerExpiry(u.ExpiresAt, u.PkgExpires)
	if exp <= 0 {
		return
	}
	delta := dayDelta(nowT, exp)
	kind := ""
	text := ""
	switch {
	case delta < 0:
		kind = "expired"
		text = u.Username + " 已到期"
	case delta <= tgExpireSoonDay:
		kind = "soon"
		if delta <= 0 {
			text = u.Username + " 今天到期"
		} else {
			text = u.Username + " 将于 " + strconv.Itoa(delta) + " 天后到期"
		}
	default:
		return
	}
	sig := kind + ":" + strconv.FormatInt(exp, 10)
	key := fmt.Sprintf("user.expire:%d", u.ID)
	_, state, ok, err := db.TGNotifyGet(s.DB, key)
	if err != nil || (ok && state == sig) {
		return
	}
	if s.tgAdminAndUser(u, text) {
		_ = db.TGNotifyPut(s.DB, key, now, sig)
	}
}

func (s *Server) tgScanTraffic(u *db.User, now int64) {
	if u.TrafficCap <= 0 {
		return
	}
	key := fmt.Sprintf("user.traffic:%d", u.ID)
	last, state, ok, err := db.TGNotifyGet(s.DB, key)
	if err != nil {
		return
	}
	ratio := u.QuotaRatio
	switch {
	case ratio >= 100:
		if ok && state == "over" && now-last < int64(tgRepeat/time.Second) {
			return
		}
		if s.tgAdminAndUser(u, u.Username+" 流量已用尽") {
			_ = db.TGNotifyPut(s.DB, key, now, "over")
		}
	case state == "over":
		if s.tgAdminAndUser(u, u.Username+" 流量已回到额度内") {
			next := "ok"
			if ratio >= 80 {
				next = "warn"
			}
			_ = db.TGNotifyPut(s.DB, key, now, next)
		}
	case ratio >= 80:
		if ok && state == "warn" && now-last < int64(tgRepeat/time.Second) {
			return
		}
		if s.tgAdminAndUser(u, u.Username+" 流量已用 "+strconv.Itoa(ratio)+"%") {
			_ = db.TGNotifyPut(s.DB, key, now, "warn")
		}
	default:
		if state != "ok" {
			_ = db.TGNotifyPut(s.DB, key, now, "ok")
		}
	}
}

func (s *Server) tgBackupFailed(err error) {
	if err == nil || !s.tgEnabled() || !s.tgAlert("tg_alert_backup") {
		return
	}
	day := db.ClockNow(s.DB).Format("2006-01-02")
	key := "backup.fail:" + day
	_, state, ok, getErr := db.TGNotifyGet(s.DB, key)
	if getErr != nil || (ok && state == "fail") {
		return
	}
	msg := err.Error()
	if len(msg) > 180 {
		msg = msg[:180]
	}
	if s.tgAdmin("定时备份失败："+msg) == nil {
		_ = db.TGNotifyPut(s.DB, key, time.Now().Unix(), "fail")
	}
}

func (s *Server) tgAdminAndUser(u *db.User, text string) bool {
	adminOK := s.tgAdmin(text) == nil
	userOK := false
	if u != nil {
		if chat, bound, err := db.TGChatByUser(s.DB, u.ID); err == nil && bound {
			userOK = s.tgSend(chat, text, nil) == nil
		}
	}
	return adminOK || userOK
}

func (s *Server) tgAdmin(text string) error {
	chat, ok := s.tgAdminChat()
	if !ok {
		return fmt.Errorf("请先点「用 Telegram 打开」连接你的 Telegram")
	}
	return s.tgSend(chat, text, nil)
}

func (s *Server) tgReply(chatID int64, text string) {
	_ = s.tgSend(chatID, text, nil)
}

func (s *Server) tgSend(chatID int64, text string, markup *tgMarkup) error {
	body := map[string]any{
		"chat_id":                  chatID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	if markup != nil {
		body["reply_markup"] = markup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.tgCall(ctx, "sendMessage", body, nil)
}

func (s *Server) tgEdit(chatID, messageID int64, text string, markup *tgMarkup) error {
	body := map[string]any{
		"chat_id":                  chatID,
		"message_id":               messageID,
		"text":                     text,
		"disable_web_page_preview": true,
	}
	if markup != nil {
		body["reply_markup"] = markup
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.tgCall(ctx, "editMessageText", body, nil)
}

func (s *Server) tgAnswer(callbackID, text string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.tgCall(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": callbackID,
		"text":              text,
	}, nil)
}

func (s *Server) tgRememberBot(ctx context.Context) {
	var me struct {
		Username string `json:"username"`
	}
	if err := s.tgCall(ctx, "getMe", map[string]any{}, &me); err != nil {
		return
	}
	name := strings.TrimPrefix(strings.TrimSpace(me.Username), "@")
	if name != "" {
		_ = db.SetSetting(s.DB, "tg_bot_username", name)
	}
}

func (s *Server) tgCall(ctx context.Context, method string, payload any, dest any) error {
	token := s.tgToken()
	if token == "" || strings.ContainsAny(token, " /\r\n\t?#") {
		return fmt.Errorf("请先填写 Bot Token")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	endpoint := s.tgBase() + "/bot" + token + "/" + method
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("连不上 Telegram")
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("连不上 Telegram。检查 API 根地址，或面板能否访问 api.telegram.org")
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("连不上 Telegram")
	}
	var wrap struct {
		OK          bool            `json:"ok"`
		Description string          `json:"description"`
		Result      json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(buf, &wrap); err != nil || !wrap.OK {
		desc := strings.TrimSpace(wrap.Description)
		return &tgAPIError{Description: desc, friendly: tgFriendly(desc)}
	}
	if dest != nil && len(wrap.Result) > 0 && string(wrap.Result) != "null" {
		if err := json.Unmarshal(wrap.Result, dest); err != nil {
			return err
		}
	}
	return nil
}

type tgAPIError struct {
	Description string
	friendly    string
}

func (e *tgAPIError) Error() string {
	if e == nil {
		return ""
	}
	if e.friendly != "" {
		return e.friendly
	}
	if strings.TrimSpace(e.Description) == "" {
		return "Telegram 没有接受这次请求"
	}
	return e.Description
}

func tgFriendly(desc string) string {
	d := strings.TrimSpace(desc)
	low := strings.ToLower(d)
	switch {
	case d == "":
		return "Telegram 没有接受这次请求"
	case strings.EqualFold(d, "Not Found") || low == "not found":
		return "Bot Token 不对。向 @BotFather 复制 123456:ABC... 这一串，不要带 bot 前缀或网址"
	case strings.Contains(low, "unauthorized"):
		return "Bot Token 已失效。向 @BotFather 重新申请后再保存"
	case strings.Contains(low, "webhook") || strings.Contains(low, "conflict"):
		return "这个机器人还挂着别的接收地址。再保存一次，或等一会儿再发命令"
	default:
		return d
	}
}

func tgWebhookConflict(err error) bool {
	var apiErr *tgAPIError
	if !errors.As(err, &apiErr) {
		return false
	}
	low := strings.ToLower(apiErr.Description)
	return strings.Contains(low, "webhook") || strings.Contains(low, "conflict")
}

func (s *Server) tgNoteErr(err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
		if token := s.tgToken(); token != "" {
			msg = strings.ReplaceAll(msg, token, "…")
		}
		msg = strings.TrimSpace(msg)
		if len(msg) > 180 {
			msg = msg[:180]
		}
	}
	prev, _ := db.GetSetting(s.DB, tgLastErrorKey)
	if strings.TrimSpace(prev) == msg {
		return
	}
	_ = db.SetSetting(s.DB, tgLastErrorKey, msg)
	if msg != "" {
		log.Printf("telegram: %s", msg)
	}
}

func (s *Server) tgDropWebhook() {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	if err := s.tgDeleteWebhook(ctx); err != nil {
		s.tgNoteErr(err)
	}
}

func (s *Server) tgDeleteWebhook(ctx context.Context) error {
	return s.tgCall(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
}

func (s *Server) tgBotName() string {
	name, _ := db.GetSetting(s.DB, "tg_bot_username")
	return strings.TrimPrefix(strings.TrimSpace(name), "@")
}

func (s *Server) tgOpenURL(ctx context.Context, code string) (string, error) {
	name := s.tgBotName()
	if name == "" {
		s.tgRememberBot(ctx)
		name = s.tgBotName()
	}
	if name == "" || strings.ContainsAny(name, "/?# \t") {
		return "", fmt.Errorf("还没有连上这个机器人。检查 Bot Token 后再试")
	}
	return "https://t.me/" + name + "?start=" + url.QueryEscape(code), nil
}

func normalizeTGToken(raw string) string {
	next := strings.TrimSpace(raw)
	if len(next) >= 4 && strings.EqualFold(next[:3], "bot") && next[3] >= '0' && next[3] <= '9' {
		next = next[3:]
	}
	return next
}

func validateTGToken(raw string) (string, error) {
	next := normalizeTGToken(raw)
	if next == "" || strings.ContainsAny(next, " /\r\n\t?#") {
		return "", fmt.Errorf("Bot Token 无效。向 @BotFather 复制 123456:ABC... 这一串，不要带网址")
	}
	id, secret, ok := strings.Cut(next, ":")
	if !ok || id == "" || secret == "" {
		return "", fmt.Errorf("Bot Token 无效。向 @BotFather 复制 123456:ABC... 这一串")
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", fmt.Errorf("Bot Token 无效。向 @BotFather 复制 123456:ABC... 这一串")
		}
	}
	return next, nil
}

func tgDigits(text string) bool {
	if len(text) != 6 {
		return false
	}
	for _, r := range text {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func parseTGChat(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("请填写管理员 Chat ID")
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("只支持私聊，Chat ID 应为正数")
	}
	return id, nil
}

func normalizeTGBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if strings.ContainsAny(raw, " \r\n\t") {
		return "", fmt.Errorf("API 根地址不能包含空白")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
		return "", fmt.Errorf("API 根地址无效")
	}
	u.RawQuery = ""
	u.Fragment = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func dayDelta(now time.Time, unix int64) int {
	loc := now.Location()
	exp := time.Unix(unix, 0).In(loc)
	nowDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	expDay := time.Date(exp.Year(), exp.Month(), exp.Day(), 0, 0, 0, 0, loc)
	return int(expDay.Sub(nowDay).Hours() / 24)
}

func tgBytes(n int64) string {
	if n < 0 {
		n = 0
	}
	units := []string{"B", "KB", "MB", "GB", "TB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return strconv.FormatInt(n, 10) + " B"
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

func (s *Server) saveTelegramSettings(enabled *bool, token, chat, base *string, server, cert, backup, expire, traffic *bool) error {
	curEnabled := db.SettingOn(s.DB, "tg_enabled", false)
	if enabled != nil {
		curEnabled = *enabled
	}
	curToken := s.tgToken()
	if token != nil && strings.TrimSpace(*token) != "" {
		next, err := validateTGToken(*token)
		if err != nil {
			return err
		}
		curToken = next
	}
	curChat := ""
	if v, _ := db.GetSetting(s.DB, "tg_admin_chat"); true {
		curChat = strings.TrimSpace(v)
	}
	if chat != nil {
		curChat = strings.TrimSpace(*chat)
	}
	if curChat != "" {
		if _, err := parseTGChat(curChat); err != nil {
			return err
		}
	}
	curBase, _ := db.GetSetting(s.DB, "tg_api_base")
	if base != nil {
		next, err := normalizeTGBase(*base)
		if err != nil {
			return err
		}
		curBase = next
	}
	if curEnabled && curToken == "" {
		return fmt.Errorf("请先填写 Bot Token")
	}
	rawStored, _ := db.GetSetting(s.DB, "tg_bot_token")
	tokenChanged := curToken != "" && curToken != normalizeTGToken(rawStored)
	_ = db.SetSetting(s.DB, "tg_enabled", boolSetting(curEnabled))
	rewritten := curToken != "" && curToken != strings.TrimSpace(rawStored)
	if rewritten {
		_ = db.SetSetting(s.DB, "tg_bot_token", curToken)
	}
	if tokenChanged {
		_ = db.SetSetting(s.DB, "tg_update_offset", "0")
	}
	if chat != nil {
		_ = db.SetSetting(s.DB, "tg_admin_chat", curChat)
	}
	if base != nil {
		_ = db.SetSetting(s.DB, "tg_api_base", curBase)
	}
	writeBool := func(key string, v *bool) {
		if v == nil {
			return
		}
		_ = db.SetSetting(s.DB, key, boolSetting(*v))
	}
	writeBool("tg_alert_server", server)
	writeBool("tg_alert_cert", cert)
	writeBool("tg_alert_backup", backup)
	writeBool("tg_alert_expire", expire)
	writeBool("tg_alert_traffic", traffic)
	if curToken != "" && (rewritten || token != nil || enabled != nil || base != nil) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			_ = s.tgDeleteWebhook(ctx)
			s.tgRememberBot(ctx)
		}()
	}
	return nil
}

func boolSetting(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

func (s *Server) tgPublicSettings() map[string]any {
	chat, _ := db.GetSetting(s.DB, "tg_admin_chat")
	base, _ := db.GetSetting(s.DB, "tg_api_base")
	name, _ := db.GetSetting(s.DB, "tg_bot_username")
	last, _ := db.GetSetting(s.DB, tgLastErrorKey)
	return map[string]any{
		"tg_enabled":       db.SettingOn(s.DB, "tg_enabled", false),
		"tg_admin_chat":    strings.TrimSpace(chat),
		"tg_api_base":      strings.TrimSpace(base),
		"tg_bot_token_set": s.tgToken() != "",
		"tg_bot_username":  strings.TrimPrefix(strings.TrimSpace(name), "@"),
		"tg_last_error":    strings.TrimSpace(last),
		"tg_alert_server":  db.SettingOn(s.DB, "tg_alert_server", true),
		"tg_alert_cert":    db.SettingOn(s.DB, "tg_alert_cert", true),
		"tg_alert_backup":  db.SettingOn(s.DB, "tg_alert_backup", true),
		"tg_alert_expire":  db.SettingOn(s.DB, "tg_alert_expire", true),
		"tg_alert_traffic": db.SettingOn(s.DB, "tg_alert_traffic", true),
	}
}
