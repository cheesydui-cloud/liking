package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func TestUserTransitForwards(t *testing.T) {
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

	admin := loginClient(t, ts.URL, "admin", "secret12")
	var made struct {
		Server struct {
			ID int64 `json:"id"`
		} `json:"server"`
	}
	decodeRes(t, transitDo(t, admin, http.MethodPost, ts.URL+"/api/servers", map[string]any{
		"name": "线路机", "public_host": "203.0.113.10",
	}), &made)
	srvID := made.Server.ID

	mk := func(name, profile string, port int, extra map[string]any) int64 {
		t.Helper()
		body := map[string]any{
			"server_id": srvID, "name": name, "profile": profile, "port": port,
		}
		for k, v := range extra {
			body[k] = v
		}
		var out struct {
			Inbound struct {
				ID int64 `json:"id"`
			} `json:"inbound"`
		}
		decodeRes(t, transitDo(t, admin, http.MethodPost, ts.URL+"/api/inbounds", body), &out)
		if out.Inbound.ID == 0 {
			t.Fatalf("inbound %s id 0", name)
		}
		return out.Inbound.ID
	}
	entry := mk("入口甲", "socks5", 18443, nil)
	other := mk("入口乙", "socks5", 18444, nil)
	pipe := mk("共享端口", "port-forward", 18445, map[string]any{
		"settings": map[string]any{"dest_host": "1.1.1.1", "dest_port": 80, "network": "tcp"},
	})
	sharedChain := mk("共享链式", "socks5", 18446, map[string]any{
		"line_kind": "chain",
		"exit_uri":  "socks5://u:p@203.0.113.9:1080",
	})

	bad, _ := json.Marshal(map[string]any{
		"name": "bad", "traffic_bytes": 0, "direction": "oneway",
		"inbound_ids": []int64{pipe}, "transit_ids": []int64{pipe},
	})
	res, err := admin.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("port transit status %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	badChain, _ := json.Marshal(map[string]any{
		"name": "bad-chain", "traffic_bytes": 0, "direction": "oneway",
		"inbound_ids": []int64{sharedChain}, "transit_ids": []int64{sharedChain},
	})
	res, err = admin.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(badChain))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("chain transit status %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	var pkg struct {
		Package struct {
			ID         int64   `json:"id"`
			InboundIDs []int64 `json:"inbound_ids"`
			TransitIDs []int64 `json:"transit_ids"`
		} `json:"package"`
	}
	decodeRes(t, transitDo(t, admin, http.MethodPost, ts.URL+"/api/packages", map[string]any{
		"name": "标准", "traffic_bytes": 0, "direction": "oneway",
		"inbound_ids": []int64{entry, other},
		"transit_ids": []int64{entry, other},
	}), &pkg)
	if len(pkg.Package.TransitIDs) != 2 {
		t.Fatalf("transit %+v", pkg.Package.TransitIDs)
	}

	var plain struct {
		Package struct {
			ID int64 `json:"id"`
		} `json:"package"`
	}
	decodeRes(t, transitDo(t, admin, http.MethodPost, ts.URL+"/api/packages", map[string]any{
		"name": "普通", "traffic_bytes": 0, "direction": "oneway",
		"inbound_ids": []int64{entry},
	}), &plain)

	createUser := func(name, password string, pkgID int64) {
		t.Helper()
		decodeRes(t, transitDo(t, admin, http.MethodPost, ts.URL+"/api/users", map[string]any{
			"username": name, "password": password, "package_id": pkgID, "days": 30,
		}), nil)
	}
	createUser("alice", "alice12", pkg.Package.ID)
	createUser("bob", "bobpass", pkg.Package.ID)
	createUser("carol", "carol12", plain.Package.ID)

	alice := loginClient(t, ts.URL, "alice", "alice12")
	bob := loginClient(t, ts.URL, "bob", "bobpass")
	carol := loginClient(t, ts.URL, "carol", "carol12")

	var me struct {
		User struct {
			Transit bool `json:"transit"`
		} `json:"user"`
	}
	decodeRes(t, transitDo(t, alice, http.MethodGet, ts.URL+"/api/me", nil), &me)
	if !me.User.Transit {
		t.Fatal("alice should see transit")
	}
	me.User.Transit = false
	decodeRes(t, transitDo(t, carol, http.MethodGet, ts.URL+"/api/me", nil), &me)
	if me.User.Transit {
		t.Fatal("carol has no transit mark")
	}

	res = transitDo(t, admin, http.MethodPost, ts.URL+"/api/me/forwards", map[string]any{
		"entry_inbound_id": entry, "kind": "chain", "exit_uri": "socks5://u:p@203.0.113.9:1080",
	})
	if res.StatusCode != http.StatusForbidden {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("admin create %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	res = transitDo(t, carol, http.MethodPost, ts.URL+"/api/me/forwards", map[string]any{
		"entry_inbound_id": entry, "kind": "chain", "exit_uri": "socks5://u:p@203.0.113.9:1080",
	})
	if res.StatusCode != http.StatusBadRequest {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("carol create %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	var created struct {
		Forward struct {
			ID      int64  `json:"id"`
			Port    int    `json:"port"`
			URI     string `json:"uri"`
			Kind    string `json:"kind"`
			Enabled bool   `json:"enabled"`
		} `json:"forward"`
	}
	decodeRes(t, transitDo(t, alice, http.MethodPost, ts.URL+"/api/me/forwards", map[string]any{
		"entry_inbound_id": entry,
		"kind":             "chain",
		"name":             "我的链式",
		"port":             20001,
		"exit_uri":         "socks5://u:p@203.0.113.9:1080",
	}), &created)
	if created.Forward.ID == 0 || created.Forward.Port != 20001 || created.Forward.Kind != "chain" || created.Forward.URI == "" {
		t.Fatalf("chain %+v", created.Forward)
	}
	chainID := created.Forward.ID

	created.Forward = struct {
		ID      int64  `json:"id"`
		Port    int    `json:"port"`
		URI     string `json:"uri"`
		Kind    string `json:"kind"`
		Enabled bool   `json:"enabled"`
	}{}
	decodeRes(t, transitDo(t, alice, http.MethodPost, ts.URL+"/api/me/forwards", map[string]any{
		"entry_inbound_id": other,
		"kind":             "port",
		"name":             "我的端口",
		"port":             20002,
		"dest_host":        "203.0.113.20",
		"dest_port":        443,
		"network":          "tcp",
	}), &created)
	if created.Forward.Kind != "port" || created.Forward.Port != 20002 || created.Forward.URI != "" {
		t.Fatalf("port %+v", created.Forward)
	}
	portID := created.Forward.ID

	b, err := corecfg.Build(d, srvID)
	if err != nil {
		t.Fatal(err)
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	doorTag := ""
	for _, raw := range xray["inbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["protocol"] != "dokodemo-door" {
			continue
		}
		st, _ := obj["settings"].(map[string]any)
		if st["address"] == "203.0.113.20" && st["port"] == float64(443) {
			doorTag, _ = obj["tag"].(string)
		}
	}
	if doorTag == "" {
		t.Fatal("personal port-forward missing dokodemo")
	}
	outTag := ""
	rules, _ := xray["routing"].(map[string]any)["rules"].([]any)
	for _, raw := range rules {
		obj, _ := raw.(map[string]any)
		tags, _ := obj["inboundTag"].([]any)
		for _, tag := range tags {
			if tag == doorTag {
				outTag, _ = obj["outboundTag"].(string)
			}
		}
	}
	if outTag == "" || outTag == "direct" {
		t.Fatalf("personal port outbound %q", outTag)
	}
	var mark float64
	for _, raw := range xray["outbounds"].([]any) {
		obj, _ := raw.(map[string]any)
		if obj["tag"] != outTag {
			continue
		}
		stream, _ := obj["streamSettings"].(map[string]any)
		sock, _ := stream["sockopt"].(map[string]any)
		mark, _ = sock["mark"].(float64)
	}
	if mark == 0 {
		t.Fatal("personal port-forward has no acct mark")
	}
	seenMark := false
	for _, m := range b.Apply.AcctMarks {
		if float64(m.Mark) == mark {
			seenMark = true
		}
	}
	if !seenMark {
		t.Fatalf("acct marks missing %v in %+v", mark, b.Apply.AcctMarks)
	}

	names := func(c *http.Client) []string {
		t.Helper()
		var nodes struct {
			Nodes []struct {
				Name string `json:"name"`
			} `json:"nodes"`
		}
		decodeRes(t, transitDo(t, c, http.MethodGet, ts.URL+"/api/me/nodes", nil), &nodes)
		out := make([]string, 0, len(nodes.Nodes))
		for _, n := range nodes.Nodes {
			out = append(out, n.Name)
		}
		return out
	}
	if !hasName(names(alice), "我的链式") || hasName(names(alice), "我的端口") {
		t.Fatalf("alice nodes %v", names(alice))
	}
	if hasName(names(bob), "我的链式") || hasName(names(bob), "我的端口") {
		t.Fatalf("bob nodes %v", names(bob))
	}
	if hasName(names(admin), "我的链式") {
		t.Fatalf("admin nodes include personal chain %v", names(admin))
	}

	var bobList struct {
		Forwards []struct {
			ID int64 `json:"id"`
		} `json:"forwards"`
		Entries []struct {
			ID int64 `json:"id"`
		} `json:"entries"`
	}
	decodeRes(t, transitDo(t, bob, http.MethodGet, ts.URL+"/api/me/forwards", nil), &bobList)
	if len(bobList.Forwards) != 0 || len(bobList.Entries) != 2 {
		t.Fatalf("bob forwards %+v entries %+v", bobList.Forwards, bobList.Entries)
	}
	res = transitDo(t, bob, http.MethodPut, ts.URL+"/api/me/forwards/"+strconv.FormatInt(chainID, 10), map[string]any{"name": "偷"})
	if res.StatusCode != http.StatusNotFound {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("bob update %d %s", res.StatusCode, body)
	}
	res.Body.Close()

	ownedPkg, _ := json.Marshal(map[string]any{
		"name": "偷套餐", "traffic_bytes": 0, "direction": "oneway",
		"inbound_ids": []int64{chainID},
	})
	res, err = admin.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(ownedPkg))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("owned in package %d %s", res.StatusCode, body)
	}
	res.Body.Close()

	decodeRes(t, transitDo(t, alice, http.MethodPut, ts.URL+"/api/me/forwards/"+strconv.FormatInt(chainID, 10), map[string]any{
		"enabled": false,
	}), nil)

	decodeRes(t, transitDo(t, admin, http.MethodPut, ts.URL+"/api/packages/"+strconv.FormatInt(pkg.Package.ID, 10), map[string]any{
		"inbound_ids": []int64{entry, other},
		"transit_ids": []int64{},
	}), nil)
	saved, err := db.GetPackage(d, pkg.Package.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.TransitIDs) != 0 {
		t.Fatalf("transit still %+v", saved.TransitIDs)
	}

	type fwd struct {
		ID       int64 `json:"id"`
		Enabled  bool  `json:"enabled"`
		AuthHold bool  `json:"auth_hold"`
	}
	var list struct {
		Forwards []fwd `json:"forwards"`
	}
	decodeRes(t, transitDo(t, alice, http.MethodGet, ts.URL+"/api/me/forwards", nil), &list)
	byID := map[int64]fwd{}
	for _, row := range list.Forwards {
		byID[row.ID] = row
	}
	if byID[chainID].Enabled || byID[chainID].AuthHold {
		t.Fatalf("user-disabled chain %+v", byID[chainID])
	}
	if byID[portID].Enabled || !byID[portID].AuthHold {
		t.Fatalf("revoked port %+v", byID[portID])
	}

	decodeRes(t, transitDo(t, admin, http.MethodPut, ts.URL+"/api/packages/"+strconv.FormatInt(pkg.Package.ID, 10), map[string]any{
		"inbound_ids": []int64{entry, other},
		"transit_ids": []int64{entry, other},
	}), nil)
	decodeRes(t, transitDo(t, alice, http.MethodGet, ts.URL+"/api/me/forwards", nil), &list)
	byID = map[int64]fwd{}
	for _, row := range list.Forwards {
		byID[row.ID] = row
	}
	if byID[chainID].Enabled || byID[chainID].AuthHold {
		t.Fatalf("chain resumed itself %+v", byID[chainID])
	}
	if !byID[portID].Enabled || byID[portID].AuthHold {
		t.Fatalf("port not resumed %+v", byID[portID])
	}

	decodeRes(t, transitDo(t, admin, http.MethodPut, ts.URL+"/api/packages/"+strconv.FormatInt(pkg.Package.ID, 10), map[string]any{
		"inbound_ids": []int64{entry, other},
		"transit_ids": []int64{},
	}), nil)
	res = transitDo(t, alice, http.MethodPut, ts.URL+"/api/me/forwards/"+strconv.FormatInt(portID, 10), map[string]any{
		"enabled": true,
	})
	if res.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("enable revoked %d %s", res.StatusCode, body)
	}
	res.Body.Close()

	ids, err := db.PackageInboundIDs(d, &db.Package{ServerIDs: []int64{srvID}})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		if id == chainID || id == portID {
			t.Fatalf("server mode leaked personal inbound %d in %v", id, ids)
		}
	}
}

func loginClient(t *testing.T, base, user, pass string) *http.Client {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := &http.Client{Jar: jar}
	decodeRes(t, transitDo(t, c, http.MethodPost, base+"/api/login", map[string]string{
		"username": user, "password": pass,
	}), nil)
	return c
}

func transitDo(t *testing.T, c *http.Client, method, url string, body any) *http.Response {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rdr = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}
