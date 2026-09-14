package server

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"liking/internal/db"
)

func setupAdminPanel(t *testing.T) (*sql.DB, *httptest.Server, *http.Client) {
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
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { srv.Close() })
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	return d, ts, c
}

func TestTrustedForwardedIgnoresPublicSpoof(t *testing.T) {
	var got string
	h := trustedForwarded(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.RemoteAddr
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "203.0.113.10:1234"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(got, "203.0.113.10:") {
		t.Fatalf("public peer trusted spoofed headers: %s", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Forwarded-For", "9.9.9.9, 1.2.3.4")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(got, "1.2.3.4:") {
		t.Fatalf("loopback XFF rightmost: %s", got)
	}

	req = httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "127.0.0.1:1234"
	req.Header.Set("X-Real-IP", "8.8.8.8")
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	h.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.HasPrefix(got, "8.8.8.8:") {
		t.Fatalf("loopback X-Real-IP: %s", got)
	}
}

func TestRejectSecondAdmin(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	body, _ := json.Marshal(map[string]any{"username": "root", "password": "secret12", "role": "admin"})
	res, err := c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("second admin %d %s", res.StatusCode, b)
	}
}

func TestEmptyPackageRejected(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	body, _ := json.Marshal(map[string]any{"name": "empty", "traffic_bytes": 0, "cycle_days": 0, "direction": "oneway"})
	res, err := c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty package %d %s", res.StatusCode, b)
	}
}

func TestTOTPBeginRequiresPassword(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	empty, _ := json.Marshal(map[string]string{})
	res, err := c.Post(ts.URL+"/api/totp/begin", "application/json", bytes.NewReader(empty))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("totp begin without password %d %s", res.StatusCode, b)
	}

	ok, _ := json.Marshal(map[string]string{"password": "secret12"})
	res, err = c.Post(ts.URL+"/api/totp/begin", "application/json", bytes.NewReader(ok))
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Secret string `json:"secret"`
	}
	decodeRes(t, res, &out)
	if out.Secret == "" {
		t.Fatal("missing totp secret")
	}
}

func TestPasswordChangeDropsOtherSessions(t *testing.T) {
	_, ts, admin := setupAdminPanel(t)
	uBody, _ := json.Marshal(map[string]any{"username": "bob", "password": "bobpass1"})
	res, err := admin.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	loginBob := func() *http.Client {
		t.Helper()
		jar, _ := cookiejar.New(nil)
		c := &http.Client{Jar: jar}
		body, _ := json.Marshal(map[string]string{"username": "bob", "password": "bobpass1"})
		res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		decodeRes(t, res, nil)
		return c
	}
	keep := loginBob()
	drop := loginBob()
	pw, _ := json.Marshal(map[string]string{"old": "bobpass1", "new": "bobpass2"})
	res, err = keep.Post(ts.URL+"/api/password", "application/json", bytes.NewReader(pw))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = keep.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = drop.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusUnauthorized {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("other session survived %d %s", res.StatusCode, b)
	}
	res.Body.Close()
}

func TestForgetPasswordAndLoginClearsPlain(t *testing.T) {
	_, ts, admin := setupAdminPanel(t)
	uBody, _ := json.Marshal(map[string]any{"username": "carol", "password": "carol12"})
	res, err := admin.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		User struct {
			ID       int64  `json:"id"`
			Password string `json:"password"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if created.User.ID == 0 || created.User.Password != "carol12" {
		t.Fatalf("created %+v", created)
	}

	forget, _ := json.Marshal(map[string]any{})
	res, err = admin.Post(ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10)+"/forget-password", "application/json", bytes.NewReader(forget))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = admin.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Users []struct {
			ID       int64  `json:"id"`
			Password string `json:"password"`
		} `json:"users"`
	}
	decodeRes(t, res, &listed)
	for _, u := range listed.Users {
		if u.ID == created.User.ID && u.Password != "" {
			t.Fatalf("plain still listed %q", u.Password)
		}
	}

	uBody, _ = json.Marshal(map[string]any{"username": "dave", "password": "dave123"})
	res, err = admin.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &created)

	jar, _ := cookiejar.New(nil)
	dave := &http.Client{Jar: jar}
	login, _ := json.Marshal(map[string]string{"username": "dave", "password": "dave123"})
	res, err = dave.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = admin.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	listed.Users = nil
	decodeRes(t, res, &listed)
	for _, u := range listed.Users {
		if u.ID == created.User.ID && u.Password != "" {
			t.Fatalf("plain survived login %q", u.Password)
		}
	}
}

func TestBackupPostRequiresPassword(t *testing.T) {
	_, ts, c := setupAdminPanel(t)
	res, err := c.Get(ts.URL + "/api/backup")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode == 200 {
		t.Fatal("GET backup")
	}

	empty, _ := json.Marshal(map[string]string{})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/backup", bytes.NewReader(empty))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("backup no password %d %s", res.StatusCode, b)
	}

	wrong, _ := json.Marshal(map[string]string{"password": "nope"})
	req, err = http.NewRequest(http.MethodPost, ts.URL+"/api/backup", bytes.NewReader(wrong))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ = io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("backup wrong password %d %s", res.StatusCode, b)
	}
}

func TestJSONBodyLimit(t *testing.T) {
	_, ts, _ := setupAdminPanel(t)
	big := bytes.Repeat([]byte("a"), maxJSONBody+16)
	body := append(append([]byte(`{"username":"admin","password":"`), big...), []byte(`"}`)...)
	res, err := http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("huge json %d %s", res.StatusCode, b)
	}
}
