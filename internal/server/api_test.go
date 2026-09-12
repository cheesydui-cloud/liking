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

	"liking/internal/certs"
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
		SubToken string `json:"sub_token"`
	}
	decodeRes(t, res, &share)
	if share.Username != "alice" || share.Profile != "vless-reality-vision" || share.SubToken == "" {
		t.Fatalf("share meta %+v", share)
	}
	if !strings.HasPrefix(share.URI, "vless://") || !strings.Contains(share.URI, "10.0.0.1:8443") {
		t.Fatalf("share uri %s", share.URI)
	}
}

func TestCreateInboundRandomPort(t *testing.T) {
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

	post := func() (int, int, string) {
		t.Helper()
		inBody, _ := json.Marshal(map[string]any{
			"server_id": created.Server.ID,
			"profile":   "ss2022",
		})
		res, err := c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Inbound struct {
				Name string `json:"name"`
				Port int    `json:"port"`
			} `json:"inbound"`
		}
		decodeRes(t, res, &out)
		return out.Inbound.Port, res.StatusCode, out.Inbound.Name
	}
	p1, _, n1 := post()
	if p1 < 10000 || p1 > 59999 {
		t.Fatalf("port %d", p1)
	}
	if n1 != "ss2022-"+strconv.Itoa(p1) {
		t.Fatalf("name %s", n1)
	}
	p2, _, _ := post()
	if p2 == p1 || p2 < 10000 || p2 > 59999 {
		t.Fatalf("ports %d %d", p1, p2)
	}

	taken, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"profile":   "ss2022",
		"port":      p1,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(taken))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("taken %d", res.StatusCode)
	}
	res.Body.Close()
}

func TestInboundShareSS2022UsesConnectIP(t *testing.T) {
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

	body, _ := json.Marshal(map[string]string{"name": "jp"})
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
	if _, err := d.Exec(`UPDATE servers SET connect_ip=? WHERE id=?`, "177.5.54.5", created.Server.ID); err != nil {
		t.Fatal(err)
	}

	inBody, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "jp-ss",
		"profile":   "ss2022",
		"port":      8789,
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
		URI     string `json:"uri"`
		Profile string `json:"profile"`
	}
	decodeRes(t, res, &share)
	if share.Profile != "ss2022" || !strings.HasPrefix(share.URI, "ss://") || !strings.Contains(share.URI, "@177.5.54.5:8789") {
		t.Fatalf("share %+v", share)
	}
	userinfo := strings.TrimPrefix(share.URI, "ss://")
	userinfo = userinfo[:strings.Index(userinfo, "@")]
	raw, err := base64.RawURLEncoding.DecodeString(userinfo)
	if err != nil {
		t.Fatalf("sip002 %v %s", err, share.URI)
	}
	if !strings.HasPrefix(string(raw), "2022-blake3-aes-128-gcm:") || strings.Contains(string(raw), "%") {
		t.Fatalf("decoded %s", raw)
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

	if err := db.AddUserTraffic(d, created.User.ID, 111, 222); err != nil {
		t.Fatal(err)
	}

	edit, _ := json.Marshal(map[string]any{
		"username":      "robert",
		"remark":        "vip",
		"package_id":    pkg.ID,
		"traffic_limit": int64(5 * 1024 * 1024 * 1024),
		"password":      "newpass12",
	})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(edit))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var edited struct {
		User struct {
			Username     string `json:"username"`
			Remark       string `json:"remark"`
			TrafficLimit *int64 `json:"traffic_limit"`
			UsedUp       int64  `json:"used_up"`
			UsedDown     int64  `json:"used_down"`
			PackageID    *int64 `json:"package_id"`
		} `json:"user"`
	}
	decodeRes(t, res, &edited)
	if edited.User.Username != "robert" || edited.User.Remark != "vip" || edited.User.TrafficLimit == nil || *edited.User.TrafficLimit != 5*1024*1024*1024 {
		t.Fatalf("edit %+v", edited.User)
	}
	if edited.User.UsedUp != 111 || edited.User.UsedDown != 222 {
		t.Fatalf("same package wiped traffic %+v", edited.User)
	}
	keep, _ := json.Marshal(map[string]any{"enabled": true})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(keep))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &edited)
	if edited.User.Username != "robert" || edited.User.TrafficLimit == nil || *edited.User.TrafficLimit != 5*1024*1024*1024 {
		t.Fatalf("partial %+v", edited.User)
	}
	clash, _ := json.Marshal(map[string]any{"username": "admin"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(clash))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusConflict {
		t.Fatalf("username clash %d", res.StatusCode)
	}
	res.Body.Close()

	clearLim, _ := json.Marshal(map[string]any{"traffic_limit": nil})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(clearLim))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &edited)
	if edited.User.TrafficLimit != nil {
		t.Fatalf("clear limit %+v", edited.User)
	}

	pkg2, err := db.CreatePackage(d, "pro", 20*1024*1024*1024, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	rebind, _ := json.Marshal(map[string]any{"package_id": pkg2.ID})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(rebind))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &edited)
	if edited.User.PackageID == nil || *edited.User.PackageID != pkg2.ID {
		t.Fatalf("rebind %+v", edited.User)
	}
	if edited.User.UsedUp != 0 || edited.User.UsedDown != 0 {
		t.Fatalf("new package should reset traffic %+v", edited.User)
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
	login2, _ := json.Marshal(map[string]string{"username": "robert", "password": "newpass12"})
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
	if me.User.TrafficCap != 20*1024*1024*1024 || me.Sub["auto"] == "" || me.Sub["clash"] == "" {
		t.Fatalf("me %+v", me)
	}
}

func TestServerTrafficLimit(t *testing.T) {
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
			ID           int64  `json:"id"`
			PublicHost   string `json:"public_host"`
			TrafficLimit int64  `json:"traffic_limit"`
		} `json:"server"`
	}
	decodeRes(t, res, &created)
	if created.Server.ID == 0 || created.Server.TrafficLimit != 0 {
		t.Fatalf("%+v", created.Server)
	}
	lim := int64(2 * 1024 * 1024 * 1024)
	upd, _ := json.Marshal(map[string]any{"traffic_limit": lim})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/servers/"+strconv.FormatInt(created.Server.ID, 10), bytes.NewReader(upd))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var updated struct {
		Server struct {
			PublicHost   string `json:"public_host"`
			TrafficLimit int64  `json:"traffic_limit"`
		} `json:"server"`
	}
	decodeRes(t, res, &updated)
	if updated.Server.TrafficLimit != lim || updated.Server.PublicHost != "10.0.0.1" {
		t.Fatalf("partial %+v", updated.Server)
	}
}

func TestCertsAndSettings(t *testing.T) {
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

	res, err = c.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	var st map[string]any
	decodeRes(t, res, &st)
	if st["cf_api_token_set"] != false {
		t.Fatalf("token set %+v", st)
	}
	if _, ok := st["cf_api_token"]; ok {
		t.Fatal("token leaked")
	}

	put, _ := json.Marshal(map[string]string{
		"panel_name": "liking", "panel_url": "", "acme_email": "a@b.com", "cf_api_token": "secret-token",
	})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/settings", bytes.NewReader(put))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	st = map[string]any{}
	decodeRes(t, res, &st)
	if st["cf_api_token_set"] != true || st["acme_email"] != "a@b.com" {
		t.Fatalf("%+v", st)
	}
	if _, ok := st["cf_api_token"]; ok {
		t.Fatal("token leaked after save")
	}

	onlyPanel, _ := json.Marshal(map[string]string{"panel_name": "liking-x"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/settings", bytes.NewReader(onlyPanel))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	st = map[string]any{}
	decodeRes(t, res, &st)
	if st["panel_name"] != "liking-x" || st["acme_email"] != "a@b.com" || st["cf_api_token_set"] != true {
		t.Fatalf("partial settings %+v", st)
	}

	body, _ := json.Marshal(map[string]string{"name": "self", "domains": "self.example.com"})
	res, err = c.Post(ts.URL+"/api/certs/selfsign", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Cert struct {
			ID        int64  `json:"id"`
			Source    string `json:"source"`
			ExpiresAt int64  `json:"expires_at"`
			KeyPEM    string `json:"key_pem"`
			Domains   string `json:"domains"`
		} `json:"cert"`
	}
	decodeRes(t, res, &created)
	if created.Cert.ID == 0 || created.Cert.Source != "selfsigned" || created.Cert.ExpiresAt == 0 || created.Cert.KeyPEM != "" {
		t.Fatalf("%+v", created.Cert)
	}

	res, err = c.Post(ts.URL+"/api/certs/"+strconv.FormatInt(created.Cert.ID, 10)+"/renew", "application/json", bytes.NewReader([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("renew selfsigned %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	pemCert, pemKey, _, err := certs.SelfSign("up.example.com", []string{"up.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	up, _ := json.Marshal(map[string]string{"name": "up", "cert_pem": pemCert, "key_pem": pemKey})
	res, err = c.Post(ts.URL+"/api/certs", "application/json", bytes.NewReader(up))
	if err != nil {
		t.Fatal(err)
	}
	var uploaded struct {
		Cert struct {
			Source    string `json:"source"`
			ExpiresAt int64  `json:"expires_at"`
			Domains   string `json:"domains"`
		} `json:"cert"`
	}
	decodeRes(t, res, &uploaded)
	if uploaded.Cert.Source != "upload" || uploaded.Cert.ExpiresAt == 0 || uploaded.Cert.Domains == "" {
		t.Fatalf("%+v", uploaded.Cert)
	}

	badACME, _ := json.Marshal(map[string]string{"domains": "nodot"})
	res, err = c.Post(ts.URL+"/api/certs/acme", "application/json", bytes.NewReader(badACME))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("acme %d %s", res.StatusCode, b)
	}
	res.Body.Close()
}

func TestSelfProfile(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	hash, err := HashPassword("secret12")
	if err != nil {
		t.Fatal(err)
	}
	admin, err := db.CreateUser(d, "admin", hash, "admin", "")
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

	badOld, _ := json.Marshal(map[string]string{"username": "owner", "old_password": "wrong"})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/me", bytes.NewReader(badOld))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatalf("wrong password %d", res.StatusCode)
	}
	res.Body.Close()

	block, _ := json.Marshal(map[string]string{"username": "root"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(admin.ID, 10), bytes.NewReader(block))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		t.Fatalf("admin rename via users %d", res.StatusCode)
	}
	res.Body.Close()

	okBody, _ := json.Marshal(map[string]string{"username": "owner", "old_password": "secret12"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/me", bytes.NewReader(okBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		User struct {
			Username string `json:"username"`
			Role     string `json:"role"`
		} `json:"user"`
	}
	decodeRes(t, res, &me)
	if me.User.Username != "owner" || me.User.Role != "admin" {
		t.Fatalf("rename %+v", me.User)
	}

	oldLogin, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err = http.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(oldLogin))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		t.Fatalf("old username still works %d", res.StatusCode)
	}
	res.Body.Close()

	pwBody, _ := json.Marshal(map[string]string{"old": "secret12", "new": "secret99"})
	res, err = c.Post(ts.URL+"/api/password", "application/json", bytes.NewReader(pwBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	create, _ := json.Marshal(map[string]string{"username": "bob", "password": "bobpass"})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(create))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	clash, _ := json.Marshal(map[string]string{"username": "bob", "old_password": "secret99"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/me", bytes.NewReader(clash))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 409 {
		t.Fatalf("username clash %d", res.StatusCode)
	}
	res.Body.Close()

	jar2, _ := cookiejar.New(nil)
	c2 := &http.Client{Jar: jar2}
	login2, _ := json.Marshal(map[string]string{"username": "bob", "password": "bobpass"})
	res, err = c2.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login2))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	userBody, _ := json.Marshal(map[string]string{"username": "bobby", "old_password": "bobpass", "new_password": "bobpass2"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/me", bytes.NewReader(userBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c2.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &me)
	if me.User.Username != "bobby" {
		t.Fatalf("user rename %+v", me.User)
	}

	jar3, _ := cookiejar.New(nil)
	c3 := &http.Client{Jar: jar3}
	login3, _ := json.Marshal(map[string]string{"username": "bobby", "password": "bobpass2"})
	res, err = c3.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login3))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
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

func TestServerCFDomains(t *testing.T) {
	cfMux := http.NewServeMux()
	cfMux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"errors":  []map[string]any{{"code": 9103, "message": "auth"}},
			})
			return
		}
		ok := func(result any) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":     true,
				"result":      result,
				"result_info": map[string]int{"page": 1, "per_page": 50, "total_pages": 1, "count": 1, "total_count": 1},
			})
		}
		switch {
		case r.URL.Path == "/zones":
			ok([]map[string]string{{"id": "z1", "name": "example.com", "status": "active"}})
		case r.URL.Path == "/zones/z1/dns_records" && r.URL.Query().Get("type") == "A":
			ok([]map[string]any{
				{"id": "r1", "type": "A", "name": "hk.example.com", "content": "31.40.214.186", "proxied": false},
				{"id": "r2", "type": "A", "name": "*.example.com", "content": "1.1.1.1", "proxied": true},
			})
		case strings.HasPrefix(r.URL.Path, "/zones/") && strings.HasSuffix(r.URL.Path, "/dns_records"):
			ok([]any{})
		default:
			http.Error(w, r.Method+" "+r.URL.Path, 404)
		}
	})
	cfTS := httptest.NewServer(cfMux)
	defer cfTS.Close()

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
	srv.CFAPI = cfTS.URL
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

	body, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "31.40.214.186"})
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
	if created.Server.ID == 0 {
		t.Fatal("server")
	}
	if _, err := d.Exec(`UPDATE servers SET connect_ip=? WHERE id=?`, "31.40.214.186", created.Server.ID); err != nil {
		t.Fatal(err)
	}

	res, err = c.Get(ts.URL + "/api/servers/" + strconv.FormatInt(created.Server.ID, 10) + "/cf-domains")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 400 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("no token %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	put, _ := json.Marshal(map[string]string{"cf_api_token": "secret-token"})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/settings", bytes.NewReader(put))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/servers/" + strconv.FormatInt(created.Server.ID, 10) + "/cf-domains")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		ServerIP string           `json:"server_ip"`
		Domains  []certs.HostName `json:"domains"`
	}
	decodeRes(t, res, &listed)
	if listed.ServerIP != "31.40.214.186" {
		t.Fatalf("server_ip %q", listed.ServerIP)
	}
	by := map[string]certs.HostName{}
	for _, h := range listed.Domains {
		by[h.Name] = h
	}
	if _, ok := by["*.example.com"]; ok {
		t.Fatal("wildcard leaked")
	}
	hk, ok := by["hk.example.com"]
	if !ok || !hk.Matched {
		t.Fatalf("hk %+v domains=%+v", hk, listed.Domains)
	}
	if _, ok := by["example.com"]; !ok {
		t.Fatalf("apex missing %+v", listed.Domains)
	}

	res, err = c.Get(ts.URL + "/api/cf-domains")
	if err != nil {
		t.Fatal(err)
	}
	var all struct {
		Domains []certs.HostName `json:"domains"`
	}
	decodeRes(t, res, &all)
	if len(all.Domains) == 0 {
		t.Fatal("cf-domains empty")
	}
}
