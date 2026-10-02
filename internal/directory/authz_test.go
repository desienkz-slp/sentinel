package directory

import (
	"context"
	"testing"
)

func superAdmin() Caller {
	return Caller{Number: "628111", Role: RoleSuperAdmin, IsStaff: true, Perms: PermsFor(RoleSuperAdmin)}
}
func noc() Caller {
	return Caller{Number: "628222", Role: RoleNOCSenior, IsStaff: true, Perms: PermsFor(RoleNOCSenior)}
}
func adminC() Caller {
	return Caller{Number: "628333", Role: RoleAdmin, IsStaff: true, Perms: PermsFor(RoleAdmin)}
}
func customerC() Caller {
	return Caller{Number: "628444", Role: RoleCustomer, Perms: PermsFor(RoleCustomer)}
}

func TestPermForTool(t *testing.T) {
	if PermForTool("mikrotik.disconnect_pppoe") != PermNetworkingWrite {
		t.Error("disconnect_pppoe harus butuh networking_write")
	}
	if PermForTool("mikrotik.get_pppoe_status") != PermNetworkingRead {
		t.Error("get_pppoe_status harus butuh networking_read")
	}
	if PermForTool("billing.get_customer") != PermReadAll {
		t.Error("billing.get_customer harus butuh read_all")
	}
	// Tool tak dikenal -> paling ketat.
	if PermForTool("tool.aneh") != PermManageStaff {
		t.Error("tool tak dikenal harus default manage_staff (paling ketat)")
	}
}

func TestAuthorizeReadNoPIN(t *testing.T) {
	// NOC baca status jaringan: langsung Allow, tanpa PIN.
	r := Authorize(noc(), "mikrotik.get_pppoe_status", false)
	if r.Decision != Allow {
		t.Errorf("NOC baca status harus Allow, dapat %s (%s)", r.Decision, r.Reason)
	}
}

func TestAuthorizeWriteNeedsPIN(t *testing.T) {
	// NOC disconnect PPPoE: izin cukup TAPI belum PIN -> NeedPIN.
	r := Authorize(noc(), "mikrotik.disconnect_pppoe", false)
	if r.Decision != NeedPIN {
		t.Errorf("WRITE tanpa PIN harus NeedPIN, dapat %s", r.Decision)
	}
	// Setelah PIN -> Allow.
	r2 := Authorize(noc(), "mikrotik.disconnect_pppoe", true)
	if r2.Decision != Allow {
		t.Errorf("WRITE dgn PIN harus Allow, dapat %s", r2.Decision)
	}
}

func TestAuthorizeAdminCannotNetworkWrite(t *testing.T) {
	// Admin tidak punya networking_write -> Deny (walau PIN diverifikasi).
	r := Authorize(adminC(), "mikrotik.disconnect_pppoe", true)
	if r.Decision != Deny {
		t.Errorf("admin tidak boleh networking_write, dapat %s", r.Decision)
	}
}

func TestAuthorizeCustomerDenied(t *testing.T) {
	// Pelanggan tidak boleh baca status jaringan pelanggan lain, apalagi WRITE.
	if Authorize(customerC(), "mikrotik.get_pppoe_status", false).Decision != Deny {
		t.Error("pelanggan tidak boleh networking_read")
	}
	if Authorize(customerC(), "mikrotik.disconnect_pppoe", true).Decision != Deny {
		t.Error("pelanggan tidak boleh networking_write")
	}
	if Authorize(customerC(), "billing.get_customer", false).Decision != Deny {
		t.Error("pelanggan tidak boleh baca data pelanggan lain (read_all)")
	}
}

func TestAuthorizeSuperAdminAll(t *testing.T) {
	// Super admin: WRITE dgn PIN -> Allow; tool tak dikenal (manage_staff) dgn PIN -> Allow.
	if Authorize(superAdmin(), "mikrotik.disconnect_pppoe", true).Decision != Allow {
		t.Error("super admin + PIN harus bisa disconnect")
	}
	if Authorize(superAdmin(), "tool.aneh", true).Decision != Allow {
		t.Error("super admin + PIN harus bisa tool manage_staff-level")
	}
	// Tanpa PIN: tetap NeedPIN (bukan langsung Allow).
	if Authorize(superAdmin(), "mikrotik.disconnect_pppoe", false).Decision != NeedPIN {
		t.Error("super admin tanpa PIN tetap NeedPIN untuk WRITE")
	}
}

// Pastikan Authorize bebas dari dependensi context (murni) — kompilasi saja cukup,
// tapi tegaskan tidak panic dengan caller kosong.
func TestAuthorizeEmptyCaller(t *testing.T) {
	_ = context.Background()
	r := Authorize(Caller{}, "mikrotik.disconnect_pppoe", true)
	if r.Decision != Deny {
		t.Error("caller kosong harus Deny")
	}
}
