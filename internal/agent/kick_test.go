package agent

import (
	"encoding/json"
	"testing"
)

func TestExtractXrayKickTargets(t *testing.T) {
	raw := json.RawMessage(`{
		"inbounds": [
			{"tag":"api","settings":{"clients":[{"email":"u1.i1"}]}},
			{"tag":"in-2","settings":{"clients":[
				{"email":"u1.i2","id":"aaaa"},
				{"email":"u9.i2","id":"bbbb"}
			]}}
		]
	}`)
	got := extractXrayKickTargets(raw, map[string]struct{}{"u1.i2": {}})
	if len(got) != 1 || got[0].Tag != "in-2" {
		t.Fatalf("%+v", got)
	}
	var u struct {
		Email string `json:"email"`
		ID    string `json:"id"`
	}
	if err := json.Unmarshal(got[0].User, &u); err != nil {
		t.Fatal(err)
	}
	if u.Email != "u1.i2" || u.ID != "aaaa" {
		t.Fatalf("user %+v", u)
	}
	if extractXrayKickTargets(nil, map[string]struct{}{"u1.i2": {}}) != nil {
		t.Fatal("empty cfg")
	}
}

func TestClashIDsForEmails(t *testing.T) {
	ids := clashIDsForEmails([]clashConn{
		{ID: "a", User: "u1.i2"},
		{ID: "", User: "u1.i2"},
		{ID: "b", User: "other"},
		{ID: "c", User: "u1.i2"},
	}, map[string]struct{}{"u1.i2": {}})
	if len(ids) != 2 || ids[0] != "a" || ids[1] != "c" {
		t.Fatalf("%v", ids)
	}
}
