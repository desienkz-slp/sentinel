package directory

import (
	"context"
	"errors"
	"testing"
)

func sampleDir() *Directory {
	return New([]Member{
		{Number: "08118001001", Name: "Budi Super", Role: RoleSuperAdmin, Title: "Owner", Active: true, PINHash: HashPIN("1234")},
		{Number: "628118002002", Name: "Ani Admin", Role: RoleAdmin, Active: true},
		{Number: "+62 811-800-3003", Name: "Candra NOC", Role: RoleNOCSenior, Active: true, PINHash: HashPIN("9999")},
		{Number: "628118004004", Name: "Nonaktif", Role: RoleAdmin, Active: false},
		{Number: "", Name: "Invalid", Role: RoleAdmin, Active: true},      // diabaikan: nomor kosong
		{Number: "628118005005", Name: "BadRole", Role: "hacker", Active: true}, // diabaikan: role invalid
	})
}

func TestLookupNormalizesNumber(t *testing.T) {
	d := sampleDir()
	// 08118001001 -> 628118001001; cari dengan format berbeda harus ketemu.
	for _, q := range []string{"628118001001", "08118001001", "+628118001001", "628118001001@s.whatsapp.net"} {
		m, ok := d.Lookup(q)
		if !ok {
			t.Fatalf("Lookup(%q) tidak ketemu", q)
		}
		if m.Role != RoleSuperAdmin {
			t.Errorf("Lookup(%q) role=%s, mau super_admin", q, m.Role)
		}
	}
}

func TestLookupInactiveAndInvalidIgnored(t *testing.T) {
	d := sampleDir()
	if _, ok := d.Lookup("628118004004"); ok {
		t.Error("staf nonaktif tidak boleh ketemu")
	}
	if _, ok := d.Lookup("628118005005"); ok {
		t.Error("role invalid harus diabaikan")
	}
}

func TestPIN(t *testing.T) {
	d := sampleDir()
	m, _ := d.Lookup("628118001001")
	if !m.HasPIN() {
		t.Fatal("super admin harus punya PIN")
	}
	if !m.CheckPIN("1234") {
		t.Error("PIN 1234 harus cocok")
	}
	if m.CheckPIN("0000") {
		t.Error("PIN salah tidak boleh cocok")
	}
	if m.CheckPIN("") {
		t.Error("PIN kosong tidak boleh cocok")
	}
	// Staf tanpa PIN.
	ani, _ := d.Lookup("628118002002")
	if ani.HasPIN() || ani.CheckPIN("apapun") {
		t.Error("staf tanpa PIN tidak boleh lolos CheckPIN")
	}
}

func TestMembersNeverLeakPINHash(t *testing.T) {
	d := sampleDir()
	for _, m := range d.Members() {
		if m.PINHash != "" {
			t.Errorf("Members() membocorkan PINHash untuk %s", m.Name)
		}
	}
}

func TestPermsHierarchy(t *testing.T) {
	cust := PermsFor(RoleCustomer)
	if len(cust) != 1 || cust[0] != PermReadOwn {
		t.Errorf("customer perms salah: %v", cust)
	}
	has := func(r Role, p Permission) bool {
		for _, x := range PermsFor(r) {
			if x == p {
				return true
			}
		}
		return false
	}
	if !has(RoleSuperAdmin, PermDeleteCustomer) {
		t.Error("super admin harus bisa delete")
	}
	if has(RoleCustomer, PermDeleteCustomer) {
		t.Error("customer TIDAK boleh delete")
	}
	if !has(RoleNOCSenior, PermNetworkingWrite) {
		t.Error("NOC senior harus bisa networking_write")
	}
	if has(RoleAdmin, PermNetworkingWrite) {
		t.Error("admin TIDAK boleh networking_write")
	}
	if !has(RoleAdmin, PermEditCustomer) {
		t.Error("admin harus bisa edit customer")
	}
}

func TestRiskyPermsNeedPIN(t *testing.T) {
	if !NeedsPIN(PermDeleteCustomer) || !NeedsPIN(PermNetworkingWrite) || !NeedsPIN(PermEditCustomer) {
		t.Error("aksi berisiko harus butuh PIN")
	}
	if NeedsPIN(PermReadOwn) || NeedsPIN(PermNetworkingRead) || NeedsPIN(PermReport) {
		t.Error("aksi baca tidak boleh butuh PIN")
	}
}

// --- Identifier dengan mock billing ---

type mockBilling struct {
	cust map[string]CustomerInfo
	err  error
}

func (m mockBilling) LookupCustomerByPhone(ctx context.Context, phone string) (CustomerInfo, bool, error) {
	if m.err != nil {
		return CustomerInfo{}, false, m.err
	}
	c, ok := m.cust[phone]
	return c, ok, nil
}

func TestIdentifyStaffWins(t *testing.T) {
	d := sampleDir()
	// Nomor super admin JUGA terdaftar sebagai pelanggan: role harus tetap staf.
	bl := mockBilling{cust: map[string]CustomerInfo{
		"628118001001": {Name: "Budi (akun pelanggan)", Status: "active"},
	}}
	idf := NewIdentifier(d, bl)
	c := idf.Identify(context.Background(), "08118001001")
	if c.Role != RoleSuperAdmin {
		t.Fatalf("role=%s, mau super_admin", c.Role)
	}
	if !c.IsStaff {
		t.Error("harus ditandai staf")
	}
	if !c.IsCustomer || c.Customer == nil {
		t.Error("data pelanggan tetap dilampirkan untuk staf yang juga pelanggan")
	}
	if !c.Can(PermDeleteCustomer) {
		t.Error("super admin harus bisa delete")
	}
}

func TestIdentifyCustomer(t *testing.T) {
	d := sampleDir()
	bl := mockBilling{cust: map[string]CustomerInfo{
		"628129990001": {Name: "Siti Pelanggan", Status: "active", Package: "Home 20Mbps"},
	}}
	idf := NewIdentifier(d, bl)
	c := idf.Identify(context.Background(), "08129990001")
	if c.Role != RoleCustomer {
		t.Fatalf("role=%s, mau customer", c.Role)
	}
	if c.IsStaff {
		t.Error("pelanggan tidak boleh ditandai staf")
	}
	if !c.IsCustomer || c.Customer == nil || c.Customer.Name != "Siti Pelanggan" {
		t.Errorf("data pelanggan salah: %+v", c.Customer)
	}
	if c.Can(PermEditCustomer) || c.Can(PermDeleteCustomer) {
		t.Error("pelanggan TIDAK boleh edit/delete")
	}
	if !c.Can(PermReadOwn) {
		t.Error("pelanggan harus bisa baca data sendiri")
	}
}

func TestIdentifyUnknownDefaultsCustomer(t *testing.T) {
	d := sampleDir()
	bl := mockBilling{cust: map[string]CustomerInfo{}}
	idf := NewIdentifier(d, bl)
	c := idf.Identify(context.Background(), "628120000000")
	if c.Role != RoleCustomer {
		t.Errorf("tak dikenal harus default customer, dapat %s", c.Role)
	}
	if c.IsStaff || c.IsCustomer {
		t.Error("tak dikenal: bukan staf, bukan pelanggan")
	}
}

func TestIdentifyBillingErrorRecorded(t *testing.T) {
	d := sampleDir()
	bl := mockBilling{err: errors.New("billing timeout")}
	idf := NewIdentifier(d, bl)
	// Pelanggan tak dikenal + billing error: jangan crash, catat error.
	c := idf.Identify(context.Background(), "628120000000")
	if c.BillingError == "" {
		t.Error("error billing harus tercatat")
	}
	if c.IsCustomer {
		t.Error("jangan klaim pelanggan saat billing error")
	}
	// Staf tetap teridentifikasi walau billing error.
	cs := idf.Identify(context.Background(), "628118002002")
	if !cs.IsStaff || cs.Role != RoleAdmin {
		t.Error("staf harus tetap teridentifikasi walau billing error")
	}
}

func TestIdentifyNilBilling(t *testing.T) {
	d := sampleDir()
	idf := NewIdentifier(d, nil) // billing belum dikonfigurasi
	c := idf.Identify(context.Background(), "628118003003")
	if c.Role != RoleNOCSenior {
		t.Errorf("staf harus teridentifikasi tanpa billing, dapat %s", c.Role)
	}
}
