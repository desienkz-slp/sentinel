// Package directory mengimplementasikan Staff Directory + model Role/Permission
// (master spec §22, §38) untuk identifikasi penelepon dan kontrol akses berbasis
// identitas (RBAC).
//
// Prinsip:
//   - Identitas = nomor WA ternormalisasi (lihat internal/identity).
//   - Penelepon tak dikenal -> RoleCustomer secara default (alur CS), BUKAN akses
//     internal. Deny-by-default tetap berlaku: tidak ada nomor = tidak ada hak staf.
//   - Hak WRITE berisiko butuh verifikasi PIN (master spec §24, §37) — PIN disimpan
//     sebagai hash, tidak pernah plaintext.
//   - Role lebih tinggi mewarisi izin role di bawahnya.
package directory

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"sort"
	"strings"

	"ainoc/internal/identity"
)

// Role adalah jabatan fungsional penelepon.
type Role string

const (
	RoleUnknown    Role = "unknown"     // belum teridentifikasi (jarang; default jatuh ke customer)
	RoleCustomer   Role = "customer"    // pelanggan: alur CS, read-only data sendiri
	RoleAdmin      Role = "admin"       // non-networking: kelola data pelanggan/billing
	RoleNOCSenior  Role = "noc_senior"  // networking: lihat + eksekusi tindakan jaringan
	RoleSuperAdmin Role = "super_admin" // bos: semua lintas sistem (delete/edit/report)
)

// rank menentukan hierarki (angka lebih besar = wewenang lebih tinggi).
func (r Role) rank() int {
	switch r {
	case RoleSuperAdmin:
		return 40
	case RoleNOCSenior:
		return 30
	case RoleAdmin:
		return 20
	case RoleCustomer:
		return 10
	default:
		return 0
	}
}

// Valid melaporkan apakah role termasuk yang dikenal.
func (r Role) Valid() bool {
	switch r {
	case RoleCustomer, RoleAdmin, RoleNOCSenior, RoleSuperAdmin:
		return true
	default:
		return false
	}
}

// Label mengembalikan nama role yang ramah untuk ditampilkan.
func (r Role) Label() string {
	switch r {
	case RoleCustomer:
		return "Pelanggan"
	case RoleAdmin:
		return "Admin"
	case RoleNOCSenior:
		return "NOC Senior"
	case RoleSuperAdmin:
		return "Super Admin"
	default:
		return "Tidak dikenal"
	}
}

// Permission adalah kapabilitas atomik.
type Permission string

const (
	PermReadOwn        Permission = "read_own"        // lihat data milik sendiri
	PermReadAll        Permission = "read_all"        // lihat data pelanggan mana pun
	PermReport         Permission = "report"          // minta laporan/statistik
	PermNetworkingRead Permission = "networking_read" // baca status jaringan (radius/mikrotik/genieacs)
	PermNetworkingWrite Permission = "networking_write" // eksekusi tindakan jaringan (reconnect/disconnect)
	PermEditCustomer   Permission = "edit_customer"   // ubah data pelanggan/billing
	PermDeleteCustomer Permission = "delete_customer" // hapus data pelanggan
	PermManageStaff    Permission = "manage_staff"    // kelola direktori staf
)

// RiskyPerms = izin yang WAJIB verifikasi PIN sebelum dieksekusi (master spec §24).
var RiskyPerms = map[Permission]bool{
	PermNetworkingWrite: true,
	PermEditCustomer:    true,
	PermDeleteCustomer:  true,
	PermManageStaff:     true,
}

// defaultPerms memetakan role -> izin bawaan (dapat ditimpa per-staf nanti).
// Role lebih tinggi mewarisi izin role di bawahnya.
var defaultPerms = map[Role][]Permission{
	RoleCustomer:  {PermReadOwn},
	RoleAdmin:     {PermReadOwn, PermReadAll, PermReport, PermNetworkingRead, PermEditCustomer},
	RoleNOCSenior: {PermReadOwn, PermReadAll, PermReport, PermNetworkingRead, PermNetworkingWrite},
	RoleSuperAdmin: {
		PermReadOwn, PermReadAll, PermReport, PermNetworkingRead, PermNetworkingWrite,
		PermEditCustomer, PermDeleteCustomer, PermManageStaff,
	},
}

// PermsFor mengembalikan daftar izin (terurut) untuk sebuah role.
func PermsFor(r Role) []Permission {
	ps := append([]Permission(nil), defaultPerms[r]...)
	sort.Slice(ps, func(i, j int) bool { return ps[i] < ps[j] })
	return ps
}

// Member adalah satu entri staf internal.
type Member struct {
	Number  string `json:"number"`   // nomor WA (akan dinormalisasi)
	Name    string `json:"name"`     // nama tampil
	Role    Role   `json:"role"`     // jabatan
	Title   string `json:"title"`    // jabatan manusiawi (opsional, mis. "Manager NOC")
	Active  bool   `json:"active"`   // nonaktif = diabaikan saat lookup
	PINHash string `json:"pin_hash"` // sha256(pin) hex; kosong = belum set PIN
	// PINSet hanya diisi di Members() untuk UI (apakah PIN ada) tanpa bocorkan hash.
	PINSet bool `json:"pin_set"`
}

// HasPIN melaporkan apakah staf punya PIN terpasang.
func (m Member) HasPIN() bool { return strings.TrimSpace(m.PINHash) != "" }

// CheckPIN membandingkan PIN plaintext dengan hash tersimpan (konstan-waktu).
func (m Member) CheckPIN(pin string) bool {
	pin = strings.TrimSpace(pin)
	if pin == "" || m.PINHash == "" {
		return false
	}
	want, err := hex.DecodeString(m.PINHash)
	if err != nil {
		return false
	}
	got := sha256.Sum256([]byte(pin))
	return subtle.ConstantTimeCompare(got[:], want) == 1
}

// HashPIN mengubah PIN plaintext menjadi hash hex untuk disimpan.
func HashPIN(pin string) string {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return ""
	}
	h := sha256.Sum256([]byte(pin))
	return hex.EncodeToString(h[:])
}

// Directory adalah kumpulan staf internal yang dapat dicari berdasarkan nomor.
type Directory struct {
	byNumber map[string]Member
}

// New membangun Directory dari daftar staf. Nomor dinormalisasi; entri dengan
// nomor kosong atau role tidak valid diabaikan. Bila satu nomor muncul dua kali,
// entri dengan role tertinggi yang dipakai (hindari eskalasi hak tak sengaja ke
// bawah).
func New(members []Member) *Directory {
	d := &Directory{byNumber: make(map[string]Member, len(members))}
	for _, m := range members {
		num := identity.Normalize(m.Number)
		if num == "" || !m.Role.Valid() {
			continue
		}
		m.Number = num
		if ex, ok := d.byNumber[num]; ok && ex.Role.rank() >= m.Role.rank() {
			continue
		}
		d.byNumber[num] = m
	}
	return d
}

// Lookup mencari staf berdasarkan nomor (dinormalisasi dulu). Mengembalikan
// (Member, true) bila ditemukan & aktif. CATATAN: Member yang dikembalikan
// menyertakan PINHash (dipakai untuk CheckPIN). Jangan kirim langsung ke UI —
// pakai Members() yang mengosongkan hash.
func (d *Directory) Lookup(number string) (Member, bool) {
	if d == nil {
		return Member{}, false
	}
	num := identity.Normalize(number)
	if num == "" {
		return Member{}, false
	}
	m, ok := d.byNumber[num]
	if !ok || !m.Active {
		return Member{}, false
	}
	return m, true
}

// Members mengembalikan semua staf (terurut per role tertinggi lalu nama) untuk
// dashboard. PINHash SELALU dikosongkan — hash tidak pernah dikirim ke UI.
func (d *Directory) Members() []Member {
	if d == nil {
		return nil
	}
	out := make([]Member, 0, len(d.byNumber))
	for _, m := range d.byNumber {
		m.PINSet = m.PINHash != "" // tandai keberadaan PIN untuk UI
		m.PINHash = ""             // jangan bocorkan hash
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Role.rank() != out[j].Role.rank() {
			return out[i].Role.rank() > out[j].Role.rank()
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// LookupWithPIN sama dengan Lookup; nama eksplisit untuk titik pakai yang
// memang butuh PINHash (verifikasi PIN). Mengembalikan Member lengkap.
func (d *Directory) LookupWithPIN(number string) (Member, bool) { return d.Lookup(number) }

// ForRole mengembalikan staf AKTIF dengan role tertentu (untuk eskalasi: pilih
// NOC/Admin yang tersedia). Terurut by nama.
func (d *Directory) ForRole(r Role) []Member {
	if d == nil {
		return nil
	}
	var out []Member
	for _, m := range d.byNumber {
		if m.Active && m.Role == r {
			m.PINHash = ""
			out = append(out, m)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
