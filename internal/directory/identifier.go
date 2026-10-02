package directory

import (
	"context"
	"strings"

	"ainoc/internal/identity"
)

// CustomerInfo adalah ringkasan pelanggan hasil lookup billing (dipisah dari
// paket billing agar directory tidak bergantung pada paket billing — hindari
// import cycle & jaga directory tetap murni).
type CustomerInfo struct {
	Name       string
	Username   string
	Phone      string
	Status     string
	IsIsolated bool
	Area       string
	Package    string
}

// BillingLookup adalah kontrak minimal yang dibutuhkan untuk mencari pelanggan
// berdasarkan nomor. Diimplementasikan oleh adapter billing (via shim di server).
type BillingLookup interface {
	LookupCustomerByPhone(ctx context.Context, phone string) (CustomerInfo, bool, error)
}

// Caller adalah hasil identifikasi satu penelepon WA.
type Caller struct {
	Number     string        `json:"number"`      // nomor ternormalisasi
	Role       Role          `json:"role"`        // role efektif
	Name       string        `json:"name"`        // nama (staf atau pelanggan)
	Title      string        `json:"title"`       // jabatan manusiawi (staf)
	IsStaff    bool          `json:"is_staff"`    // true bila dari direktori staf
	IsCustomer bool          `json:"is_customer"` // true bila cocok di billing
	Customer   *CustomerInfo `json:"customer,omitempty"`
	Perms      []Permission  `json:"perms"` // izin efektif
	// BillingError terisi bila lookup billing gagal (jangan simpulkan "bukan
	// pelanggan" — master spec §52). Identifikasi staf tetap valid.
	BillingError string `json:"billing_error,omitempty"`
}

// Can melaporkan apakah penelepon memiliki izin tertentu.
func (c Caller) Can(p Permission) bool {
	for _, x := range c.Perms {
		if x == p {
			return true
		}
	}
	return false
}

// NeedsPIN melaporkan apakah izin ini tergolong berisiko (wajib verifikasi PIN).
func NeedsPIN(p Permission) bool { return RiskyPerms[p] }

// Identifier mengidentifikasi penelepon: cek direktori staf dulu (nomor internal),
// lalu billing (pelanggan). Tak dikenal -> RoleCustomer (alur CS), BUKAN akses
// internal — deny-by-default.
type Identifier struct {
	dir     *Directory
	billing BillingLookup // boleh nil (billing belum dikonfigurasi)
}

// NewIdentifier membuat Identifier. billing boleh nil.
func NewIdentifier(dir *Directory, billing BillingLookup) *Identifier {
	return &Identifier{dir: dir, billing: billing}
}

// Identify menentukan siapa penelepon dari nomornya.
//
// Urutan (master spec §6, §9):
//  1. Direktori staf internal (admin/NOC/super admin) — prioritas tertinggi.
//  2. Billing (pelanggan) — bila bukan staf.
//  3. Tak dikenal -> RoleCustomer tanpa data (tetap dilayani sebagai CS, tapi
//     tanpa hak internal).
//
// Catatan keamanan: staf dicek lebih dulu agar nomor yang kebetulan juga
// terdaftar sebagai pelanggan tetap mendapat hak staf-nya.
func (idf *Identifier) Identify(ctx context.Context, number string) Caller {
	num := identity.Normalize(number)
	c := Caller{Number: num, Role: RoleCustomer}

	// 1. Staf internal.
	if m, ok := idf.dir.Lookup(num); ok {
		c.Role = m.Role
		c.Name = m.Name
		c.Title = m.Title
		c.IsStaff = true
		c.Perms = PermsFor(m.Role)
		// Staf juga bisa jadi pelanggan; lampirkan data pelanggan bila ada,
		// tapi role TETAP role staf.
		idf.attachCustomer(ctx, num, &c)
		return c
	}

	// 2. Pelanggan via billing.
	idf.attachCustomer(ctx, num, &c)
	// Role tetap customer (default); izin customer.
	c.Perms = PermsFor(RoleCustomer)
	return c
}

// attachCustomer mencoba melampirkan data pelanggan dari billing. Kegagalan API
// dicatat di BillingError, tidak menggagalkan identifikasi.
func (idf *Identifier) attachCustomer(ctx context.Context, num string, c *Caller) {
	if idf.billing == nil || num == "" {
		return
	}
	info, ok, err := idf.billing.LookupCustomerByPhone(ctx, num)
	if err != nil {
		c.BillingError = err.Error()
		return
	}
	if ok {
		c.IsCustomer = true
		ci := info
		c.Customer = &ci
		if strings.TrimSpace(c.Name) == "" {
			c.Name = info.Name
		}
	}
}
