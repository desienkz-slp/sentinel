// Package customtool mewadahi tool read-only yang DIUSULKAN Endpoint B sebagai
// spesifikasi deklaratif, lalu disetujui superadmin sebelum aktif.
//
// Mengapa bukan kode bebas: Endpoint B (LLM) tidak pernah boleh menulis kode Go
// yang dieksekusi — itu melanggar deny-by-default dan membuka eksekusi arbitrer.
// Sebagai gantinya, Endpoint B hanya MENYUSUN Spec: domain + path GET relatif +
// cara ekstraksi field. Spec itu menjadi DRAFT; superadmin menyetujui/menolak.
// Setelah disetujui, sebuah adapter GENERIK mengeksekusi GET pada domain yang
// sama (memakai konfigurasi base URL + auth yang SUDAH ADA di sistem), lalu
// mengekstrak field yang diminta. Tidak ada kode baru yang dieksekusi — hanya
// parameter yang divalidasi.
package customtool

import (
	"fmt"
	"strings"
	"time"
)

// Status adalah status persetujuan satu spec.
type Status string

const (
	StatusDraft   Status = "draft"
	StatusActive  Status = "active"
	StatusRejected Status = "rejected"
)

// Domain yang boleh menjadi target tool custom. Hanya 4 sistem eksternal yang
// sudah dikenal NOC Sentinel — TIDAK ada domain/server arbitrer (anti-SSRF).
var AllowedDomains = map[string]bool{
	"billing": true, "radius": true, "mikrotik": true, "genieacs": true,
}

// Spec adalah satu tool read-only deklaratif.
//
//   - Path: path GET RELATIF ke base URL domain (mis. "/customers/{identity}/invoices").
//     TIDAK boleh URL absolut, mengandung "://", atau ".." (anti-SSRF/path traversal).
//   - Extract: jalur JSON (dot/bracket) untuk mengambil satu nilai dari respons.
//     Hanya karakter aman [a-zA-Z0-9_.\[\]] — bukan ekspresi/kode.
//   - Parameters: skema argumen JSON (untuk function-calling LLM).
type Spec struct {
	Name        string         `json:"name"`
	Domain      string         `json:"domain"`
	Description string         `json:"description"`
	Path        string         `json:"path"`
	Method      string         `json:"method"`
	Extract     string         `json:"extract,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	Status      Status         `json:"status"`
	ProposedBy  string         `json:"proposed_by,omitempty"`
	ProposedAt  time.Time      `json:"proposed_at"`
	ApprovedAt  *time.Time     `json:"approved_at,omitempty"`
	Reason      string         `json:"reason,omitempty"` // alasan usulan / penolakan
}

// Validate memeriksa spec sebelum disimpan sebagai draft. Semua aturan di sini
// adalah gerbang keamanan — jangan pernah dilonggarkan lewat prompt.
func Validate(s Spec) error {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return fmt.Errorf("nama tool wajib diisi")
	}
	// Nama tool mengikuti konvensi domain.aksinya (mis. billing.get_x).
	if !strings.Contains(name, ".") {
		return fmt.Errorf("nama tool harus berformat domain.aksi (mis. billing.get_x)")
	}

	domain := strings.TrimSpace(strings.ToLower(s.Domain))
	if !AllowedDomains[domain] {
		return fmt.Errorf("domain %q tidak diizinkan (hanya billing/radius/mikrotik/genieacs)", domain)
	}

	// Method hanya GET — read-only mutlak. Tidak ada POST/PUT/DELETE.
	if m := strings.ToUpper(strings.TrimSpace(s.Method)); m != "GET" {
		return fmt.Errorf("method %q tidak diizinkan — hanya GET (read-only)", m)
	}

	path := strings.TrimSpace(s.Path)
	if path == "" {
		return fmt.Errorf("path wajib diisi")
	}
	if !strings.HasPrefix(path, "/") {
		return fmt.Errorf("path harus relatif (diawali /), bukan URL absolut")
	}
	low := strings.ToLower(path)
	if strings.Contains(low, "://") || strings.Contains(low, "..") {
		return fmt.Errorf("path mengandung pola berbahaya (URL absolut / traversal)")
	}

	// Extract = jalur JSON aman saja.
	if ex := strings.TrimSpace(s.Extract); ex != "" {
		if !validJSONPath(ex) {
			return fmt.Errorf("extract %q tidak valid (hanya jalur JSON dot/bracket)", ex)
		}
	}
	return nil
}

// validJSONPath memastikan extract hanya berisi karakter jalur JSON yang aman:
// huruf, angka, titik, underscore, strip, dan kurung siku berisi angka. Tanpa
// operator, fungsi, atau karakter yang bisa diinterpretasikan sebagai kode.
func validJSONPath(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.', r == '_', r == '-', r == '[', r == ']', r == '*':
		default:
			return false
		}
	}
	// Tolak ".." agar tidak ada traversal di dalam jalur ekstraksi.
	return !strings.Contains(s, "..")
}
