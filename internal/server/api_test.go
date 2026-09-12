package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
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
		"port":      8443,
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

	pkgBody, _ := json.Marshal(map[string]any{
		"name": "std", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway",
		"server_ids": []int64{created.Server.ID},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Package struct {
			ID        int64   `json:"id"`
			ServerIDs []int64 `json:"server_ids"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg)
	if len(pkg.Package.ServerIDs) != 1 || pkg.Package.ServerIDs[0] != created.Server.ID {
		t.Fatalf("package servers %+v", pkg.Package.ServerIDs)
	}

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
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("sub %d %s", res.StatusCode, b)
	}
	subBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	decoded, err := base64.StdEncoding.DecodeString(string(subBody))
	if err != nil {
		t.Fatalf("sub b64 %v %s", err, subBody)
	}
	if !strings.Contains(string(decoded), "vless://") || !strings.Contains(string(decoded), ":8443") {
		t.Fatalf("sub uri %s", decoded)
	}

	var listed struct {
		Inbounds []struct {
			ID   int64 `json:"id"`
			Port int   `json:"port"`
		} `json:"inbounds"`
	}
	res, err = c.Get(ts.URL + "/api/inbounds")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &listed)
	if len(listed.Inbounds) != 1 {
		t.Fatalf("inbounds %d", len(listed.Inbounds))
	}
	upBody, _ := json.Marshal(map[string]any{"port": 9443, "name": "vless-1"})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/inbounds/"+strconv.FormatInt(listed.Inbounds[0].ID, 10), bytes.NewReader(upBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

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

func TestInboundShareURI(t *testing.T) {
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
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &created)

	inBody, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "vless-1",
		"profile":   "vless-reality-vision",
		"port":      8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdIn struct {
		Inbound struct {
			ID int64 `json:"id"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &createdIn)

	res, err = c.Get(ts.URL + "/api/inbounds/" + strconv.FormatInt(createdIn.Inbound.ID, 10) + "/share")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("share without user %d", res.StatusCode)
	}
	res.Body.Close()

	pkgBody, _ := json.Marshal(map[string]any{
		"name": "std", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway",
		"server_ids": []int64{created.Server.ID},
	})
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
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/inbounds/" + strconv.FormatInt(createdIn.Inbound.ID, 10) + "/share")
	if err != nil {
		t.Fatal(err)
	}
	var share struct {
		URI      string `json:"uri"`
		Username string `json:"username"`
		Profile  string `json:"profile"`
	}
	decodeRes(t, res, &share)
	if share.Username != "alice" || share.Profile != "vless-reality-vision" {
		t.Fatalf("share meta %+v", share)
	}
	if !strings.HasPrefix(share.URI, "vless://") || !strings.Contains(share.URI, "10.0.0.1:8443") {
		t.Fatalf("share uri %s", share.URI)
	}
}

func TestCreateMieruWhenOnlyXrayReported(t *testing.T) {
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
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &created)
	if err := db.MarkServerOnline(d, created.Server.ID, "0.1.9", "linux", "amd64", "10.0.0.1", []string{"xray"}); err != nil {
		t.Fatal(err)
	}

	inBody, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "mieru-1",
		"profile":   "mieru",
		"port":      8444,
		"settings":  map[string]any{"transport": "TCP"},
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
}

func TestParseBusyPorts(t *testing.T) {
	got := parseBusyPorts("已跳过占用端口: 443, 26011")
	if len(got) != 2 || got[0] != 443 || got[1] != 26011 {
		t.Fatalf("%v", got)
	}
	got = parseBusyPorts("端口 443 已被占用")
	if len(got) != 1 || got[0] != 443 {
		t.Fatalf("%v", got)
	}
}

func TestUserPasswordAndExtend(t *testing.T) {
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
	pkg, err := db.CreatePackage(d, "std", 10*1024*1024*1024, 30, 0, "oneway")
	if err != nil {
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

	body, _ := json.Marshal(map[string]any{"username": "bob", "package_id": pkg.ID, "days": 30})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Password string `json:"password"`
		User     struct {
			ID        int64 `json:"id"`
			ExpiresAt int64 `json:"expires_at"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if len(created.Password) < 6 || created.User.ID == 0 || created.User.ExpiresAt == 0 {
		t.Fatalf("generated %+v", created)
	}
	before := created.User.ExpiresAt
	ext, _ := json.Marshal(map[string]any{"extend_days": 30, "remark": ""})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(ext))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var updated struct {
		User struct {
			ExpiresAt int64 `json:"expires_at"`
		} `json:"user"`
	}
	decodeRes(t, res, &updated)
	if updated.User.ExpiresAt-before < 29*24*3600 {
		t.Fatalf("extend %d -> %d", before, updated.User.ExpiresAt)
	}

	pwBody, _ := json.Marshal(map[string]string{"password": ""})
	res, err = c.Post(ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10)+"/password", "application/json", bytes.NewReader(pwBody))
	if err != nil {
		t.Fatal(err)
	}
	var rotated struct {
		Password string `json:"password"`
	}
	decodeRes(t, res, &rotated)
	if len(rotated.Password) < 6 {
		t.Fatalf("random password %q", rotated.Password)
	}

	res, err = c.Get(ts.URL + "/api/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	var dash struct {
		Members  int   `json:"members"`
		Packages int   `json:"packages"`
		Used     int64 `json:"used_bytes"`
	}
	decodeRes(t, res, &dash)
	if dash.Members < 1 || dash.Packages < 1 {
		t.Fatalf("dashboard %+v", dash)
	}

	jar2, _ := cookiejar.New(nil)
	c2 := &http.Client{Jar: jar2}
	login2, _ := json.Marshal(map[string]string{"username": "bob", "password": rotated.Password})
	res, err = c2.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login2))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c2.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		User struct {
			TrafficCap int64 `json:"traffic_cap"`
		} `json:"user"`
		Sub map[string]string `json:"sub"`
	}
	decodeRes(t, res, &me)
	if me.User.TrafficCap != 10*1024*1024*1024 || me.Sub["auto"] == "" || me.Sub["clash"] == "" {
		t.Fatalf("me %+v", me)
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
