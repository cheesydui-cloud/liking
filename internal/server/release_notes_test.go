package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

	"liking/internal/db"
)

func TestPackageRemarkStaysAdminOnly(t *testing.T) {
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
	t.Cleanup(srv.Close)
	ts := httptest.NewServer(srv.Router())
	t.Cleanup(ts.Close)

	admin := panelLogin(t, ts.URL, "admin", "secret12")
	srvBody, _ := json.Marshal(map[string]string{"name": "edge", "public_host": "203.0.113.9"})
	res, err := admin.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(srvBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)

	inBody, _ := json.Marshal(map[string]any{
		"server_id": createdSrv.Server.ID,
		"name":      "vless-1",
		"profile":   "vless-reality-vision",
		"port":      8443,
	})
	res, err = admin.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdIn struct {
		Inbound struct {
			ID int64 `json:"id"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &createdIn)
	if createdIn.Inbound.ID == 0 {
		t.Fatal("missing inbound")
	}

	const note = "PKG-NOTE-7k2m-管理员专属"
	pkgBody, _ := json.Marshal(map[string]any{
		"name":          "std",
		"remark":        "  " + note + "  ",
		"traffic_bytes": 0,
		"direction":     "oneway",
		"inbound_ids":   []int64{createdIn.Inbound.ID},
	})
	res, err = admin.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdPkg struct {
		Package struct {
			ID     int64  `json:"id"`
			Remark string `json:"remark"`
		} `json:"package"`
	}
	decodeRes(t, res, &createdPkg)
	if createdPkg.Package.ID == 0 || createdPkg.Package.Remark != note {
		t.Fatalf("package remark %+v", createdPkg.Package)
	}

	userBody, _ := json.Marshal(map[string]any{
		"username":   "alice",
		"password":   "secret12",
		"package_id": createdPkg.Package.ID,
	})
	res, err = admin.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(userBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdUser struct {
		User struct {
			ID       int64  `json:"id"`
			SubToken string `json:"sub_token"`
			Remark   string `json:"remark"`
		} `json:"user"`
	}
	decodeRes(t, res, &createdUser)
	if createdUser.User.SubToken == "" {
		t.Fatal("missing sub token")
	}
	if err := db.TGBind(d, createdUser.User.ID, 88001122); err != nil {
		t.Fatal(err)
	}

	alice := panelLogin(t, ts.URL, "alice", "secret12")
	res, err = alice.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	meRaw := readBody(t, res)
	if strings.Contains(meRaw, note) || strings.Contains(meRaw, "tg_bound") || strings.Contains(meRaw, "88001122") {
		t.Fatalf("me leaked %s", meRaw)
	}
	res, err = alice.Get(ts.URL + "/api/packages")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		t.Fatalf("user packages %d", res.StatusCode)
	}
	res.Body.Close()
	for _, path := range []string{"", "/clash", "/singbox", "/uri"} {
		res, err = http.Get(ts.URL + "/api/sub/" + createdUser.User.SubToken + path)
		if err != nil {
			t.Fatal(err)
		}
		raw := readBody(t, res)
		if strings.Contains(raw, note) {
			t.Fatalf("sub %s leaked remark %s", path, raw)
		}
	}

	res, err = admin.Get(ts.URL + "/api/packages")
	if err != nil {
		t.Fatal(err)
	}
	pkgRaw := readBody(t, res)
	if !strings.Contains(pkgRaw, note) {
		t.Fatalf("admin list missing remark %s", pkgRaw)
	}

	omit, _ := json.Marshal(map[string]any{"name": "std-kept"})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/packages/"+itoa(createdPkg.Package.ID), bytes.NewReader(omit))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var updated struct {
		Package struct {
			Name   string `json:"name"`
			Remark string `json:"remark"`
		} `json:"package"`
	}
	decodeRes(t, res, &updated)
	if updated.Package.Name != "std-kept" || updated.Package.Remark != note {
		t.Fatalf("omit cleared remark %+v", updated.Package)
	}

	long := strings.Repeat("备", 201)
	bad, _ := json.Marshal(map[string]string{"remark": long})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/packages/"+itoa(createdPkg.Package.ID), bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = admin.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("long remark %d", res.StatusCode)
	}
	res.Body.Close()

	res, err = admin.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	usersRaw := readBody(t, res)
	if strings.Contains(usersRaw, "88001122") {
		t.Fatalf("user list leaked chat id %s", usersRaw)
	}
	var users struct {
		Users []struct {
			Username string `json:"username"`
			TGBound  bool   `json:"tg_bound"`
		} `json:"users"`
	}
	if err := json.Unmarshal([]byte(usersRaw), &users); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, u := range users.Users {
		seen[u.Username] = u.TGBound
	}
	if !seen["alice"] || seen["admin"] {
		t.Fatalf("tg marks %+v", seen)
	}
}

func panelLogin(t *testing.T, base, user, pass string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	body, _ := json.Marshal(map[string]string{"username": user, "password": pass})
	res, err := c.Post(base+"/api/login", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	return c
}

func readBody(t *testing.T, res *http.Response) string {
	t.Helper()
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode >= 400 {
		t.Fatalf("status %d %s", res.StatusCode, b)
	}
	return string(b)
}
