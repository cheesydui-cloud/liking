package server

import (
	"bytes"
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

func TestSetUserTrafficSyncsSubscription(t *testing.T) {
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

	admin := &http.Client{}
	admin.Jar, _ = cookiejar.New(nil)
	login, _ := json.Marshal(map[string]string{"username": "admin", "password": "secret12"})
	res, err := admin.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(login))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	srvBody, _ := json.Marshal(map[string]string{"name": "n1", "public_host": "10.0.0.1"})
	res, err = admin.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(srvBody))
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
		"name": "year", "traffic_bytes": int64(400) << 30, "cycle_days": 0, "direction": "oneway",
		"server_ids": []int64{createdSrv.Server.ID},
	})
	res, err = admin.Post(ts.URL+"/api/packages", "application/json", bytes.NewReader(pkgBody))
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
		"username": "neohero", "password": "passpass", "package_id": pkg.Package.ID,
	})
	res, err = admin.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(uBody))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		User struct {
			ID       int64  `json:"id"`
			SubToken string `json:"sub_token"`
		} `json:"user"`
	}
	decodeRes(t, res, &created)
	if created.User.ID == 0 || created.User.SubToken == "" {
		t.Fatal("missing user")
	}
	if err := db.AddUserTraffic(d, created.User.ID, 100, 200); err != nil {
		t.Fatal(err)
	}
	before, err := db.GetUser(d, created.User.ID)
	if err != nil {
		t.Fatal(err)
	}

	want := int64(5 * 1024 * 1024)
	setBody, _ := json.Marshal(map[string]any{"bytes": want})
	res, err = admin.Post(ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10)+"/used-traffic", "application/json", bytes.NewReader(setBody))
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		User struct {
			BilledBytes int64  `json:"billed_bytes"`
			UsedUp      int64  `json:"used_up"`
			UsedDown    int64  `json:"used_down"`
			Direction   string `json:"direction"`
		} `json:"user"`
	}
	decodeRes(t, res, &set)
	if set.User.BilledBytes != want || set.User.UsedUp != 0 || set.User.UsedDown != want || set.User.Direction != "oneway" {
		t.Fatalf("set view %+v", set.User)
	}

	res, err = admin.Get(ts.URL + "/api/users/" + strconv.FormatInt(created.User.ID, 10) + "/traffic")
	if err != nil {
		t.Fatal(err)
	}
	var traffic struct {
		Billed int64 `json:"billed_bytes"`
		Up     int64 `json:"used_up"`
		Down   int64 `json:"used_down"`
	}
	decodeRes(t, res, &traffic)
	if traffic.Billed != want || traffic.Up != 0 || traffic.Down != want {
		t.Fatalf("admin traffic %+v", traffic)
	}

	res, err = http.Get(ts.URL + "/api/sub/" + created.User.SubToken + "/uri")
	if err != nil {
		t.Fatal(err)
	}
	info := res.Header.Get("Subscription-Userinfo")
	res.Body.Close()
	if !strings.Contains(info, "download="+strconv.FormatInt(want, 10)) {
		t.Fatalf("userinfo %q", info)
	}

	member := &http.Client{}
	member.Jar, _ = cookiejar.New(nil)
	memberLogin, _ := json.Marshal(map[string]string{"username": "neohero", "password": "passpass"})
	res, err = member.Post(ts.URL+"/api/login", "application/json", bytes.NewReader(memberLogin))
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)
	res, err = member.Get(ts.URL + "/api/me/traffic")
	if err != nil {
		t.Fatal(err)
	}
	var me struct {
		Billed int64 `json:"billed_bytes"`
	}
	decodeRes(t, res, &me)
	if me.Billed != want {
		t.Fatalf("subscription page %d", me.Billed)
	}

	after, err := db.GetUser(d, created.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.CycleStart != before.CycleStart {
		t.Fatalf("cycle moved %d -> %d", before.CycleStart, after.CycleStart)
	}
	if err := db.AddUserTraffic(d, created.User.ID, 9, 7); err != nil {
		t.Fatal(err)
	}
	res, err = member.Get(ts.URL + "/api/me/traffic")
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, &me)
	if me.Billed != want+7 {
		t.Fatalf("later use %d", me.Billed)
	}

	bad, _ := json.Marshal(map[string]any{"bytes": -1})
	res, err = admin.Post(ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10)+"/used-traffic", "application/json", bytes.NewReader(bad))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusBadRequest {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("negative status %d %s", res.StatusCode, body)
	}
	res.Body.Close()

	res, err = member.Post(ts.URL+"/api/users/"+strconv.FormatInt(created.User.ID, 10)+"/used-traffic", "application/json", bytes.NewReader(setBody))
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != http.StatusForbidden {
		body, _ := io.ReadAll(res.Body)
		t.Fatalf("member status %d %s", res.StatusCode, body)
	}
	res.Body.Close()
}
