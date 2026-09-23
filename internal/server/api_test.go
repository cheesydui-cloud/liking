package server

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"liking/internal/certs"
	"liking/internal/db"
	"liking/internal/totp"
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

	res, err = c.Get(ts.URL + "/api/sub/" + createdUser.User.SubToken + "/clash")
	if err != nil {
		t.Fatal(err)
	}
	clashBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(clashBody), "RULE-SET,youtube,油管视频") || !strings.Contains(string(clashBody), "MATCH,回落") {
		t.Fatalf("clash sub %d %s", res.StatusCode, clashBody)
	}

	res, err = c.Get(ts.URL + "/api/sub/" + createdUser.User.SubToken + "/singbox")
	if err != nil {
		t.Fatal(err)
	}
	sbBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 200 || !strings.Contains(string(sbBody), `"final": "回落"`) || !strings.Contains(string(sbBody), "youtube") {
		t.Fatalf("singbox sub %d %s", res.StatusCode, sbBody)
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
	var share map[string]any
	decodeRes(t, res, &share)
	if share["username"] != "admin" || share["profile"] != "vless-reality-vision" {
		t.Fatalf("share meta %+v", share)
	}
	if _, ok := share["sub_token"]; ok {
		t.Fatalf("sub_token leaked %+v", share)
	}
	uri, _ := share["uri"].(string)
	if !strings.HasPrefix(uri, "vless://") || !strings.Contains(uri, "10.0.0.1:8443") {
		t.Fatalf("share uri %s", uri)
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
	share = map[string]any{}
	decodeRes(t, res, &share)
	if share["username"] != "admin" {
		t.Fatalf("share stays current user %+v", share)
	}
	if _, ok := share["sub_token"]; ok {
		t.Fatalf("sub_token leaked %+v", share)
	}
}

func TestAdminSubscriptionAllNodes(t *testing.T) {
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
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		User struct {
			Role     string `json:"role"`
			SubToken string `json:"sub_token"`
		} `json:"user"`
		Sub map[string]string `json:"sub"`
	}
	decodeRes(t, res, &me)
	if me.User.Role != "admin" || me.User.SubToken == "" || me.Sub["auto"] == "" || me.Sub["uri"] == "" {
		t.Fatalf("admin me %+v", me)
	}

	res, err = c.Get(ts.URL + "/api/me/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var nodes struct {
		Nodes []struct {
			Name string `json:"name"`
			Port int    `json:"port"`
			URI  string `json:"uri"`
		} `json:"nodes"`
	}
	decodeRes(t, res, &nodes)
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].Port != 8443 {
		t.Fatalf("admin nodes %+v", nodes.Nodes)
	}
	if !strings.HasPrefix(nodes.Nodes[0].URI, "vless://") || !strings.Contains(nodes.Nodes[0].URI, "10.0.0.1:8443") {
		t.Fatalf("admin node uri %s", nodes.Nodes[0].URI)
	}

	res, err = c.Get(ts.URL + "/api/sub/" + me.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("admin sub %d %s", res.StatusCode, b)
	}
	adminBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	adminDecoded, err := base64.StdEncoding.DecodeString(string(adminBody))
	if err != nil {
		t.Fatalf("admin sub b64 %v %s", err, adminBody)
	}
	adminURI := strings.TrimSpace(string(adminDecoded))
	if !strings.HasPrefix(adminURI, "vless://") || !strings.Contains(adminURI, "10.0.0.1:8443") {
		t.Fatalf("admin uri %s", adminURI)
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
			SubToken string `json:"sub_token"`
		} `json:"user"`
	}
	decodeRes(t, res, &createdUser)

	res, err = c.Get(ts.URL + "/api/sub/" + createdUser.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	aliceBody, _ := io.ReadAll(res.Body)
	res.Body.Close()
	aliceDecoded, err := base64.StdEncoding.DecodeString(string(aliceBody))
	if err != nil {
		t.Fatalf("alice sub b64 %v %s", err, aliceBody)
	}
	aliceURI := strings.TrimSpace(string(aliceDecoded))
	if vlessUUID(adminURI) == "" || vlessUUID(adminURI) == vlessUUID(aliceURI) {
		t.Fatalf("admin and alice share uuid admin=%s alice=%s", adminURI, aliceURI)
	}
}

func TestMeNodesTrafficAndChain(t *testing.T) {
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
		"name":      "direct-1",
		"profile":   "vless-reality-vision",
		"port":      8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var direct struct {
		Inbound struct {
			ID int64 `json:"id"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &direct)

	chainBody, _ := json.Marshal(map[string]any{
		"server_id":       created.Server.ID,
		"name":            "relay-1",
		"profile":         "vless-reality-vision",
		"port":            8444,
		"line_kind":       "chain",
		"exit_inbound_id": direct.Inbound.ID,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(chainBody))
	if err != nil {
		t.Fatal(err)
	}
	var chain struct {
		Inbound struct {
			ID int64 `json:"id"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &chain)

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
	var createdUser struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	decodeRes(t, res, &createdUser)

	admin, err := db.GetUserByName(d, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AddDailyTraffic(d, "2026-09-13", admin.ID, direct.Inbound.ID, 100, 200); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDailyTraffic(d, "2026-09-13", createdUser.User.ID, direct.Inbound.ID, 10, 20); err != nil {
		t.Fatal(err)
	}
	if err := db.AddDailyTraffic(d, "2026-09-13", createdUser.User.ID, chain.Inbound.ID, 5, 7); err != nil {
		t.Fatal(err)
	}

	type meNode struct {
		Name     string `json:"name"`
		LineKind string `json:"line_kind"`
		UsedUp   int64  `json:"used_up"`
		UsedDown int64  `json:"used_down"`
	}
	findNode := func(list []meNode, name string) meNode {
		t.Helper()
		for _, n := range list {
			if n.Name == name {
				return n
			}
		}
		t.Fatalf("missing node %s in %+v", name, list)
		return meNode{}
	}

	res, err = c.Get(ts.URL + "/api/me/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var adminNodes struct {
		Nodes []meNode `json:"nodes"`
	}
	decodeRes(t, res, &adminNodes)
	dNode := findNode(adminNodes.Nodes, "direct-1")
	if dNode.LineKind != "direct" || dNode.UsedUp != 100 || dNode.UsedDown != 200 {
		t.Fatalf("admin direct %+v", dNode)
	}
	rNode := findNode(adminNodes.Nodes, "relay-1")
	if rNode.LineKind != "chain" || rNode.UsedUp != 0 || rNode.UsedDown != 0 {
		t.Fatalf("admin relay %+v", rNode)
	}

	aliceJar, _ := cookiejar.New(nil)
	aliceC := &http.Client{Jar: aliceJar}
	aliceLogin, _ := json.Marshal(map[string]string{"username": "alice", "password": "alice12"})
	res, err = aliceC.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(aliceLogin))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = aliceC.Get(ts.URL + "/api/me/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var aliceNodes struct {
		Nodes []meNode `json:"nodes"`
	}
	decodeRes(t, res, &aliceNodes)
	dNode = findNode(aliceNodes.Nodes, "direct-1")
	if dNode.LineKind != "direct" || dNode.UsedUp != 10 || dNode.UsedDown != 20 {
		t.Fatalf("alice direct %+v", dNode)
	}
	rNode = findNode(aliceNodes.Nodes, "relay-1")
	if rNode.LineKind != "chain" || rNode.UsedUp != 5 || rNode.UsedDown != 7 {
		t.Fatalf("alice relay %+v", rNode)
	}
}

func vlessUUID(uri string) string {
	rest := strings.TrimPrefix(uri, "vless://")
	at := strings.Index(rest, "@")
	if at <= 0 {
		return ""
	}
	return rest[:at]
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
	res, err = c.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Users []struct {
			ID       int64  `json:"id"`
			Password string `json:"password"`
			Role     string `json:"role"`
		} `json:"users"`
	}
	decodeRes(t, res, &listed)
	sawBob := false
	for _, u := range listed.Users {
		if u.Role == "admin" && u.Password != "" {
			t.Fatal("admin password in list")
		}
		if u.ID == created.User.ID {
			sawBob = true
			if u.Password != "" {
				t.Fatalf("list still has password %q", u.Password)
			}
		}
	}
	if !sawBob {
		t.Fatal("bob not listed")
	}
	res, err = c.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	var adminMe struct {
		User map[string]any `json:"user"`
	}
	decodeRes(t, res, &adminMe)
	if pw, ok := adminMe.User["password"]; ok && pw != nil && pw != "" {
		t.Fatalf("/me leaked password %#v", pw)
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
		"speed_limit":   50,
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
			Password     string `json:"password"`
			TrafficLimit *int64 `json:"traffic_limit"`
			UsedUp       int64  `json:"used_up"`
			UsedDown     int64  `json:"used_down"`
			PackageID    *int64 `json:"package_id"`
			SpeedLimit   int64  `json:"speed_limit"`
			NetUpBps     int64  `json:"net_up_bps"`
			NetDownBps   int64  `json:"net_down_bps"`
		} `json:"user"`
	}
	decodeRes(t, res, &edited)
	if edited.User.Username != "robert" || edited.User.Remark != "vip" || edited.User.TrafficLimit == nil || *edited.User.TrafficLimit != 5*1024*1024*1024 || edited.User.SpeedLimit != 50 {
		t.Fatalf("edit %+v", edited.User)
	}
	if edited.User.Password != "" {
		t.Fatalf("edit still returned stored password %q", edited.User.Password)
	}
	if edited.User.UsedUp != 111 || edited.User.UsedDown != 222 {
		t.Fatalf("same package wiped traffic %+v", edited.User)
	}
	badSpeed, _ := json.Marshal(map[string]any{"speed_limit": 10000*125 + 1})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10), bytes.NewReader(badSpeed))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == 200 {
		t.Fatal("speed_limit above 10000 Mbps should fail")
	}
	io.ReadAll(res.Body)
	res.Body.Close()

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
			TrafficCap int64  `json:"traffic_cap"`
			Password   string `json:"password"`
		} `json:"user"`
		Sub map[string]string `json:"sub"`
	}
	decodeRes(t, res, &me)
	if me.User.TrafficCap != 20*1024*1024*1024 || me.Sub["auto"] == "" || me.Sub["clash"] == "" {
		t.Fatalf("me %+v", me)
	}
	if me.User.Password != "" {
		t.Fatalf("/me user leaked password %q", me.User.Password)
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

func TestServerDisableIPv6(t *testing.T) {
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

	body, _ := json.Marshal(map[string]any{"name": "n1", "public_host": "10.0.0.1", "disable_ipv6": true})
	res, err = c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Server struct {
			ID          int64 `json:"id"`
			DisableIPv6 bool  `json:"disable_ipv6"`
		} `json:"server"`
	}
	decodeRes(t, res, &created)
	if created.Server.ID == 0 || !created.Server.DisableIPv6 {
		t.Fatalf("%+v", created.Server)
	}

	upd, _ := json.Marshal(map[string]any{"disable_ipv6": false})
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
			DisableIPv6 bool `json:"disable_ipv6"`
		} `json:"server"`
	}
	decodeRes(t, res, &updated)
	if updated.Server.DisableIPv6 {
		t.Fatalf("still disabled %+v", updated.Server)
	}

	upd, _ = json.Marshal(map[string]any{"disable_ipv6": true})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/servers/"+strconv.FormatInt(created.Server.ID, 10), bytes.NewReader(upd))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &updated)
	if !updated.Server.DisableIPv6 {
		t.Fatal("not disabled")
	}
}

func TestServerPortRange(t *testing.T) {
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

	body, _ := json.Marshal(map[string]any{"name": "n1", "public_host": "10.0.0.1", "port_min": 40000, "port_max": 40010})
	res, err = c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Server struct {
			ID      int64 `json:"id"`
			PortMin int   `json:"port_min"`
			PortMax int   `json:"port_max"`
		} `json:"server"`
	}
	decodeRes(t, res, &created)
	if created.Server.PortMin != 40000 || created.Server.PortMax != 40010 {
		t.Fatalf("create %+v", created.Server)
	}

	bad, _ := json.Marshal(map[string]any{"port_min": 50000, "port_max": 40000})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/servers/"+strconv.FormatInt(created.Server.ID, 10), bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == 200 {
		t.Fatal("min>max should fail")
	}
	io.ReadAll(res.Body)
	res.Body.Close()

	inBody, _ := json.Marshal(map[string]any{
		"server_id": created.Server.ID,
		"name":      "auto",
		"profile":   "vless-reality-vision",
		"port":      0,
	})
	seen := map[int]struct{}{}
	for i := 0; i < 5; i++ {
		res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
		if err != nil {
			t.Fatal(err)
		}
		var createdIn struct {
			Inbound struct {
				Port int `json:"port"`
			} `json:"inbound"`
		}
		decodeRes(t, res, &createdIn)
		p := createdIn.Inbound.Port
		if p < 40000 || p > 40010 {
			t.Fatalf("port %d outside range", p)
		}
		if _, ok := seen[p]; ok {
			t.Fatalf("duplicate port %d", p)
		}
		seen[p] = struct{}{}
	}

	upd, _ := json.Marshal(map[string]any{"port_min": 20000, "port_max": 20000})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/servers/"+strconv.FormatInt(created.Server.ID, 10), bytes.NewReader(upd))
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
			PortMin         int   `json:"port_min"`
			PortMax         int   `json:"port_max"`
			ExpiresAt       int64 `json:"expires_at"`
			TrafficResetDay int   `json:"traffic_reset_day"`
		} `json:"server"`
	}
	decodeRes(t, res, &updated)
	if updated.Server.PortMin != 20000 || updated.Server.PortMax != 20000 {
		t.Fatalf("update %+v", updated.Server)
	}

	meta, _ := json.Marshal(map[string]any{"expires_at": int64(1800000000), "traffic_reset_day": 15})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/servers/"+strconv.FormatInt(created.Server.ID, 10), bytes.NewReader(meta))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &updated)
	if updated.Server.ExpiresAt != 1800000000 || updated.Server.TrafficResetDay != 15 {
		t.Fatalf("expiry %+v", updated.Server)
	}

	badDay, _ := json.Marshal(map[string]any{"traffic_reset_day": 32})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/servers/"+strconv.FormatInt(created.Server.ID, 10), bytes.NewReader(badDay))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == 200 {
		t.Fatal("reset day 32 should fail")
	}
	io.ReadAll(res.Body)
	res.Body.Close()
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
	if st["timezone"] != "Asia/Shanghai" {
		t.Fatalf("timezone %+v", st)
	}
	if st["sub_rule_preset"] != "balanced" {
		t.Fatalf("sub rules %+v", st["sub_rule_preset"])
	}
	denyCat, _ := st["site_deny_catalog"].([]any)
	if len(denyCat) < 8 {
		t.Fatalf("site deny catalog %+v", st["site_deny_catalog"])
	}
	catNames := map[string]bool{}
	for _, raw := range denyCat {
		m, _ := raw.(map[string]any)
		name, _ := m["name"].(string)
		catNames[name] = true
	}
	if !catNames["speedtest"] || !catNames["iplookup"] || !catNames["openai"] {
		t.Fatalf("catalog names %+v", catNames)
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
	if st["sub_rule_preset"] != "balanced" {
		t.Fatalf("rules wiped %+v", st["sub_rule_preset"])
	}

	rulePut, _ := json.Marshal(map[string]any{
		"sub_rule_preset":     "custom",
		"sub_rule_categories": []string{"ads", "private", "bogus"},
	})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/settings", bytes.NewReader(rulePut))
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
	if st["sub_rule_preset"] != "custom" {
		t.Fatalf("custom preset %+v", st["sub_rule_preset"])
	}
	gotCats, _ := st["sub_rule_categories"].([]any)
	if len(gotCats) != 2 || gotCats[0] != "ads" || gotCats[1] != "private" {
		t.Fatalf("custom cats %+v", gotCats)
	}
	cat0, _ := st["sub_rule_catalog"].([]any)
	if len(cat0) < 20 {
		t.Fatalf("catalog %+v", len(cat0))
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
	if res.StatusCode != 400 {
		t.Fatalf("user rename %d", res.StatusCode)
	}
	res.Body.Close()

	pwOnly, _ := json.Marshal(map[string]string{"old_password": "bobpass", "new_password": "bobpass2"})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/me", bytes.NewReader(pwOnly))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c2.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &me)
	if me.User.Username != "bob" {
		t.Fatalf("user password change renamed %+v", me.User)
	}

	jar3, _ := cookiejar.New(nil)
	c3 := &http.Client{Jar: jar3}
	login3, _ := json.Marshal(map[string]string{"username": "bob", "password": "bobpass2"})
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
	if r.Header().Get("X-Frame-Options") != "DENY" {
		t.Fatalf("frame %q", r.Header().Get("X-Frame-Options"))
	}
	if r.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("nosniff %q", r.Header().Get("X-Content-Type-Options"))
	}
	csp := r.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "default-src 'self'") {
		t.Fatalf("csp %q", csp)
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

func TestBackupRestore(t *testing.T) {
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
	if err := db.SetSetting(d, "cf_api_token", "tok-live"); err != nil {
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

	body, _ := json.Marshal(map[string]string{"name": "hk", "public_host": "hk.example.com"})
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

	res, err = c.Get(ts.URL + "/api/backup/summary")
	if err != nil {
		t.Fatal(err)
	}
	var live db.BackupSummary
	decodeRes(t, res, &live)
	if live.Users < 1 || live.Servers != 1 || !live.HasCFToken {
		t.Fatalf("live %+v", live)
	}

	res, err = c.Get(ts.URL + "/api/backup")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode == 200 {
		t.Fatalf("GET backup still works %d", res.StatusCode)
	}

	bakBody, _ := json.Marshal(map[string]string{"password": "secret12", "encrypt_password": "backup-pw"})
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/api/backup", bytes.NewReader(bakBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 || len(raw) < 32 || !bytes.HasPrefix(raw, []byte("LKB1")) {
		t.Fatalf("download %d %d", res.StatusCode, len(raw))
	}

	create, _ := json.Marshal(map[string]string{"username": "intruder", "password": "intruder"})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(create))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	postFile := func(path, password, filePassword string, payload []byte) *http.Response {
		t.Helper()
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, err := mw.CreateFormFile("file", "x.lkb1")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write(payload); err != nil {
			t.Fatal(err)
		}
		if password != "" {
			if err := mw.WriteField("password", password); err != nil {
				t.Fatal(err)
			}
		}
		if filePassword != "" {
			if err := mw.WriteField("file_password", filePassword); err != nil {
				t.Fatal(err)
			}
		}
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, ts.URL+path, &buf)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", mw.FormDataContentType())
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}

	res = postFile("/api/backup/preview", "", "backup-pw", raw)
	var prev db.BackupSummary
	decodeRes(t, res, &prev)
	if prev.Servers != 1 || prev.Users != 1 {
		t.Fatalf("preview %+v", prev)
	}

	res = postFile("/api/backup/restore", "wrong", "backup-pw", raw)
	if res.StatusCode != 403 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("bad pw %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	res = postFile("/api/backup/restore", "secret12", "backup-pw", raw)
	var out map[string]any
	decodeRes(t, res, &out)
	if out["relogin"] != true {
		t.Fatalf("restore %+v", out)
	}

	res, err = c.Get(ts.URL + "/api/servers")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 401 {
		res.Body.Close()
		t.Fatalf("session survived %d", res.StatusCode)
	}
	res.Body.Close()

	jar2, _ := cookiejar.New(nil)
	c2 := &http.Client{Jar: jar2}
	res, err = c2.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c2.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var users struct {
		Users []map[string]any `json:"users"`
	}
	decodeRes(t, res, &users)
	for _, u := range users.Users {
		if u["username"] == "intruder" {
			t.Fatal("intruder survived restore")
		}
	}
	res, err = c2.Get(ts.URL + "/api/servers")
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
}

func TestPackageInboundIDs(t *testing.T) {
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

	body, _ := json.Marshal(map[string]string{"name": "hk", "public_host": "hk.example.com"})
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

	mk := func(name string, port int) int64 {
		t.Helper()
		inBody, _ := json.Marshal(map[string]any{
			"server_id": created.Server.ID, "name": name, "profile": "ss2022", "port": port,
		})
		res, err := c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Inbound struct {
				ID int64 `json:"id"`
			} `json:"inbound"`
		}
		decodeRes(t, res, &out)
		return out.Inbound.ID
	}
	a := mk("a", 8443)
	b := mk("b", 8444)

	pkgBody, _ := json.Marshal(map[string]any{
		"name": "pick", "traffic_bytes": 0, "cycle_days": 0, "direction": "oneway",
		"inbound_ids": []int64{a},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Package struct {
			ID         int64   `json:"id"`
			CycleDays  int     `json:"cycle_days"`
			InboundIDs []int64 `json:"inbound_ids"`
			ServerIDs  []int64 `json:"server_ids"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg)
	if pkg.Package.CycleDays != 0 {
		t.Fatalf("cycle %d", pkg.Package.CycleDays)
	}
	if len(pkg.Package.InboundIDs) != 1 || pkg.Package.InboundIDs[0] != a {
		t.Fatalf("inbounds %+v want %d not %d", pkg.Package.InboundIDs, a, b)
	}
	if len(pkg.Package.ServerIDs) != 0 {
		t.Fatalf("servers %+v", pkg.Package.ServerIDs)
	}

	upd, _ := json.Marshal(map[string]any{"inbound_ids": []int64{a, b}})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/packages/"+strconv.FormatInt(pkg.Package.ID, 10), bytes.NewReader(upd))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &pkg)
	if len(pkg.Package.InboundIDs) != 2 {
		t.Fatalf("updated %+v", pkg.Package.InboundIDs)
	}
}

func TestTrafficAPI(t *testing.T) {
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
		"server_id": created.Server.ID, "name": "v1", "profile": "vless-reality-vision", "port": 8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var inb struct {
		Inbound struct {
			ID int64 `json:"id"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &inb)

	pkgBody, _ := json.Marshal(map[string]any{
		"name": "two", "traffic_bytes": 1024, "cycle_days": 0, "direction": "twoway",
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
		"username": "carol", "password": "passpass", "package_id": pkg.Package.ID,
	})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdU struct {
		User struct {
			ID int64 `json:"id"`
		} `json:"user"`
	}
	decodeRes(t, res, &createdU)
	if err := db.AddUserTraffic(d, createdU.User.ID, 10, 15); err != nil {
		t.Fatal(err)
	}
	day := db.ClockDay(d)
	if err := db.AddDailyTraffic(d, day, createdU.User.ID, inb.Inbound.ID, 40, 60); err != nil {
		t.Fatal(err)
	}

	res, err = c.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var list struct {
		Users []struct {
			Username    string `json:"username"`
			BilledBytes int64  `json:"billed_bytes"`
			Direction   string `json:"direction"`
			TrafficCap  int64  `json:"traffic_cap"`
		} `json:"users"`
	}
	decodeRes(t, res, &list)
	found := false
	for _, u := range list.Users {
		if u.Username == "carol" {
			found = true
			if u.BilledBytes != 25 || u.Direction != "twoway" || u.TrafficCap != 1024 {
				t.Fatalf("user %+v", u)
			}
		}
	}
	if !found {
		t.Fatal("carol missing")
	}

	res, err = c.Get(ts.URL + "/api/traffic?days=7")
	if err != nil {
		t.Fatal(err)
	}
	var tr struct {
		Days []struct {
			Day  string `json:"day"`
			Up   int64  `json:"up"`
			Down int64  `json:"down"`
		} `json:"days"`
		Users []struct {
			Name string `json:"name"`
			Up   int64  `json:"up"`
			Down int64  `json:"down"`
		} `json:"users"`
		Inbounds []struct {
			ID int64 `json:"id"`
			Up int64 `json:"up"`
		} `json:"inbounds"`
		Billed int64 `json:"billed_bytes"`
	}
	decodeRes(t, res, &tr)
	if len(tr.Days) != 7 {
		t.Fatalf("days %d", len(tr.Days))
	}
	last := tr.Days[len(tr.Days)-1]
	if last.Up != 40 || last.Down != 60 {
		t.Fatalf("today %+v", last)
	}
	if len(tr.Users) != 1 || tr.Users[0].Name != "carol" {
		t.Fatalf("users %+v", tr.Users)
	}
	if len(tr.Inbounds) != 1 || tr.Inbounds[0].ID != inb.Inbound.ID {
		t.Fatalf("inbounds %+v", tr.Inbounds)
	}
	if tr.Billed != 25 {
		t.Fatalf("billed %d", tr.Billed)
	}

	res, err = c.Get(ts.URL + "/api/users/" + strconv.FormatInt(createdU.User.ID, 10) + "/traffic")
	if err != nil {
		t.Fatal(err)
	}
	var ut struct {
		Days []struct {
			Up   int64 `json:"up"`
			Down int64 `json:"down"`
		} `json:"days"`
	}
	decodeRes(t, res, &ut)
	if len(ut.Days) == 0 || ut.Days[len(ut.Days)-1].Up != 40 {
		t.Fatalf("user traffic %+v", ut)
	}

	res, err = c.Get(ts.URL + "/api/dashboard")
	if err != nil {
		t.Fatal(err)
	}
	var dash struct {
		Used  int64 `json:"used_bytes"`
		Today int64 `json:"today_bytes"`
		Days  []any `json:"days"`
		Hours []any `json:"hours"`
	}
	decodeRes(t, res, &dash)
	if dash.Used != 25 || dash.Today != 100 || len(dash.Days) != 30 || len(dash.Hours) != 24 {
		t.Fatalf("dash %+v", dash)
	}

	res, err = c.Get(ts.URL + "/api/inbounds")
	if err != nil {
		t.Fatal(err)
	}
	var ins struct {
		Inbounds []struct {
			ID     int64 `json:"id"`
			UsedUp int64 `json:"used_up"`
		} `json:"inbounds"`
	}
	decodeRes(t, res, &ins)
	okIn := false
	for _, in := range ins.Inbounds {
		if in.ID == inb.Inbound.ID && in.UsedUp == 40 {
			okIn = true
		}
	}
	if !okIn {
		t.Fatalf("inbound traffic %+v", ins.Inbounds)
	}
}

func TestTOTPLoginAndAdminCIDR(t *testing.T) {
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
	secret, err := totp.RandomSecret()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserTOTP(d, admin.ID, secret, true); err != nil {
		t.Fatal(err)
	}
	srv, err := New(d)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()
	c := &http.Client{}

	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d %s", res.StatusCode, body)
	}
	var fail map[string]any
	if err := json.Unmarshal(body, &fail); err != nil {
		t.Fatal(err)
	}
	if fail["need_totp"] != true {
		t.Fatalf("need_totp %+v", fail)
	}

	code := totp.MustCode(secret, time.Now())
	okLogin, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12", "totp": code})
	res, err = c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(okLogin))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	if err := db.SetSetting(d, "admin_cidrs", "10.0.0.0/8"); err != nil {
		t.Fatal(err)
	}
	res, err = c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(okLogin))
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("cidr want 403 got %d %s", res.StatusCode, b)
	}

	if err := db.SetSetting(d, "admin_cidrs", "127.0.0.1"); err != nil {
		t.Fatal(err)
	}
	jar, _ := cookiejar.New(nil)
	c2 := &http.Client{Jar: jar}
	res, err = c2.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(okLogin))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c2.Get(ts.URL + "/api/settings")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
}

func sessionCookieMaxAge(res *http.Response) int {
	for _, c := range res.Cookies() {
		if c.Name == sessionCookie {
			return c.MaxAge
		}
	}
	return -2
}

func TestLoginRememberCookie(t *testing.T) {
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
	c := &http.Client{}

	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	wantShort := int(sessionTTL.Seconds())
	if got := sessionCookieMaxAge(res); got != wantShort {
		t.Fatalf("default max-age %d want %d", got, wantShort)
	}
	decodeRes(t, res, nil)

	remember, _ := json.Marshal(map[string]any{"username": "admin", "password": "secret12", "remember": true})
	res, err = c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(remember))
	if err != nil {
		t.Fatal(err)
	}
	wantLong := int(sessionRememberTTL.Seconds())
	if got := sessionCookieMaxAge(res); got != wantLong {
		t.Fatalf("remember max-age %d want %d", got, wantLong)
	}
	tok := ""
	for _, ck := range res.Cookies() {
		if ck.Name == sessionCookie {
			tok = ck.Value
		}
	}
	decodeRes(t, res, nil)
	if tok == "" {
		t.Fatal("remember cookie")
	}
	u, err := db.GetSessionUser(d, tok)
	if err != nil || u == nil || u.Username != "admin" {
		t.Fatalf("session user %v %v", u, err)
	}

	off, _ := json.Marshal(map[string]any{"username": "admin", "password": "secret12", "remember": false})
	res, err = c.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(off))
	if err != nil {
		t.Fatal(err)
	}
	if got := sessionCookieMaxAge(res); got != wantShort {
		t.Fatalf("remember=false max-age %d want %d", got, wantShort)
	}
	decodeRes(t, res, nil)
}

func TestEncryptedBackupAndSubUserinfo(t *testing.T) {
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

	put, _ := json.Marshal(map[string]any{"announce": "维护", "backup_hour": "4", "backup_keep": 3})
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

	encBody, _ := json.Marshal(map[string]string{"password": "secret12", "encrypt_password": "backup-pw"})
	req, err = http.NewRequest(http.MethodPost, ts.URL+"/api/backup", bytes.NewReader(encBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("backup %d %s", res.StatusCode, raw)
	}
	if !bytes.HasPrefix(raw, []byte("LKB1")) {
		t.Fatalf("not encrypted magic %q", raw[:min(8, len(raw))])
	}

	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	fw, err := w.CreateFormFile("file", "liking-backup.lkb1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(raw); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	res, err = c.Post(ts.URL+"/api/backup/preview", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != 400 {
		t.Fatalf("preview without pw %d %s", res.StatusCode, b)
	}

	buf.Reset()
	w = multipart.NewWriter(&buf)
	fw, err = w.CreateFormFile("file", "liking-backup.lkb1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("file_password", "backup-pw"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	res, err = c.Post(ts.URL+"/api/backup/preview", w.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	var sum db.BackupSummary
	decodeRes(t, res, &sum)
	if sum.Users < 1 {
		t.Fatalf("preview %+v", sum)
	}

	srvBody, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "10.0.0.1"})
	res, err = c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(srvBody))
	if err != nil {
		t.Fatal(err)
	}
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)
	pkgBody, _ := json.Marshal(map[string]any{
		"name": "p1", "traffic_bytes": 0, "cycle_days": 0, "direction": "oneway",
		"server_ids": []int64{createdSrv.Server.ID},
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
	uBody, _ := json.Marshal(map[string]any{"username": "bob", "password": "passpass", "package_id": pkg.Package.ID})
	res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		User struct {
			SubToken string `json:"sub_token"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if created.User.SubToken == "" {
		t.Fatal("missing sub token")
	}
	res, err = http.Get(ts.URL + "/api/sub/" + created.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	info := res.Header.Get("Subscription-Userinfo")
	interval := res.Header.Get("Profile-Update-Interval")
	res.Body.Close()
	if interval != "24" {
		t.Fatalf("interval %q", interval)
	}
	if !strings.Contains(info, "upload=0") || !strings.Contains(info, "download=") {
		t.Fatalf("userinfo %q", info)
	}
}

func TestUserSubRulesInheritAndOverride(t *testing.T) {
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
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)
	inBody, _ := json.Marshal(map[string]any{
		"server_id": createdSrv.Server.ID, "name": "vless-1", "profile": "vless-reality-vision", "port": 8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	pkgBody, _ := json.Marshal(map[string]any{
		"name": "std", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway",
		"server_ids": []int64{createdSrv.Server.ID},
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
	var created struct {
		User struct {
			ID                int64    `json:"id"`
			SubToken          string   `json:"sub_token"`
			SubRulePreset     string   `json:"sub_rule_preset"`
			SubRuleCategories []string `json:"sub_rule_categories"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if created.User.SubRulePreset != "" {
		t.Fatalf("new user should inherit %+v", created.User)
	}

	putJSON := func(path string, payload any) *http.Response {
		t.Helper()
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPut, ts.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	userPath := "/api/users/" + strconv.FormatInt(created.User.ID, 10)
	getSub := func(fmtName string) string {
		t.Helper()
		res, err := http.Get(ts.URL + "/api/sub/" + created.User.SubToken + "/" + fmtName)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != 200 {
			t.Fatalf("%s sub %d %s", fmtName, res.StatusCode, b)
		}
		return string(b)
	}

	clash := getSub("clash")
	if !strings.Contains(clash, "RULE-SET,youtube,油管视频") || strings.Contains(clash, "广告拦截") {
		t.Fatalf("default inherit clash %s", clash)
	}
	sb := getSub("singbox")
	if !strings.Contains(sb, "youtube") || strings.Contains(sb, "category-ads-all") {
		t.Fatalf("default inherit singbox %s", sb)
	}

	res = putJSON(userPath, map[string]any{
		"sub_rule_preset":     "custom",
		"sub_rule_categories": []string{"ads", "private", "bogus"},
	})
	var edited struct {
		User struct {
			SubRulePreset     string   `json:"sub_rule_preset"`
			SubRuleCategories []string `json:"sub_rule_categories"`
			Username          string   `json:"username"`
		} `json:"user"`
	}
	decodeRes(t, res, &edited)
	if edited.User.SubRulePreset != "custom" || len(edited.User.SubRuleCategories) != 2 || edited.User.SubRuleCategories[0] != "ads" || edited.User.SubRuleCategories[1] != "private" {
		t.Fatalf("custom save %+v", edited.User)
	}

	clash = getSub("clash")
	if !strings.Contains(clash, "广告拦截") || strings.Contains(clash, "RULE-SET,youtube,油管视频") {
		t.Fatalf("user custom clash %s", clash)
	}
	sb = getSub("singbox")
	if !strings.Contains(sb, "category-ads-all") || strings.Contains(sb, "youtube") {
		t.Fatalf("user custom singbox %s", sb)
	}

	res = putJSON(userPath, map[string]any{"enabled": true})
	decodeRes(t, res, &edited)
	if edited.User.SubRulePreset != "custom" || edited.User.Username != "alice" {
		t.Fatalf("partial wiped rules %+v", edited.User)
	}

	res = putJSON(userPath, map[string]any{"sub_rule_preset": "nope"})
	if res.StatusCode == 200 {
		t.Fatal("invalid preset should fail")
	}
	io.ReadAll(res.Body)
	res.Body.Close()

	res = putJSON(userPath, map[string]any{"sub_rule_preset": "inherit", "sub_rule_categories": []string{"ads"}})
	decodeRes(t, res, &edited)
	if edited.User.SubRulePreset != "" || len(edited.User.SubRuleCategories) != 0 {
		t.Fatalf("inherit should clear %+v", edited.User)
	}
	clash = getSub("clash")
	if !strings.Contains(clash, "RULE-SET,youtube,油管视频") {
		t.Fatalf("back to global %s", clash)
	}

	res = putJSON("/api/settings", map[string]any{"sub_rule_preset": "minimal"})
	decodeRes(t, res, nil)
	clash = getSub("clash")
	if strings.Contains(clash, "RULE-SET,youtube,油管视频") || !strings.Contains(clash, "国内服务") {
		t.Fatalf("global minimal inherit %s", clash)
	}

	res = putJSON(userPath, map[string]any{"sub_rule_preset": "balanced"})
	decodeRes(t, res, &edited)
	if edited.User.SubRulePreset != "balanced" {
		t.Fatalf("user balanced %+v", edited.User)
	}
	clash = getSub("clash")
	if !strings.Contains(clash, "RULE-SET,youtube,油管视频") {
		t.Fatalf("user balanced vs global minimal %s", clash)
	}

	uri, err := http.Get(ts.URL + "/api/sub/" + created.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	uriBody, _ := io.ReadAll(uri.Body)
	uri.Body.Close()
	if uri.StatusCode != 200 {
		t.Fatalf("uri %d %s", uri.StatusCode, uriBody)
	}
	decoded, err := base64.StdEncoding.DecodeString(string(uriBody))
	if err != nil {
		t.Fatalf("uri b64 %v", err)
	}
	if !strings.Contains(string(decoded), "vless://") {
		t.Fatalf("uri still needs nodes %s", decoded)
	}
}

func TestUserSiteDeny(t *testing.T) {
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
	var createdSrv struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, res, &createdSrv)
	inBody, _ := json.Marshal(map[string]any{
		"server_id": createdSrv.Server.ID, "name": "vless-1", "profile": "vless-reality-vision", "port": 8443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	pkgBody, _ := json.Marshal(map[string]any{
		"name": "std", "traffic_bytes": 0, "cycle_days": 30, "direction": "oneway",
		"server_ids": []int64{createdSrv.Server.ID},
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
	var created struct {
		User struct {
			ID                 int64    `json:"id"`
			SubToken           string   `json:"sub_token"`
			SiteDenyCategories []string `json:"site_deny_categories"`
			SiteDenyDomains    []string `json:"site_deny_domains"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if len(created.User.SiteDenyCategories) != 0 || len(created.User.SiteDenyDomains) != 0 {
		t.Fatalf("new user deny %+v", created.User)
	}

	putJSON := func(path string, payload any) *http.Response {
		t.Helper()
		raw, _ := json.Marshal(payload)
		req, err := http.NewRequest(http.MethodPut, ts.URL+path, bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		res, err := c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	userPath := "/api/users/" + strconv.FormatInt(created.User.ID, 10)
	var edited struct {
		User struct {
			Username           string   `json:"username"`
			SiteDenyCategories []string `json:"site_deny_categories"`
			SiteDenyDomains    []string `json:"site_deny_domains"`
			SiteFilterMode     string   `json:"site_filter_mode"`
		} `json:"user"`
	}

	res = putJSON(userPath, map[string]any{
		"site_deny_categories": []string{"google", "nope"},
		"site_deny_domains":    []string{"https://WWW.Instagram.com/a", "10.0.0.0/8"},
	})
	decodeRes(t, res, &edited)
	if len(edited.User.SiteDenyCategories) != 1 || edited.User.SiteDenyCategories[0] != "google" {
		t.Fatalf("cats %+v", edited.User.SiteDenyCategories)
	}
	if len(edited.User.SiteDenyDomains) != 2 || edited.User.SiteDenyDomains[0] != "www.instagram.com" || edited.User.SiteDenyDomains[1] != "10.0.0.0/8" {
		t.Fatalf("domains %+v", edited.User.SiteDenyDomains)
	}
	if edited.User.SiteFilterMode != "deny" {
		t.Fatalf("compat mode %q", edited.User.SiteFilterMode)
	}

	res = putJSON(userPath, map[string]any{"enabled": true})
	decodeRes(t, res, &edited)
	if edited.User.Username != "alice" || edited.User.SiteDenyCategories[0] != "google" || len(edited.User.SiteDenyDomains) != 2 || edited.User.SiteFilterMode != "deny" {
		t.Fatalf("partial wiped deny %+v", edited.User)
	}

	res = putJSON(userPath, map[string]any{"site_deny_categories": []string{"youtube", "tiktok"}})
	decodeRes(t, res, &edited)
	if len(edited.User.SiteDenyCategories) != 2 {
		t.Fatalf("cats only %+v", edited.User.SiteDenyCategories)
	}
	gotCats := map[string]bool{}
	for _, n := range edited.User.SiteDenyCategories {
		gotCats[n] = true
	}
	if !gotCats["youtube"] || !gotCats["tiktok"] {
		t.Fatalf("cats only %+v", edited.User.SiteDenyCategories)
	}
	if len(edited.User.SiteDenyDomains) != 2 {
		t.Fatalf("domains should keep %+v", edited.User.SiteDenyDomains)
	}

	tooMany := make([]string, 51)
	for i := range tooMany {
		tooMany[i] = "n" + strconv.Itoa(i) + ".example.com"
	}
	res = putJSON(userPath, map[string]any{"site_deny_domains": tooMany})
	if res.StatusCode == 200 {
		t.Fatal("51 domains should fail")
	}
	io.ReadAll(res.Body)
	res.Body.Close()

	res = putJSON(userPath, map[string]any{"site_deny_categories": []string{}, "site_deny_domains": []string{}})
	decodeRes(t, res, &edited)
	if len(edited.User.SiteDenyCategories) != 0 || len(edited.User.SiteDenyDomains) != 0 || edited.User.SiteFilterMode != "" {
		t.Fatalf("clear %+v", edited.User)
	}

	res = putJSON(userPath, map[string]any{"site_filter_mode": "allow"})
	if res.StatusCode != 400 {
		t.Fatalf("empty allow %d", res.StatusCode)
	}
	io.ReadAll(res.Body)
	res.Body.Close()

	res = putJSON(userPath, map[string]any{"site_filter_mode": "nope", "site_deny_categories": []string{"tiktok"}})
	if res.StatusCode != 400 {
		t.Fatalf("bad mode %d", res.StatusCode)
	}
	io.ReadAll(res.Body)
	res.Body.Close()

	res = putJSON(userPath, map[string]any{
		"site_filter_mode":     "allow",
		"site_deny_categories": []string{"speedtest", "iplookup", "nope"},
	})
	decodeRes(t, res, &edited)
	if edited.User.SiteFilterMode != "allow" {
		t.Fatalf("allow mode %q", edited.User.SiteFilterMode)
	}
	if len(edited.User.SiteDenyCategories) != 2 || edited.User.SiteDenyCategories[0] != "speedtest" || edited.User.SiteDenyCategories[1] != "iplookup" {
		t.Fatalf("allow cats %+v", edited.User.SiteDenyCategories)
	}

	getSub := func(fmtName string) string {
		t.Helper()
		r, err := http.Get(ts.URL + "/api/sub/" + created.User.SubToken + "/" + fmtName)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 200 {
			t.Fatalf("%s sub %d %s", fmtName, r.StatusCode, b)
		}
		return string(b)
	}
	clash := getSub("clash")
	if strings.Contains(clash, "国内服务") || strings.Contains(clash, "geolocation-cn") {
		t.Fatalf("allow clash still domestic %s", clash)
	}
	if !strings.Contains(clash, "私有网络") {
		t.Fatalf("allow clash dropped private %s", clash)
	}
	sb := getSub("singbox")
	if strings.Contains(sb, "geolocation-cn") || strings.Contains(sb, "国内服务") {
		t.Fatalf("allow singbox still domestic %s", sb)
	}

	res = putJSON(userPath, map[string]any{"site_filter_mode": ""})
	decodeRes(t, res, &edited)
	if edited.User.SiteFilterMode != "" || len(edited.User.SiteDenyCategories) != 0 {
		t.Fatalf("explicit off %+v", edited.User)
	}
	clash = getSub("clash")
	if !strings.Contains(clash, "国内服务") {
		t.Fatalf("off should restore domestic %s", clash)
	}
}
