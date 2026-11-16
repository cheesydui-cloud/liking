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

	"liking/internal/corecfg"
	"liking/internal/db"
)

func TestTrafficForwardSubscriptionAndPackageUsers(t *testing.T) {
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

	mkServer := func(name, host string) int64 {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"name": name, "public_host": host})
		res, err := c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			Server struct {
				ID int64 `json:"id"`
			} `json:"server"`
		}
		decodeRes(t, res, &out)
		return out.Server.ID
	}
	entryID := mkServer("上海火山", "203.0.113.10")
	hopID := mkServer("沪日 IX", "203.0.113.11")
	landID := mkServer("美国家宽", "203.0.113.12")

	inBody, _ := json.Marshal(map[string]any{
		"server_id": landID, "name": "家宽", "profile": "socks5", "port": 18443,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var inbound struct {
		Inbound struct {
			ID int64 `json:"id"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &inbound)
	if inbound.Inbound.ID == 0 {
		t.Fatal("landing inbound")
	}

	fwBody, _ := json.Marshal(map[string]any{
		"name":               "沪日专线",
		"entry_server_id":    entryID,
		"entry_port":         21000,
		"landing_inbound_id": inbound.Inbound.ID,
		"landing_dial_host":  "203.0.113.22",
		"hops": []map[string]any{{
			"server_id": hopID, "listen_port": 21001, "dial_host": "203.0.113.21",
		}},
	})
	res, err = c.Post(ts.URL+"/api/traffic-forwards", "application/json", bytes.NewReader(fwBody))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Forward struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			EntryPort int    `json:"entry_port"`
			EntryHost string `json:"entry_host"`
			Hops      []struct {
				ListenPort int    `json:"listen_port"`
				DialHost   string `json:"dial_host"`
			} `json:"hops"`
		} `json:"forward"`
	}
	decodeRes(t, res, &created)
	if created.Forward.ID == 0 || created.Forward.EntryPort != 21000 || created.Forward.EntryHost != "203.0.113.10" {
		t.Fatalf("forward %+v", created.Forward)
	}
	if len(created.Forward.Hops) != 1 || created.Forward.Hops[0].DialHost != "203.0.113.21" || created.Forward.Hops[0].ListenPort != 21001 {
		t.Fatalf("hops %+v", created.Forward.Hops)
	}

	res, err = c.Get(ts.URL + "/api/inbounds")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Inbounds []struct {
			ServerID int64 `json:"server_id"`
		} `json:"inbounds"`
	}
	decodeRes(t, res, &listing)
	if len(listing.Inbounds) != 1 || listing.Inbounds[0].ServerID != landID {
		t.Fatalf("entry must not grow a node %+v", listing.Inbounds)
	}

	delIn, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/inbounds/"+strconv.FormatInt(inbound.Inbound.ID, 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Do(delIn)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("delete landing %d %s", res.StatusCode, b)
	}
	res.Body.Close()
	delSrv, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/servers/"+strconv.FormatInt(entryID, 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Do(delSrv)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusConflict {
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		t.Fatalf("delete entry %d %s", res.StatusCode, b)
	}
	res.Body.Close()

	steal, _ := json.Marshal(map[string]any{
		"server_id": entryID, "name": "占端口", "profile": "socks5", "port": 21000,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(steal))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == http.StatusOK {
		res.Body.Close()
		t.Fatal("node stole a forward port")
	}
	res.Body.Close()

	emptyPkg, _ := json.Marshal(map[string]any{
		"name": "空", "traffic_bytes": 0, "cycle_days": 0, "direction": "oneway",
		"inbound_ids": []int64{}, "forward_ids": []int64{},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(emptyPkg))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode == http.StatusOK {
		res.Body.Close()
		t.Fatal("empty package")
	}
	res.Body.Close()

	pkgBody, _ := json.Marshal(map[string]any{
		"name": "专线", "traffic_bytes": 0, "cycle_days": 0, "direction": "oneway",
		"inbound_ids": []int64{}, "forward_ids": []int64{created.Forward.ID},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
	if err != nil {
		t.Fatal(err)
	}
	var pkg struct {
		Package struct {
			ID         int64   `json:"id"`
			ForwardIDs []int64 `json:"forward_ids"`
			InboundIDs []int64 `json:"inbound_ids"`
		} `json:"package"`
	}
	decodeRes(t, res, &pkg)
	if len(pkg.Package.ForwardIDs) != 1 || pkg.Package.ForwardIDs[0] != created.Forward.ID || len(pkg.Package.InboundIDs) != 0 {
		t.Fatalf("package %+v", pkg.Package)
	}

	mkUser := func(name, remark string, packageID int64) (int64, string) {
		t.Helper()
		body, _ := json.Marshal(map[string]any{
			"username": name, "password": "passpass", "remark": remark, "package_id": packageID, "days": 30,
		})
		res, err := c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var out struct {
			User struct {
				ID       int64  `json:"id"`
				SubToken string `json:"sub_token"`
			} `json:"user"`
		}
		decodeRes(t, res, &out)
		if out.User.ID == 0 || out.User.SubToken == "" {
			t.Fatalf("user %+v", out.User)
		}
		return out.User.ID, out.User.SubToken
	}
	bobID, bobToken := mkUser("bob", "上海", pkg.Package.ID)
	_, _ = mkUser("carol", "", pkg.Package.ID)
	otherBody, _ := json.Marshal(map[string]any{
		"name": "其它", "traffic_bytes": 0, "cycle_days": 0, "direction": "oneway",
		"inbound_ids": []int64{inbound.Inbound.ID},
	})
	res, err = c.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(otherBody))
	if err != nil {
		t.Fatal(err)
	}
	var other struct {
		Package struct {
			ID int64 `json:"id"`
		} `json:"package"`
	}
	decodeRes(t, res, &other)
	mkUser("dave", "别的套餐", other.Package.ID)

	res, err = c.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var users struct {
		Users []struct {
			Username  string `json:"username"`
			Remark    string `json:"remark"`
			Role      string `json:"role"`
			PackageID *int64 `json:"package_id"`
			Enabled   bool   `json:"enabled"`
		} `json:"users"`
	}
	decodeRes(t, res, &users)
	var onPkg []string
	for _, u := range users.Users {
		if u.Role == "admin" || u.PackageID == nil || *u.PackageID != pkg.Package.ID {
			continue
		}
		onPkg = append(onPkg, u.Username+":"+u.Remark)
		if !u.Enabled {
			t.Fatalf("new user disabled %+v", u)
		}
	}
	if len(onPkg) != 2 || !strings.Contains(strings.Join(onPkg, ","), "bob:上海") || !strings.Contains(strings.Join(onPkg, ","), "carol:") {
		t.Fatalf("package users %+v", onPkg)
	}

	sub := func(token string) string {
		t.Helper()
		res, err := c.Get(ts.URL + "/api/sub/" + token + "/uri")
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(res.Body)
		if res.StatusCode != 200 {
			t.Fatalf("sub %d %s", res.StatusCode, raw)
		}
		decoded, err := base64.StdEncoding.DecodeString(string(raw))
		if err != nil {
			t.Fatalf("b64 %v %s", err, raw)
		}
		return string(decoded)
	}
	text := sub(bobToken)
	if !strings.Contains(text, "203.0.113.10:21000") || strings.Contains(text, "203.0.113.12") {
		t.Fatalf("sub %s", text)
	}
	if strings.Contains(text, "203.0.113.22") || strings.Contains(text, "203.0.113.21") {
		t.Fatalf("dial address leaked %s", text)
	}

	res, err = c.Get(ts.URL + "/api/packages/" + strconv.FormatInt(pkg.Package.ID, 10) + "/nodes")
	if err != nil {
		t.Fatal(err)
	}
	var preview struct {
		Nodes []struct {
			Name     string `json:"name"`
			Host     string `json:"host"`
			Port     int    `json:"port"`
			LineKind string `json:"line_kind"`
		} `json:"nodes"`
	}
	decodeRes(t, res, &preview)
	if len(preview.Nodes) != 1 || preview.Nodes[0].Host != "203.0.113.10" || preview.Nodes[0].Port != 21000 || preview.Nodes[0].LineKind != "traffic" {
		t.Fatalf("package preview %+v", preview.Nodes)
	}
	res, err = c.Get(ts.URL + "/api/users/" + strconv.FormatInt(bobID, 10) + "/nodes")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &preview)
	if len(preview.Nodes) != 1 || preview.Nodes[0].Host != "203.0.113.10" || preview.Nodes[0].Port != 21000 {
		t.Fatalf("user preview %+v", preview.Nodes)
	}

	built, err := corecfg.Build(d, entryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Apply.Forwards) != 1 || built.Apply.Forwards[0].DestHost != "203.0.113.21" || built.Apply.Forwards[0].DestPort != 21001 {
		t.Fatalf("entry rules %+v", built.Apply.Forwards)
	}
	hopBuilt, err := corecfg.Build(d, hopID)
	if err != nil {
		t.Fatal(err)
	}
	if len(hopBuilt.Apply.Forwards) != 1 || hopBuilt.Apply.Forwards[0].DestHost != "203.0.113.22" || hopBuilt.Apply.Forwards[0].DestPort != 18443 {
		t.Fatalf("hop rules %+v", hopBuilt.Apply.Forwards)
	}
	landBuilt, err := corecfg.Build(d, landID)
	if err != nil {
		t.Fatal(err)
	}
	if landBuilt.Apply.Forwards == nil || len(landBuilt.Apply.Forwards) != 0 {
		t.Fatalf("landing rules %+v", landBuilt.Apply.Forwards)
	}

	both, _ := json.Marshal(map[string]any{
		"inbound_ids": []int64{inbound.Inbound.ID},
		"forward_ids": []int64{created.Forward.ID},
	})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/packages/"+strconv.FormatInt(pkg.Package.ID, 10), bytes.NewReader(both))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	text = sub(bobToken)
	if !strings.Contains(text, "203.0.113.10:21000") || !strings.Contains(text, "203.0.113.12:18443") {
		t.Fatalf("both %s", text)
	}

	off, _ := json.Marshal(map[string]any{
		"name": "沪日专线", "enabled": false,
		"entry_server_id": entryID, "entry_port": 21000,
		"landing_inbound_id": inbound.Inbound.ID, "landing_dial_host": "203.0.113.22",
		"hops": []map[string]any{{"server_id": hopID, "listen_port": 21001, "dial_host": "203.0.113.21"}},
	})
	req, err = http.NewRequest(http.MethodPut, ts.URL+"/api/traffic-forwards/"+strconv.FormatInt(created.Forward.ID, 10), bytes.NewReader(off))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	text = sub(bobToken)
	if strings.Contains(text, "203.0.113.10:21000") || !strings.Contains(text, "203.0.113.12:18443") {
		t.Fatalf("paused %s", text)
	}
	clients, err := db.ListClientsByUser(d, bobID)
	if err != nil {
		t.Fatal(err)
	}
	kept := false
	for _, cl := range clients {
		if cl != nil && cl.InboundID == inbound.Inbound.ID {
			kept = true
		}
	}
	if !kept {
		t.Fatal("pause dropped the landing account")
	}

	delFw, err := http.NewRequest(http.MethodDelete, ts.URL+"/api/traffic-forwards/"+strconv.FormatInt(created.Forward.ID, 10), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err = c.Do(delFw)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = c.Get(ts.URL + "/api/inbounds")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &listing)
	found := false
	for _, in := range listing.Inbounds {
		if in.ServerID == landID {
			found = true
		}
	}
	if !found {
		t.Fatal("deleting the forward removed the landing node")
	}
}
