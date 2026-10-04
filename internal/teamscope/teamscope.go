// Package teamscope menegakkan batas akses tiap TIM di level KODE, bukan prompt.
//
// Dua jaminan (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 2):
//
//  1. Tool yang boleh dipaparkan dan dieksekusi per tim ditentukan daftar di sini.
//     Tool di luar daftar ditolak walau model memintanya.
//  2. Tim CS hanya boleh membaca data MILIK pelanggan yang sedang dilayani.
//     Argumen identitas dari model TIDAK dipercaya: ditimpa dengan identitas
//     terverifikasi dari hasil identifikasi pengirim, atau ditolak.
//
// Paket ini murni (tanpa I/O): hasilnya hanya bergantung pada argumen, jadi
// bisa diuji tuntas dan hasilnya sama untuk model apa pun.
package teamscope

import (
	"fmt"
	"sort"
	"strings"

	"ainoc/internal/router"
)

// Verdict = hasil pemeriksaan satu pemanggilan tool.
type Verdict struct {
	Allowed bool
	// Reason selalu terisi saat ditolak; aman ditampilkan ke model/audit.
	Reason string
	// Args = argumen yang BOLEH dipakai. Untuk CS, identitas sudah dipaksa ke
	// pelanggan terverifikasi. Selalu salinan: map masukan tidak diubah.
	Args map[string]any
	// Rewritten = true bila argumen dari model diubah/ditimpa.
	Rewritten bool
}

// csTools = tool yang boleh dipakai tim CS (hanya data satu pelanggan).
var csTools = map[string]bool{
	"billing.get_customer":      true,
	"billing.get_history":       true,
	"mikrotik.get_pppoe_status": true,
	"genieacs.get_device_state": true,
}

// csProbes = probe jaringan bawaan (internal/diag) yang boleh dipakai tim CS.
// Nama harus SAMA dengan nama tool di diag. Probe yang mengungkap info host
// server (interface, service, system) atau infrastruktur (radius, traceroute)
// sengaja TIDAK ada di sini.
var csProbes = map[string]bool{
	"ping": true, "dns": true, "tcp": true, "http": true,
}

// identityKeys = semua nama argumen yang membawa identitas pelanggan/akun.
// Dikumpulkan dari adapter nyata (billing, radius, mikrotik, genieacs).
var identityKeys = []string{
	"identity", "username", "name", "customer", "search", "phone",
	"device_id", "_id", "serial", "customer_id",
}

// scopeless = tool tanpa argumen identitas yang sah, tetapi MELIHAT banyak
// pelanggan/sistem. Tidak pernah boleh untuk CS.
// (Dipakai untuk pesan penolakan yang jelas.)
var scopeless = map[string]string{
	"billing.list_customers":  "daftar pelanggan",
	"radius.get_system_stats": "statistik sistem",

	"mikrotik.get_interface_stats":  "statistik interface",
	"mikrotik.get_interface_live":   "trafik interface",
	"mikrotik.get_customer_traffic": "trafik pelanggan",
	"genieacs.get_devices":          "daftar perangkat",
}

// CSToolNames mengembalikan semua nama tool yang diizinkan untuk tim CS.
func CSToolNames() []string {
	out := make([]string, 0, len(csTools)+len(csProbes))
	for n := range csTools {
		out = append(out, n)
	}
	for n := range csProbes {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Classified melaporkan apakah tool sudah diklasifikasi eksplisit: boleh untuk
// CS (csTools/csProbes) atau dicatat sebagai cakupan luas (scopeless). Tool
// tulis dicatat di writeTools. Dipakai uji agar tool baru tak lolos tanpa tinjauan.
func Classified(name string) bool {
	if csTools[name] || csProbes[name] || writeTools[name] {
		return true
	}
	_, ok := scopeless[name]
	return ok
}

// writeTools = tool tulis yang tidak pernah boleh untuk CS.
var writeTools = map[string]bool{"mikrotik.disconnect_pppoe": true}

// CSAllowsTool melaporkan apakah tool boleh dipakai tim CS (registry atau probe).
func CSAllowsTool(name string) bool { return csTools[name] || csProbes[name] }

// Customer = identitas pelanggan TERVERIFIKASI (hasil identifikasi pengirim),
// bukan nilai dari model.
type Customer struct {
	Username string
	Phone    string
}

// Valid melaporkan apakah ada identitas yang bisa dipakai membatasi akses.
func (c Customer) Valid() bool { return strings.TrimSpace(c.Username) != "" }

// Check memeriksa satu pemanggilan tool untuk sebuah tim.
//
//   - NOC/CS-Lead: tool boleh (otorisasi peran tetap di directory.Authorize),
//     argumen tidak diubah.
//   - CS: tool harus ada di daftar CS; tool probe jaringan diteruskan apa
//     adanya; tool registry dipaksa ke identitas pelanggan terverifikasi.
//     Tanpa identitas terverifikasi, tool registry DITOLAK (gagal tertutup).
func Check(team router.Team, tool string, args map[string]any, cust Customer) Verdict {
	out := copyArgs(args)

	if team != router.TeamCS {
		return Verdict{Allowed: true, Args: out}
	}

	// ---- Tim CS ----
	if csProbes[tool] {
		return Verdict{Allowed: true, Args: out}
	}
	if what, ok := scopeless[tool]; ok {
		return Verdict{Reason: fmt.Sprintf("tim CS tidak boleh mengakses %s (hanya data pelanggan yang sedang dilayani)", what), Args: out}
	}
	if !csTools[tool] {
		return Verdict{Reason: fmt.Sprintf("tool %q tidak tersedia untuk tim CS", tool), Args: out}
	}
	if !cust.Valid() {
		return Verdict{Reason: "identitas pelanggan belum terverifikasi; data akun tidak dapat dibaca", Args: out}
	}

	rewritten := false
	for _, k := range identityKeys {
		if v, present := out[k]; present {
			if s, isStr := v.(string); !isStr || !sameIdentity(s, cust) {
				rewritten = true
			}
			delete(out, k)
		}
	}
	// Tool yang menerima device_id (GenieACS) tidak boleh diberi id dari model:
	// perangkat dicari lewat identitas pelanggan oleh adapter.
	out["identity"] = cust.Username
	return Verdict{Allowed: true, Args: out, Rewritten: rewritten}
}

// sameIdentity: nilai dari model dianggap sama dengan pelanggan terverifikasi
// bila cocok dengan username atau nomor (tanpa beda huruf besar/kecil).
func sameIdentity(v string, c Customer) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return false
	}
	if v == strings.ToLower(strings.TrimSpace(c.Username)) {
		return true
	}
	if c.Phone != "" && digits(v) != "" && digits(v) == digits(c.Phone) {
		return true
	}
	return false
}

func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func copyArgs(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+1)
	for k, v := range in {
		out[k] = v
	}
	return out
}

// FilterTools menyaring daftar NAMA tool yang dipaparkan ke model sesuai tim.
// Untuk NOC/CS-Lead daftar tidak berubah.
func FilterTools(team router.Team, names []string) []string {
	if team != router.TeamCS {
		return append([]string(nil), names...)
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if CSAllowsTool(n) {
			out = append(out, n)
		}
	}
	return out
}
