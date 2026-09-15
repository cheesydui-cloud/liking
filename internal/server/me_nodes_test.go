package server

import (
	"bytes"
	"encoding/base64"
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

func TestMeNodesHiddenStarredAndPreview(t *testing.T) {
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
	aliceHash, err := HashPassword("alice12")
	if err != nil {
		t.Fatal(err)
	}
	alice, err := db.CreateUser(d, "alice", aliceHash, "user", "")
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

	mkIn := func(name string, port int) int64 {
		t.Helper()
		inBody, _ := json.Marshal(map[string]any{
			"server_id": created.Server.ID,
			"name":      name,
			"profile":   "vless-reality-vision",
			"port":      port,
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
		if out.Inbound.ID == 0 {
			t.Fatalf("inbound id 0 for %s", name)
		}
		return out.Inbound.ID
	}
	idA := mkIn("node-a", 8443)
	idB := mkIn("node-b", 8444)

	res, err = c.Get(ts.URL + "/api/me/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var nodes struct {
		Nodes []struct {
			ID         int64  `json:"id"`
			Name       string `json:"name"`
			ServerID   int64  `json:"server_id"`
			URI        string `json:"uri"`
			PeriodUp   int64  `json:"period_up"`
			UsedUp     int64  `json:"used_up"`
		} `json:"nodes"`
		Hidden []struct {
			ID     int64  `json:"id"`
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"hidden"`
		Starred []int64 `json:"starred"`
	}
	decodeRes(t, res, &nodes)
	if len(nodes.Nodes) != 2 || len(nodes.Hidden) != 0 {
		t.Fatalf("live nodes %+v hidden %+v", nodes.Nodes, nodes.Hidden)
	}
	if nodes.Nodes[0].ServerID != created.Server.ID {
		t.Fatalf("server_id %+v", nodes.Nodes[0])
	}
	if !strings.HasPrefix(nodes.Nodes[0].URI, "vless://") {
		t.Fatalf("uri %s", nodes.Nodes[0].URI)
	}

	dis, _ := json.Marshal(map[string]any{"enabled": false, "name": "node-b", "port": 8444})
	res = doJSON(t, c, http.MethodPut, ts.URL+"/api/inbounds/"+itoa(idB), dis)
	decodeRes(t, res, nil)
	res, err = c.Get(ts.URL + "/api/me/nodes")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &nodes)
	if len(nodes.Nodes) != 1 || nodes.Nodes[0].ID != idA {
		t.Fatalf("after disable nodes %+v", nodes.Nodes)
	}
	if len(nodes.Hidden) != 1 || nodes.Hidden[0].ID != idB || nodes.Hidden[0].Reason != "停用" {
		t.Fatalf("hidden %+v", nodes.Hidden)
	}

	en, _ := json.Marshal(map[string]any{"enabled": true, "name": "node-b", "port": 8444})
	res = doJSON(t, c, http.MethodPut, ts.URL+"/api/inbounds/"+itoa(idB), en)
	decodeRes(t, res, nil)

	star, _ := json.Marshal(map[string]any{"inbound_ids": []int64{idA}})
	res = doJSON(t, c, http.MethodPut, ts.URL+"/api/me/starred", star)
	var starred struct {
		Starred []int64 `json:"starred"`
	}
	decodeRes(t, res, &starred)
	if len(starred.Starred) != 1 || starred.Starred[0] != idA {
		t.Fatalf("starred %+v", starred)
	}

	res, err = c.Get(ts.URL + "/api/me")
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		User struct {
			SubToken string `json:"sub_token"`
		} `json:"user"`
	}
	decodeRes(t, res, &me)
	res, err = c.Get(ts.URL + "/api/sub/" + me.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("sub %d %s", res.StatusCode, b)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	decoded, err := base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("b64 %v %s", err, raw)
	}
	text := strings.TrimSpace(string(decoded))
	if !strings.Contains(text, "10.0.0.1:8443") || strings.Contains(text, "10.0.0.1:8444") {
		t.Fatalf("starred sub %s", text)
	}

	clear, _ := json.Marshal(map[string]any{"inbound_ids": []int64{}})
	res = doJSON(t, c, http.MethodPut, ts.URL+"/api/me/starred", clear)
	decodeRes(t, res, nil)
	res, err = c.Get(ts.URL + "/api/sub/" + me.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ = io.ReadAll(res.Body)
	res.Body.Close()
	decoded, err = base64.StdEncoding.DecodeString(string(raw))
	if err != nil {
		t.Fatalf("b64 %v", err)
	}
	text = strings.TrimSpace(string(decoded))
	if !strings.Contains(text, "10.0.0.1:8443") || !strings.Contains(text, "10.0.0.1:8444") {
		t.Fatalf("all sub %s", text)
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
	bind, _ := json.Marshal(map[string]any{"package_id": pkg.Package.ID})
	res = doJSON(t, c, http.MethodPut, ts.URL+"/api/users/"+itoa(alice.ID), bind)
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/users/" + itoa(alice.ID) + "/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var preview struct {
		Nodes []struct {
			Name string `json:"name"`
			URI  string `json:"uri"`
		} `json:"nodes"`
	}
	decodeRes(t, res, &preview)
	if len(preview.Nodes) != 2 {
		t.Fatalf("user preview %+v", preview.Nodes)
	}
	if preview.Nodes[0].URI != "" {
		t.Fatalf("preview must not include uri %+v", preview.Nodes[0])
	}

	res, err = c.Get(ts.URL + "/api/packages/" + itoa(pkg.Package.ID) + "/nodes")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &preview)
	if len(preview.Nodes) != 2 {
		t.Fatalf("package preview %+v", preview.Nodes)
	}

	aliceJar, _ := cookiejar.New(nil)
	aliceC := &http.Client{Jar: aliceJar}
	aliceLogin, _ := json.Marshal(map[string]string{"username": "alice", "password": "alice12"})
	res, err = aliceC.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(aliceLogin))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res = doJSON(t, aliceC, http.MethodPut, ts.URL+"/api/me/starred", star)
	if res.StatusCode != 403 {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("alice starred %d %s", res.StatusCode, b)
	}
	res.Body.Close()
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}

func doJSON(t *testing.T, c *http.Client, method, url string, body []byte) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, url, bytes.NewReader(body))
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
