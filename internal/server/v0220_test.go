package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strconv"
	"testing"

	"liking/internal/corecfg"
	"liking/internal/db"
)

func TestInboundRejectCN(t *testing.T) {
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

	sbody, _ := json.Marshal(map[string]string{"name": "jp", "public_host": "198.51.100.8"})
	res, err = c.Post(ts.URL+"/api/servers", "application/json", bytes.NewReader(sbody))
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
		"server_id": createdSrv.Server.ID, "name": "land", "profile": "vless-reality-vision", "port": 8443, "reject_cn": true,
	})
	res, err = c.Post(ts.URL+"/api/inbounds", "application/json", bytes.NewReader(inBody))
	if err != nil {
		t.Fatal(err)
	}
	var created struct {
		Inbound struct {
			ID       int64 `json:"id"`
			RejectCN bool  `json:"reject_cn"`
			Port     int   `json:"port"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &created)
	if !created.Inbound.RejectCN || created.Inbound.Port != 8443 {
		t.Fatalf("%+v", created.Inbound)
	}

	b, err := corecfg.Build(d, createdSrv.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.RejectCN.Ports) != 1 || b.Apply.RejectCN.Ports[0] != 8443 {
		t.Fatalf("apply %+v", b.Apply.RejectCN)
	}

	off, _ := json.Marshal(map[string]any{"reject_cn": false})
	req, err := http.NewRequest(http.MethodPut, ts.URL+"/api/inbounds/"+strconv.FormatInt(created.Inbound.ID, 10), bytes.NewReader(off))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	res, err = c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var updated struct {
		Inbound struct {
			RejectCN bool `json:"reject_cn"`
		} `json:"inbound"`
	}
	decodeRes(t, res, &updated)
	if updated.Inbound.RejectCN {
		t.Fatal("still on")
	}
	b, err = corecfg.Build(d, createdSrv.Server.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.RejectCN.Ports) != 0 {
		t.Fatalf("cleared %+v", b.Apply.RejectCN)
	}
}
