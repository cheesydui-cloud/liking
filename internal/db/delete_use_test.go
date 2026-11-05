package db

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

func TestDeleteUseCustomersBlockPackage(t *testing.T) {
	d := useDB(t)
	srv := useServer(t, d, "入口")
	node := useNode(t, d, srv.ID, "节点A", 10001, "vless", "{}")
	pkg := usePackageInbounds(t, d, "月付", node.ID)

	disabled := useCustomer(t, d, "停用的", pkg, 0)
	disabled.Enabled = false
	if err := UpdateUser(d, disabled); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`UPDATE users SET traffic_limit=100, used_up=100 WHERE id=?`, disabled.ID); err != nil {
		t.Fatal(err)
	}
	useCustomer(t, d, "过期的", pkg, 1)
	adminPkg := usePackageInbounds(t, d, "管理员套餐", node.ID)
	useCustomerRole(t, d, "管理员", "admin", adminPkg, 0)

	mustUse(t, PackageInUse(d, pkg.ID), "2", "月付")
	mustFree(t, PackageInUse(d, adminPkg.ID))
	mustFree(t, PackageInUse(d, 99999))

	var still int
	if err := d.QueryRow(`SELECT COUNT(*) FROM user_packages WHERE package_id=?`, pkg.ID).Scan(&still); err != nil {
		t.Fatal(err)
	}
	if still != 2 {
		t.Fatalf("guard changed bindings: %d", still)
	}
}

func TestDeleteUseAdminDoesNotBlockAndUnbindFrees(t *testing.T) {
	d := useDB(t)
	srv := useServer(t, d, "入口")
	node := useNode(t, d, srv.ID, "节点A", 10001, "vless", "{}")
	pkg := usePackageInbounds(t, d, "月付", node.ID)
	admin := useCustomerRole(t, d, "管理员", "admin", pkg, 0)
	alice := useCustomer(t, d, "alice", pkg, 0)

	mustUse(t, PackageInUse(d, pkg.ID), "1", "月付")
	if err := UnbindUserPackage(d, alice.ID); err != nil {
		t.Fatal(err)
	}
	mustFree(t, PackageInUse(d, pkg.ID))
	mustFree(t, InboundInUse(d, node.ID))
	mustFree(t, ServerInUse(d, srv.ID))
	_ = admin
}

func TestDeleteUseExplicitInboundLeavesSibling(t *testing.T) {
	d := useDB(t)
	srv := useServer(t, d, "入口")
	kept := useNode(t, d, srv.ID, "在用", 10001, "vless", "{}")
	free := useNode(t, d, srv.ID, "空闲", 10002, "vless", "{}")
	pkg := usePackageInbounds(t, d, "月付", kept.ID)
	useCustomer(t, d, "alice", pkg, 0)
	unused := usePackageInbounds(t, d, "空套餐", free.ID)

	mustUse(t, InboundInUse(d, kept.ID), "在用", "月付", "1")
	mustFree(t, InboundInUse(d, free.ID))
	mustFree(t, PackageInUse(d, unused.ID))
	mustUse(t, ServerInUse(d, srv.ID), "入口", "在用", "月付")
	mustFree(t, InboundInUse(d, 99999))
}

func TestDeleteUseBusiestPackageWins(t *testing.T) {
	d := useDB(t)
	srv := useServer(t, d, "入口")
	node := useNode(t, d, srv.ID, "节点A", 10001, "vless", "{}")
	small := usePackageInbounds(t, d, "小套餐", node.ID)
	big := usePackageInbounds(t, d, "大套餐", node.ID)
	useCustomer(t, d, "one", small, 0)
	useCustomer(t, d, "two", big, 0)
	useCustomer(t, d, "three", big, 0)
	useCustomer(t, d, "four", big, 0)

	mustUse(t, InboundInUse(d, node.ID), "大套餐", "3")
}

func TestDeleteUseWholeServerBindCoversLaterNodes(t *testing.T) {
	d := useDB(t)
	srv := useServer(t, d, "空实例")
	other := useServer(t, d, "别的")
	pkg := usePackageServers(t, d, "整机", srv.ID)
	useCustomer(t, d, "alice", pkg, 0)

	mustUse(t, ServerInUse(d, srv.ID), "绑在套餐", "整机", "1")
	mustFree(t, ServerInUse(d, other.ID))

	node := useNode(t, d, srv.ID, "后加的", 10001, "vless", "{}")
	siblingHost := useNode(t, d, other.ID, "别人的", 10002, "vless", "{}")
	mustUse(t, InboundInUse(d, node.ID), "后加的", "整机")
	mustFree(t, InboundInUse(d, siblingHost.ID))
	mustUse(t, ServerInUse(d, srv.ID), "上的节点", "后加的")
}

func TestDeleteUseWireGuardExit(t *testing.T) {
	d := useDB(t)
	entry := useServer(t, d, "入口")
	home := useServer(t, d, "家宽")
	other := useServer(t, d, "无关")
	settings := fmt.Sprintf(`{"exit_server_id":%d}`, home.ID)
	node := useNode(t, d, entry.ID, "扎鸡", 10001, "wireguard", settings)
	decoy := useNode(t, d, entry.ID, "伪装", 10002, "vless", settings)
	pkg := usePackageInbounds(t, d, "月付", node.ID, decoy.ID)
	useCustomer(t, d, "alice", pkg, 0)

	err := ServerInUse(d, home.ID)
	mustUse(t, err, "家宽出口", "扎鸡")
	if strings.Contains(err.Error(), "伪装") {
		t.Fatalf("non-wireguard exit_server_id blocked home: %v", err)
	}
	mustFree(t, ServerInUse(d, other.ID))
	mustUse(t, ServerInUse(d, entry.ID), "上的节点", "月付")
}

func TestDeleteUseChainLandingAndHop(t *testing.T) {
	d := useDB(t)
	entrySrv := useServer(t, d, "入口")
	hopSrv := useServer(t, d, "中转机器")
	landSrv := useServer(t, d, "落地机器")
	other := useServer(t, d, "无关")
	hop := useNode(t, d, hopSrv.ID, "跳点", 10001, "vless", "{}")
	land := useNode(t, d, landSrv.ID, "落地", 10002, "vless", "{}")
	settings := fmt.Sprintf(`{"hops":[{"kind":"panel","inbound_id":%d}]}`, hop.ID)
	entry := useChain(t, d, entrySrv.ID, "入口节点", 10003, settings, land.ID)
	pkg := usePackageInbounds(t, d, "月付", entry.ID)
	useCustomer(t, d, "alice", pkg, 0)

	mustUse(t, InboundInUse(d, entry.ID), "入口节点", "月付")
	mustFree(t, InboundInUse(d, hop.ID))
	mustFree(t, InboundInUse(d, land.ID))
	mustUse(t, ServerInUse(d, hopSrv.ID), "中转", "跳点", "入口节点")
	mustUse(t, ServerInUse(d, landSrv.ID), "中转", "落地", "入口节点")
	mustUse(t, ServerInUse(d, entrySrv.ID), "上的节点", "入口节点")
	mustFree(t, ServerInUse(d, other.ID))
	mustFree(t, ServerInUse(d, 99999))
}

func useDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func useServer(t *testing.T, d *sql.DB, name string) *Server {
	t.Helper()
	tok, err := RandomHex(8)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := CreateServer(d, name, name+".example", tok)
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func useNode(t *testing.T, d *sql.DB, serverID int64, name string, port int, profile, settings string) *Inbound {
	t.Helper()
	in, err := CreateInbound(d, &Inbound{
		ServerID: serverID, Name: name, Profile: profile,
		Protocol: "vless", Network: "tcp", Security: "none", Core: "xray",
		Listen: "0.0.0.0", Port: port, Enabled: true, Settings: settings, LineKind: "direct",
	})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func useChain(t *testing.T, d *sql.DB, serverID int64, name string, port int, settings string, exitID int64) *Inbound {
	t.Helper()
	in, err := CreateInbound(d, &Inbound{
		ServerID: serverID, Name: name, Profile: "vless",
		Protocol: "vless", Network: "tcp", Security: "none", Core: "xray",
		Listen: "0.0.0.0", Port: port, Enabled: true, Settings: settings,
		LineKind: "chain", ExitInboundID: &exitID,
	})
	if err != nil {
		t.Fatal(err)
	}
	return in
}

func usePackageInbounds(t *testing.T, d *sql.DB, name string, inboundIDs ...int64) *Package {
	t.Helper()
	p, err := CreatePackage(d, name, 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPackageInbounds(d, p.ID, inboundIDs, nil); err != nil {
		t.Fatal(err)
	}
	return p
}

func usePackageServers(t *testing.T, d *sql.DB, name string, serverIDs ...int64) *Package {
	t.Helper()
	p, err := CreatePackage(d, name, 0, 30, 0, "oneway")
	if err != nil {
		t.Fatal(err)
	}
	if err := SetPackageServers(d, p.ID, serverIDs); err != nil {
		t.Fatal(err)
	}
	return p
}

func useCustomer(t *testing.T, d *sql.DB, name string, pkg *Package, expiresAt int64) *User {
	t.Helper()
	return useCustomerRole(t, d, name, "user", pkg, expiresAt)
}

func useCustomerRole(t *testing.T, d *sql.DB, name, role string, pkg *Package, expiresAt int64) *User {
	t.Helper()
	u, err := CreateUserConfigured(d, name, "hash", "", role, "", expiresAt, pkg, 0)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustUse(t *testing.T, err error, parts ...string) {
	t.Helper()
	if !IsInUse(err) {
		t.Fatalf("want in-use error, got %v", err)
	}
	msg := err.Error()
	for _, p := range parts {
		if !strings.Contains(msg, p) {
			t.Fatalf("message %q missing %q", msg, p)
		}
	}
}

func mustFree(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
