package corecfg

import (
	"encoding/json"
	"testing"

	"liking/internal/db"
)

func TestBuildXrayAndMita(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "v1", Profile: ProfileVLESSRealityVision,
		Port: 443, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, in); err != nil {
		t.Fatal(err)
	}
	m := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 8964, Enabled: true, LineKind: "direct", Settings: `{"transport":"TCP"}`,
	}
	if err := Normalize(m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, m); err != nil {
		t.Fatal(err)
	}

	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Xray) == 0 || len(b.Apply.Mita) == 0 {
		t.Fatalf("expected xray+mita, got xray=%d mita=%d singbox=%d", len(b.Apply.Xray), len(b.Apply.Mita), len(b.Apply.Singbox))
	}
	if b.Rev == "" {
		t.Fatal("rev")
	}
	var xray map[string]any
	if err := json.Unmarshal(b.Apply.Xray, &xray); err != nil {
		t.Fatal(err)
	}
	if _, ok := xray["inbounds"]; !ok {
		t.Fatal("xray inbounds")
	}
	routing, _ := xray["routing"].(map[string]any)
	rules, _ := routing["rules"].([]any)
	apiRule := ""
	for _, r := range rules {
		m, _ := r.(map[string]any)
		tags, _ := m["inboundTag"].([]any)
		for _, t := range tags {
			if t == "api" {
				apiRule, _ = m["outboundTag"].(string)
			}
		}
	}
	if apiRule != "api" {
		t.Fatalf("api inbound must route to api module, got %q", apiRule)
	}
	for _, o := range xray["outbounds"].([]any) {
		if o.(map[string]any)["tag"] == "api-out" {
			t.Fatal("freedom api-out loops the stats API onto itself")
		}
	}
}

func TestProvisionKeepsMieruPasswordWhenDisabled(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 54675, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	created, err := db.CreateInbound(d, in)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := db.CreatePackage(d, "std", 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetPackageServers(d, pkg.ID, []int64{srv.ID}); err != nil {
		t.Fatal(err)
	}
	u, err := db.CreateUser(d, "alice", "h", "user", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.BindUserPackage(d, u.ID, pkg.ID, 0); err != nil {
		t.Fatal(err)
	}
	u, _ = db.GetUser(d, u.ID)
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err := db.GetClient(d, created.ID, u.ID)
	if err != nil || c.Password == "" || !c.Enabled {
		t.Fatalf("client %+v %v", c, err)
	}
	pw := c.Password
	user := c.Username
	created.Enabled = false
	if err := db.UpdateInbound(d, created); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err = db.GetClient(d, created.ID, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.Password != pw || c.Username != user {
		t.Fatalf("password rotated on disable: %s/%s -> %s/%s", user, pw, c.Username, c.Password)
	}
	if c.Enabled {
		t.Fatal("disabled inbound should disable client")
	}
	created.Enabled = true
	if err := db.UpdateInbound(d, created); err != nil {
		t.Fatal(err)
	}
	if _, err := ProvisionUser(d, u); err != nil {
		t.Fatal(err)
	}
	c, err = db.GetClient(d, created.ID, u.ID)
	if err != nil || !c.Enabled || c.Password != pw || c.Username != user {
		t.Fatalf("after enable %+v %v", c, err)
	}
}

func TestBuildIncludesMitaWhenCoreNotReported(t *testing.T) {
	d, err := db.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, err := db.CreateServer(d, "n1", "10.0.0.1", "tok")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.MarkServerOnline(d, srv.ID, "0.1.6", "linux", "amd64", "10.0.0.1", []string{"xray"}); err != nil {
		t.Fatal(err)
	}
	in := &db.Inbound{
		ServerID: srv.ID, Name: "v1", Profile: ProfileVLESSRealityVision,
		Port: 8443, Enabled: true, LineKind: "direct", Settings: "{}",
	}
	if err := Normalize(in, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, in); err != nil {
		t.Fatal(err)
	}
	m := &db.Inbound{
		ServerID: srv.ID, Name: "m1", Profile: ProfileMieru,
		Port: 8964, Enabled: true, LineKind: "direct", Settings: `{"transport":"TCP"}`,
	}
	if err := Normalize(m, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateInbound(d, m); err != nil {
		t.Fatal(err)
	}
	b, err := Build(d, srv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Apply.Xray) == 0 {
		t.Fatal("xray")
	}
	if len(b.Apply.Mita) == 0 {
		t.Fatal("mita config must be sent so the agent can install the core")
	}
}
