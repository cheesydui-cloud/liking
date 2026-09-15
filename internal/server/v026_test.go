package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"liking/internal/db"
	"liking/internal/wsproto"
)

func TestFindAgentBinaryRejectsTraversal(t *testing.T) {
	if findAgentBinary("linux", "amd64/../panel.db") != "" {
		t.Fatal("arch traversal")
	}
	if findAgentBinary("linux/../etc", "amd64") != "" {
		t.Fatal("os traversal")
	}
	if findAgentBinary("darwin", "amd64") != "" {
		t.Fatal("os allowlist")
	}
	if findAgentBinary("linux", "arm") != "" {
		t.Fatal("arch allowlist")
	}
	if findAgentBinary("linux", "amd64\\x") != "" {
		t.Fatal("backslash")
	}
}

func TestApplyStatsRejectsForeignAndNegative(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, _ := db.RandomHex(8)
	srv, err := db.CreateServer(d, "a", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	tok2, _ := db.RandomHex(8)
	srv2, err := db.CreateServer(d, "b", "2.2.2.2", tok2)
	if err != nil {
		t.Fatal(err)
	}
	in, err := db.CreateInbound(d, &db.Inbound{
		ServerID: srv.ID, Name: "n", Profile: "vless-reality", Protocol: "vless",
		Network: "tcp", Security: "reality", Core: "xray", Listen: "0.0.0.0",
		Port: 443, Enabled: true, Settings: "{}", LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "bob", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	email := db.EmailFor(u.ID, in.ID)
	if err := db.UpsertClient(d, &db.Client{InboundID: in.ID, UserID: u.ID, Email: email, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	h := NewHub(d)
	h.applyStats(srv2.ID, []wsproto.Sample{{Email: email, Up: 10000, Down: 20000}})
	got, err := db.GetUser(d, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.UsedUp != 0 || got.UsedDown != 0 {
		t.Fatalf("foreign stats credited %+v", got)
	}
	h.applyStats(srv.ID, []wsproto.Sample{{Email: email, Up: -1, Down: 20}})
	got, _ = db.GetUser(d, u.ID)
	if got.UsedUp != 0 || got.UsedDown != 0 {
		t.Fatalf("negative credited %+v", got)
	}
	h.applyStats(srv.ID, []wsproto.Sample{{Email: email, Up: 10, Down: 20}})
	got, _ = db.GetUser(d, u.ID)
	if got.UsedUp != 10 || got.UsedDown != 20 {
		t.Fatalf("own stats %+v", got)
	}
}

func TestLoginCSRFCrossSite(t *testing.T) {
	_, ts, _ := setupAdminPanel(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	req.Header.Set("Origin", "https://evil.example")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("csrf %d", res.StatusCode)
	}
}

func TestDecodeJSONRequiresContentType(t *testing.T) {
	_, ts, _ := setupAdminPanel(t)
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("no content-type %d", res.StatusCode)
	}
}

func TestSecureRequestAfterXFFRewrite(t *testing.T) {
	var got bool
	h := trustedForwarded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = isSecureRequest(r)
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Real-IP", "203.0.113.9")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !got {
		t.Fatal("https behind nginx should stay secure after XFF rewrite")
	}
}

func TestShouldResetClampsShortMonth(t *testing.T) {
	u := &db.User{TrafficResetDay: 31, CycleStart: time.Date(2026, 1, 31, 0, 0, 0, 0, time.UTC).Unix()}
	feb := time.Date(2026, 2, 28, 12, 0, 0, 0, time.UTC)
	if !shouldResetAt(u, nil, feb) {
		t.Fatal("feb 28 should reset when day is 31")
	}
	mar1 := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	if shouldResetAt(u, nil, mar1) {
		t.Fatal("mar 1 is not the clamped reset day")
	}
}

func TestSoonerUnix(t *testing.T) {
	if soonerUnix(10, 20) != 10 || soonerUnix(20, 10) != 10 {
		t.Fatal("min")
	}
	if soonerUnix(0, 20) != 20 || soonerUnix(10, 0) != 10 {
		t.Fatal("zero means unset")
	}
}

func TestLoginLimiterPrunesEmpty(t *testing.T) {
	l := newLoginLimiter()
	l.fails["1.1.1.1"] = []time.Time{time.Now().Add(-20 * time.Minute)}
	if !l.Allow("1.1.1.1") {
		t.Fatal("old fails should allow")
	}
	if _, ok := l.fails["1.1.1.1"]; ok {
		t.Fatal("empty key kept")
	}
}

func TestReconcileOnConnectRetriesLastError(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, _ := db.RandomHex(8)
	srv, err := db.CreateServer(d, "n1", "1.1.1.1", tok)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetServerRev(d, srv.ID, "rev-1"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetServerApplyError(d, srv.ID, errApply("boom")); err != nil {
		t.Fatal(err)
	}
	called := make(chan int64, 1)
	h := NewHub(d)
	h.Redispatch = func(ids []int64) {
		called <- ids[0]
	}
	h.reconcileOnConnect(srv.ID, "rev-1")
	select {
	case id := <-called:
		if id != srv.ID {
			t.Fatalf("id %d", id)
		}
	case <-time.After(time.Second):
		t.Fatal("expected redispatch when last_error set")
	}
}

type errApply string

func (e errApply) Error() string { return string(e) }
