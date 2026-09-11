package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"testing"

	"liking/internal/db"
)

func decodeRes(t *testing.T, res *http.Response, v any) {
	t.Helper()
	defer res.Body.Close()
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("status %d %s", res.StatusCode, body)
	}
	if v != nil {
		if err := json.Unmarshal(body, v); err != nil {
			t.Fatalf("json: %v %s", err, body)
		}
	}
}

func TestLoginAndServerAndInbound(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
	defer srv.Close()

	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}

	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	body, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "10.0.0.1"})
	res, err = c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Server struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		} `json:"server"`
		Install string `json:"install"`
	}
	decodeRes(t, res, &created)
	if created.Server.ID == 0 || created.Server.Token == "" || created.Install == "" {
		t.Fatal("server")
	}

	inBody, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "vless-1",
		"profile":   "vless-reality-vision",
		"port":      443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/inbounds")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Inbounds []map[string]any `json:"inbounds"`
	}
	decodeRes(t, res, &listing)
	if len(listing.Inbounds) != 1 {
		t.Fatalf("list %d", len(listing.Inbounds))
	}
	st, _ := listing.Inbounds[0]["settings"].(map[string]any)
	if st["public_key"] == nil && st["private_key"] == nil {
		t.Fatalf("settings %+v", st)
	}

	pkgBody, _ := json.Marshal(map[string]any{"name": "std", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway"})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Package struct {
			ID int64 `json:"id"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg)

	uBody, _ := json.Marshal(map[string]any{
		"username": "alice", "password": "alice12", "package_id": pkg.Package.ID, "days": 30,
	})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdUser struct {
		User struct {
			ID       int64  `json:"id"`
			SubToken string `json:"sub_token"`
		} `json:"user"`
	}
	decodeRes(t, res, &createdUser)
	if createdUser.User.SubToken == "" {
		t.Fatal("sub token")
	}

	res, err = c.Get(ts.URL + "/api/sub/" + createdUser.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("sub %d %s", res.StatusCode, b)
	}

	head, err := http.NewRequest(http.MethodHead, ts.URL+"/v1/agent-bin?os=linux&arch=amd64", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Do(head)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusNotFound && res.StatusCode != http.StatusOK {
		t.Fatalf("agent-bin HEAD %d", res.StatusCode)
	}

	res, err = c.Get(ts.URL + "/api/servers")
	if err != nil {
		t.Fatal(err)
	}
	var servers struct {
		Servers []map[string]any `json:"servers"`
	}
	decodeRes(t, res, &servers)
	if len(servers.Servers) != 1 {
		t.Fatalf("servers %d", len(servers.Servers))
	}
	if tok, _ := servers.Servers[0]["token"].(string); tok != "" {
		t.Fatal("list should not leak token")
	}
	if errMsg, _ := servers.Servers[0]["last_error"].(string); errMsg != "" {
		t.Fatalf("last_error %q", errMsg)
	}

	u, _ := url.Parse(ts.URL)
	if len(jar.Cookies(u)) == 0 {
		t.Fatal("no session cookie")
	}
}

func TestHealthz(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	srv.Router().ServeHTTP(r, req)
	if r.Code != 200 {
		t.Fatalf("health %d", r.Code)
	}
}
