// Package claimguard mendeteksi klaim FAKTA SISTEM (angka metrik, status
// perangkat/layanan) pada jawaban model. Dipakai agen NOC tool-first
// (docs/PLAN_LANJUTAN_DB_DAN_FASE.md, Fase A): bila jawaban memuat klaim seperti
// itu padahal tidak ada tool call di giliran tersebut, kode mengganti jawabannya.
//
// Fungsi murni: tanpa I/O, tanpa LLM. Detektor sengaja konservatif ke arah
// "tandai": lebih baik minta cek ulang daripada membiarkan fakta karangan.
package claimguard

import (
	"regexp"
	"strings"
)

// Notice = pengganti jawaban yang memuat klaim tanpa bukti.
const Notice = "Belum diperiksa: saya belum menjalankan pengecekan untuk pertanyaan ini, " +
	"jadi tidak ada data sistem yang bisa saya sebutkan. Sebutkan pelanggan atau sistem " +
	"yang ingin dicek (contoh: cek user namauser)."

var (
	// Angka diikuti satuan/objek sistem: "12 pelanggan", "45 ms", "98%", "Rp 150.000".
	reMetric = regexp.MustCompile(`(?i)\b\d[\d.,]*\s*(%|ms\b|mbps\b|kbps\b|gbps\b|dbm\b|db\b|pelanggan\b|user\b|sesi\b|session\b|perangkat\b|device\b|paket\b|hari\b|jam\b|menit\b|detik\b)|\brp\.?\s*\d`)
	reIP     = regexp.MustCompile(`\b\d{1,3}(\.\d{1,3}){3}\b`)

	subjects     = `(radius|mikrotik|genieacs|billing|pppoe|olt|router|modem|ont|sesi|session|koneksi|tagihan|server|layanan|akun|pelanggan|internet|jaringan|perangkat|device|link|pon)`
	statuses     = `(online|offline|down|up|aktif|nonaktif|mati|normal|sehat|terisolir|isolir|lancar|bermasalah|gangguan|tersambung|terhubung|terputus|putus|lunas|menunggak|tertunggak|jatuh tempo)`
	reSubjStatus = regexp.MustCompile(`(?i)\b` + subjects + `\b[^.\n]{0,40}?\b` + statuses + `\b`)
	reStatusSubj = regexp.MustCompile(`(?i)\b` + statuses + `\b[^.\n]{0,15}?\b` + subjects + `\b`)

	// Tanda bahwa kalimat hanya mengaku belum tahu / meminta data: bukan klaim.
	reHedge = regexp.MustCompile(`(?i)\b(belum (diperiksa|dicek|bisa memastikan)|tidak (bisa|dapat) memastikan|perlu (dicek|diperiksa)|silakan (sebutkan|kirim)|pelanggan mana|apakah)\b`)
)

// Result = hasil pemeriksaan.
type Result struct {
	Flagged bool     // ada klaim fakta sistem
	Reasons []string // jenis klaim (tanpa mengutip isi jawaban)
}

// Detect memeriksa teks jawaban. Teks kosong -> tidak ditandai.
func Detect(answer string) Result {
	a := strings.TrimSpace(answer)
	if a == "" {
		return Result{}
	}
	var r Result
	if reMetric.MatchString(a) {
		r.Reasons = append(r.Reasons, "angka metrik")
	}
	if reIP.MatchString(a) {
		r.Reasons = append(r.Reasons, "alamat IP")
	}
	if reSubjStatus.MatchString(a) || reStatusSubj.MatchString(a) {
		if !reHedge.MatchString(a) || reMetric.MatchString(a) || reIP.MatchString(a) {
			r.Reasons = append(r.Reasons, "status sistem")
		}
	}
	r.Flagged = len(r.Reasons) > 0
	return r
}

// Guard menerapkan aturan: klaim fakta tanpa tool call -> diganti Notice.
// toolUsed = ada setidaknya satu tool call di giliran ini.
// Mengembalikan teks akhir dan apakah diganti.
func Guard(answer string, toolUsed bool) (string, bool) {
	if toolUsed {
		return answer, false
	}
	if Detect(answer).Flagged {
		return Notice, true
	}
	return answer, false
}
