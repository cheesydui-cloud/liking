package db

import "testing"

func TestSpliceDisplayIDs(t *testing.T) {
	got, err := SpliceDisplayIDs([]int64{1, 2, 3, 4, 5}, []int64{5, 1, 3})
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{5, 2, 1, 4, 3}
	if len(got) != len(want) {
		t.Fatalf("%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v", got)
		}
	}
	if _, err := SpliceDisplayIDs([]int64{1, 2}, []int64{2, 2}); err == nil {
		t.Fatal("duplicate")
	}
	if _, err := SpliceDisplayIDs([]int64{1, 2}, []int64{9}); err == nil {
		t.Fatal("missing")
	}
	if _, err := SpliceDisplayIDs([]int64{1}, nil); err == nil {
		t.Fatal("empty")
	}
}

func TestReorderCardsKeepsListByID(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	a, err := CreateUser(d, "a", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := CreateUser(d, "b", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := CreateUser(d, "c", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	var sortA int64
	if err := d.QueryRow(`SELECT sort_order FROM users WHERE id=?`, a.ID).Scan(&sortA); err != nil {
		t.Fatal(err)
	}
	if sortA != a.ID {
		t.Fatalf("new user sort_order %d id %d", sortA, a.ID)
	}
	if err := ReorderCards(d, CardUsers, []int64{c.ID, a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	byID, err := ListUsers(d)
	if err != nil {
		t.Fatal(err)
	}
	if byID[0].ID != a.ID || byID[1].ID != b.ID || byID[2].ID != c.ID {
		t.Fatalf("id order changed %#v", []int64{byID[0].ID, byID[1].ID, byID[2].ID})
	}
	if err := ArrangeByDisplay(d, CardUsers, byID, func(u *User) int64 { return u.ID }); err != nil {
		t.Fatal(err)
	}
	if byID[0].ID != c.ID || byID[1].ID != a.ID || byID[2].ID != b.ID {
		t.Fatalf("display %#v", []int64{byID[0].ID, byID[1].ID, byID[2].ID})
	}
	if err := ReorderCards(d, CardUsers, []int64{b.ID, c.ID}); err != nil {
		t.Fatal(err)
	}
	ids, err := displayIDs(d, CardUsers)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] != b.ID || ids[1] != a.ID || ids[2] != c.ID {
		t.Fatalf("subset splice %#v", ids)
	}
}

func TestReorderInboundsStayOnOneServer(t *testing.T) {
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s1, err := CreateServer(d, "a", "a.example", tok)
	if err != nil {
		t.Fatal(err)
	}
	tok2, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := CreateServer(d, "b", "b.example", tok2)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(srv int64, name string, port int) int64 {
		t.Helper()
		in, err := CreateInbound(d, &Inbound{
			ServerID: srv, Name: name, Profile: "socks", Protocol: "socks",
			Network: "tcp", Security: "none", Core: "xray", Listen: "0.0.0.0",
			Port: port, Enabled: true, Settings: `{}`, LineKind: "direct",
		})
		if err != nil {
			t.Fatal(err)
		}
		return in.ID
	}
	a := mk(s1.ID, "a", 1001)
	b := mk(s1.ID, "b", 1002)
	c := mk(s2.ID, "c", 1003)
	if err := ReorderCards(d, CardInbounds, []int64{b, a}); err != nil {
		t.Fatal(err)
	}
	ids, err := displayIDs(d, CardInbounds)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[0] != b || ids[1] != a || ids[2] != c {
		t.Fatalf("order %#v", ids)
	}
	if err := ReorderCards(d, CardInbounds, []int64{a, c}); err == nil {
		t.Fatal("cross server")
	}
	ids, err = displayIDs(d, CardInbounds)
	if err != nil {
		t.Fatal(err)
	}
	if ids[0] != b || ids[1] != a || ids[2] != c {
		t.Fatalf("order changed after reject %#v", ids)
	}
}
