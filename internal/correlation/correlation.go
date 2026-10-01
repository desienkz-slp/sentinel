// Package correlation menormalkan dan mengorelasikan bukti lintas sistem.
//
// Sesuai blueprint §21 (Diagnostic Correlation) dan §51 (Tool Result
// Normalization): sistem yang berbeda menyebut keadaan yang sama dengan istilah
// berbeda. Package ini menyatukan mereka ke status kanonik, lalu menyimpulkan
// area investigasi utama dari bukti gabungan — tanpa menebak data yang tidak
// diketahui (UNKNOWN tetap UNKNOWN, blueprint §47).
package correlation

import (
	"fmt"
	"strings"
)

// State adalah status kanonik sebuah domain (setelah normalisasi).
type State string

const (
	StateActive    State = "ACCOUNT_ACTIVE" // billing: aktif
	StateInactive  State = "ACCOUNT_INACTIVE"
	StateAuthed    State = "AUTHENTICATED" // radius: autentikasi ok
	StateUnauthed  State = "UNAUTHENTICATED"
	StatePPPoEUp   State = "PPPOE_ACTIVE" // mikrotik: sesi aktif
	StatePPPoEDown State = "PPPOE_OFFLINE"
	StateDeviceOn  State = "DEVICE_ONLINE" // genieacs: ONT/CPE online
	StateDeviceOff State = "DEVICE_OFFLINE"
	StateUnknown   State = "UNKNOWN"
)

// Evidence adalah bukti ternormalisasi dari satu domain.
type Evidence struct {
	Domain string `json:"domain"` // billing | radius | mikrotik | genieacs
	State  State  `json:"state"`
	Raw    string `json:"raw,omitempty"`
}

// Normalize mengubah nilai mentah dari satu domain menjadi status kanonik.
// Nilai yang tidak dikenali -> UNKNOWN (jangan pernah menebak).
func Normalize(domain, raw string) State {
	v := strings.ToUpper(strings.TrimSpace(raw))
	switch domain {
	case "billing":
		switch {
		case contains(v, "ACTIVE", "AKTIF", "BERLANGGANAN"):
			return StateActive
		case contains(v, "INACTIVE", "NONAKTIF", "PUTUS", "SUSPEND", "BLOKIR", "EXPIRED"):
			return StateInactive
		}
	case "radius":
		switch {
		case contains(v, "OK", "ACCEPT", "AUTHORIZED", "AUTH_OK", "AKTIF", "ACTIVE"):
			return StateAuthed
		case contains(v, "REJECT", "FAIL", "UNAUTH", "GAGAL", "TOLAK"):
			return StateUnauthed
		}
	case "mikrotik":
		switch {
		case contains(v, "RUNNING", "ACTIVE", "AKTIF", "UP"):
			return StatePPPoEUp
		case contains(v, "DISCONNECT", "OFFLINE", "DOWN", "PUTUS", "MATI"):
			return StatePPPoEDown
		}
	case "genieacs":
		switch {
		case contains(v, "ONLINE", "UP", "READY", "AKTIF"):
			return StateDeviceOn
		case contains(v, "OFFLINE", "DOWN", "LOS", "MATI", "UNREACHABLE"):
			return StateDeviceOff
		}
	}
	return StateUnknown
}

// Set adalah kumpulan bukti ternormalisasi per domain.
type Set struct {
	items map[string]Evidence
}

// NewSet membuat set kosong.
func NewSet() *Set { return &Set{items: map[string]Evidence{}} }

// Add menambah/menimpa bukti satu domain.
func (s *Set) Add(domain, raw string) {
	s.items[domain] = Evidence{Domain: domain, State: Normalize(domain, raw), Raw: raw}
}

// Get mengembalikan bukti satu domain.
func (s *Set) Get(domain string) (Evidence, bool) {
	e, ok := s.items[domain]
	return e, ok
}

// Domains mengembalikan domain yang sudah punya bukti.
func (s *Set) Domains() []string {
	out := make([]string, 0, len(s.items))
	for d := range s.items {
		out = append(out, d)
	}
	return out
}

// Unknown melaporkan domain yang TIDAK punya bukti (harus dicatat sebagai
// UNKNOWN, bukan diisi tebakan).
func (s *Set) Missing(required ...string) []string {
	var out []string
	for _, d := range required {
		if _, ok := s.items[d]; !ok {
			out = append(out, d)
		}
	}
	return out
}

// Conclusion adalah hasil korelasi.
type Conclusion struct {
	Diagnosis    string   `json:"diagnosis"`
	Confidence   float64  `json:"confidence"` // 0..1
	PrimaryArea  string   `json:"primary_area"`
	Missing      []string `json:"missing"`
	Alternatives []string `json:"alternatives,omitempty"`
	Complete     bool     `json:"complete"` // semua domain wajib terisi
}

// Correlate menyimpulkan area investigasi dari bukti gabungan.
//
// Aturan diagnosis (dari skill CUSTOMER_INTERNET_DOWN):
//   - billing inactive -> masalah akun/tagihan
//   - billing active + device offline -> CPE/ONT
//   - billing active + radius unauthed -> autentikasi
//   - billing active + radius authed + device online + pppoe down -> sesi PPPoE
//   - ada domain UNKNOWN -> keyakinan rendah + daftar missing
func (s *Set) Correlate(required ...string) Conclusion {
	bill, hasBill := s.items["billing"]
	rad, hasRad := s.items["radius"]
	mtk, hasMtk := s.items["mikrotik"]
	acs, hasAcs := s.items["genieacs"]

	missing := s.Missing(required...)
	concl := Conclusion{Missing: missing, Complete: len(missing) == 0}

	// Ada bukti yang UNKNOWN -> turunkan keyakinan, tandai alternatif.
	hasUnknown := false
	for _, e := range s.items {
		if e.State == StateUnknown {
			hasUnknown = true
		}
	}

	switch {
	case hasBill && bill.State == StateInactive:
		concl.Diagnosis = "akun/tagihan tidak aktif"
		concl.PrimaryArea = "billing"
		concl.Confidence = 0.9
	case hasAcs && acs.State == StateDeviceOff:
		concl.Diagnosis = "CPE/ONT offline (daya/link fisik)"
		concl.PrimaryArea = "genieacs"
		concl.Confidence = 0.85
	case hasRad && rad.State == StateUnauthed:
		concl.Diagnosis = "autentikasi RADIUS gagal"
		concl.PrimaryArea = "radius"
		concl.Confidence = 0.8
	case hasBill && bill.State == StateActive &&
		hasRad && rad.State == StateAuthed &&
		hasAcs && acs.State == StateDeviceOn &&
		hasMtk && mtk.State == StatePPPoEDown:
		concl.Diagnosis = "sesi PPPoE stale/putus"
		concl.PrimaryArea = "mikrotik"
		concl.Confidence = 0.9
		concl.Alternatives = []string{"perangkat router pelanggan mati", "kabel akses bermasalah"}
	default:
		concl.Diagnosis = "bukti tidak cukup untuk diagnosis pasti"
		concl.PrimaryArea = ""
		concl.Confidence = 0.3
	}

	if hasUnknown {
		concl.Confidence *= 0.5
		if concl.Confidence < 0.1 {
			concl.Confidence = 0.1
		}
		concl.Alternatives = append(concl.Alternatives, "ada data yang tidak diketahui — verifikasi manual")
	}
	if len(missing) > 0 {
		concl.Confidence *= 0.7
		if concl.Confidence < 0.1 {
			concl.Confidence = 0.1
		}
	}

	return concl
}

// Summary adalah representasi JSON untuk logging/audit.
func (s *Set) Summary() map[string]any {
	out := map[string]any{}
	for d, e := range s.items {
		out[d] = map[string]any{"state": e.State, "raw": e.Raw}
	}
	return out
}

func contains(v string, subs ...string) bool {
	for _, s := range subs {
		if strings.Contains(v, s) {
			return true
		}
	}
	return false
}

// String untuk debugging.
func (c Conclusion) String() string {
	return fmt.Sprintf("%s (%.0f%%) area=%s missing=%v", c.Diagnosis, c.Confidence*100, c.PrimaryArea, c.Missing)
}
