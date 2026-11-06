package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"

	"liking/internal/db"
)

func TestCardOrderAPI(t *testing.T) {
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

	var ids []int64
	for _, name := range []string{"ann", "bob", "cid"} {
		body, _ := json.Marshal(map[string]string{"username": name, "password": "secret12"})
		res, err = c.Post(ts.URL+"/api/users", "application/json", bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		var created struct {
			User struct {
				ID int64 `json:"id"`
			} `json:"user"`
		}
		decodeRes(t, res, &created)
		ids = append(ids, created.User.ID)
	}
	moved := []int64{ids[2], ids[0], ids[1]}
	payload, _ := json.Marshal(map[string]any{"kind": "users", "ids": moved})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/card-order", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	decodeRes(t, res, nil)

	res, err = c.Get(ts.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	var listed struct {
		Users []struct {
			ID   int64  `json:"id"`
			Role string `json:"role"`
		} `json:"users"`
	}
	decodeRes(t, res, &listed)
	var got []int64
	for _, u := range listed.Users {
		if u.Role == "admin" {
			continue
		}
		got = append(got, u.ID)
	}
	if len(got) != 3 || got[0] != moved[0] || got[1] != moved[1] || got[2] != moved[2] {
		t.Fatalf("api order %#v want %#v", got, moved)
	}
	byID, err := db.ListUsers(d)
	if err != nil {
		t.Fatal(err)
	}
	if byID[1].ID != ids[0] || byID[2].ID != ids[1] || byID[3].ID != ids[2] {
		t.Fatal("provision list followed display order")
	}

	bad, _ := json.Marshal(map[string]any{"kind": "nope", "ids": moved})
	req, _ = http.NewRequest(http.MethodPut, ts.URL+"/api/card-order", bytes.NewReader(bad))
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", res.StatusCode)
	}
}
