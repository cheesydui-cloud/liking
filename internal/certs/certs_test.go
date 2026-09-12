package certs

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestSplitAndDNSNames(t *testing.T) {
	got := SplitNames("Example.COM, *.example.com\nwww.example.com")
	if len(got) != 3 || got[0] != "example.com" || got[1] != "*.example.com" {
		t.Fatalf("%v", got)
	}
	if DNS01Name("*.example.com") != "_acme-challenge.example.com" {
		t.Fatal(DNS01Name("*.example.com"))
	}
	z := ZoneCandidates("_acme-challenge.www.example.com")
	if len(z) < 2 || z[0] != "www.example.com" || z[len(z)-1] != "example.com" {
		t.Fatalf("%v", z)
	}
	if _, err := NormalizeDNS([]string{"not a host"}); err == nil {
		t.Fatal("expected invalid")
	}
	if _, err := NormalizeDNS([]string{"1.2.3.4"}); err == nil {
		t.Fatal("ip")
	}
	names, err := NormalizeDNS([]string{"Example.com", "*.example.com"})
	if err != nil || len(names) != 2 {
		t.Fatalf("%v %v", names, err)
	}
}

func TestSelfSignAndParse(t *testing.T) {
	certPEM, keyPEM, exp, err := SelfSign("t.example", []string{"t.example.com", "*.t.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	if exp <= 0 || !strings.Contains(certPEM, "BEGIN CERTIFICATE") {
		t.Fatal("pem")
	}
	key, err := ParsePrivateKey(keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	if err := KeyMatchesCert(certPEM, key); err != nil {
		t.Fatal(err)
	}
	domains, notAfter, err := ParseMeta(certPEM)
	if err != nil {
		t.Fatal(err)
	}
	if notAfter.Unix() != exp {
		t.Fatalf("exp %d %d", notAfter.Unix(), exp)
	}
	joined := strings.Join(domains, " ")
	if !strings.Contains(joined, "t.example.com") {
		t.Fatalf("domains %v", domains)
	}
	block, _ := pem.Decode([]byte(certPEM))
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.VerifyHostname("foo.t.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflarePresentAndCleanup(t *testing.T) {
	type rec struct {
		ID, Name, Content string
	}
	var recs []rec
	seq := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"errors":  []map[string]any{{"code": 9103, "message": "auth"}},
			})
			return
		}
		ok := func(result any) {
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			ok([]map[string]string{{"id": "zid1", "name": "example.com", "status": "active"}})
		case r.URL.Path == "/zones/zid1/dns_records" && r.Method == http.MethodGet:
			var out []map[string]string
			for _, x := range recs {
				out = append(out, map[string]string{"id": x.ID, "type": "TXT", "name": x.Name, "content": x.Content})
			}
			if out == nil {
				out = []map[string]string{}
			}
			ok(out)
		case r.URL.Path == "/zones/zid1/dns_records" && r.Method == http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			var req struct {
				Name, Content string
			}
			_ = json.Unmarshal(body, &req)
			seq++
			id := "rid" + strconv.Itoa(seq)
			recs = append(recs, rec{ID: id, Name: req.Name, Content: req.Content})
			ok(map[string]string{"id": id})
		case strings.HasPrefix(r.URL.Path, "/zones/zid1/dns_records/") && r.Method == http.MethodDelete:
			id := strings.TrimPrefix(r.URL.Path, "/zones/zid1/dns_records/")
			var keep []rec
			for _, x := range recs {
				if x.ID != id {
					keep = append(keep, x)
				}
			}
			recs = keep
			ok(map[string]string{"id": id})
		default:
			http.Error(w, r.Method+" "+r.URL.Path, 404)
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cf := &Cloudflare{Token: "tok", API: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()
	if err := cf.Present(ctx, "_acme-challenge.example.com", "abc"); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("created %d", len(recs))
	}
	if err := cf.Present(ctx, "_acme-challenge.example.com", "abc"); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("dup %d", len(recs))
	}
	if err := cf.DeleteTXT(ctx, "_acme-challenge.example.com", "abc"); err != nil {
		t.Fatal(err)
	}
	if len(recs) != 0 {
		t.Fatalf("left %v", recs)
	}
}

func TestCloudflareListHostNames(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": false,
				"errors":  []map[string]any{{"code": 9103, "message": "auth"}},
			})
			return
		}
		ok := func(result any, page, total int) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success":     true,
				"result":      result,
				"result_info": map[string]int{"page": page, "per_page": 50, "total_pages": total, "count": 1, "total_count": total},
			})
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page < 1 {
			page = 1
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/zones":
			if page == 1 {
				ok([]map[string]string{{"id": "z1", "name": "example.com", "status": "active"}}, 1, 2)
				return
			}
			ok([]map[string]string{{"id": "z2", "name": "other.org", "status": "active"}}, 2, 2)
		case r.URL.Path == "/zones/z1/dns_records" && r.URL.Query().Get("type") == "A":
			ok([]map[string]any{
				{"id": "r1", "type": "A", "name": "hk.example.com", "content": "31.40.214.186", "proxied": false},
				{"id": "r2", "type": "A", "name": "*.example.com", "content": "1.1.1.1", "proxied": true},
				{"id": "r3", "type": "A", "name": "_acme-challenge.example.com", "content": "x", "proxied": false},
				{"id": "r4", "type": "A", "name": "www.example.com", "content": "9.9.9.9", "proxied": true},
				{"id": "r5", "type": "A", "name": "example.com", "content": "31.40.214.186", "proxied": false},
			}, 1, 1)
		case strings.HasPrefix(r.URL.Path, "/zones/") && strings.HasSuffix(r.URL.Path, "/dns_records"):
			ok([]map[string]any{}, 1, 1)
		default:
			http.Error(w, r.Method+" "+r.URL.Path, 404)
		}
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	cf := &Cloudflare{Token: "tok", API: ts.URL, HTTP: ts.Client()}
	got, err := cf.ListHostNames(context.Background(), "31.40.214.186", "example.com")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]HostName{}
	for _, h := range got {
		by[h.Name] = h
	}
	if _, ok := by["*.example.com"]; ok {
		t.Fatal("wildcard")
	}
	if _, ok := by["_acme-challenge.example.com"]; ok {
		t.Fatal("acme")
	}
	hk, ok := by["hk.example.com"]
	if !ok || !hk.Matched || hk.Proxied || hk.Content != "31.40.214.186" {
		t.Fatalf("hk %+v", hk)
	}
	apex, ok := by["example.com"]
	if !ok || !apex.Matched {
		t.Fatalf("apex %+v", apex)
	}
	www, ok := by["www.example.com"]
	if !ok || !www.Proxied || www.Matched {
		t.Fatalf("www %+v", www)
	}
	if _, ok := by["other.org"]; !ok {
		t.Fatalf("missing paged zone: %v", got)
	}
	if len(got) < 2 || !got[0].Matched {
		t.Fatalf("matched should sort first: %+v", got)
	}
}
