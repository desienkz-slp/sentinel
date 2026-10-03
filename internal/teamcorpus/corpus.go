// Package teamcorpus berisi korpus uji SINTETIS untuk router/tim CS-NOC.
//
// Semua isi dibuat dari POLA kejadian, bukan salinan percakapan nyata: tidak ada
// nomor, nama, username, atau JID asli. Username memakai pola "pelanggan-uji",
// "uji-01", dst. Jangan menambahkan data nyata ke berkas ini (repo publik).
package teamcorpus

// Pengirim = peran sintetis pengirim pesan.
type Pengirim string

const (
	Pelanggan Pengirim = "pelanggan"
	StafNOC   Pengirim = "staf_noc"
	StafAdmin Pengirim = "staf_admin"
)

// Handler = jalur penanganan yang diharapkan (kode vs LLM).
type Handler string

const (
	HStatusIntegrasi Handler = "status_integrasi"
	HDaftarPelanggan Handler = "daftar_pelanggan"
	HPerintahCek     Handler = "perintah_cek" // cek billing/pppoe/traffic/riwayat
	HLLM             Handler = "llm"          // diteruskan ke agen (tak cocok pola)
)

// Kasus = satu pesan + keputusan yang diharapkan (perilaku v0.2.21).
type Kasus struct {
	Nama    string
	Dari    Pengirim
	Pesan   string
	Harapan Handler
	Catatan string
}

// Staf adalah kasus untuk pengirim staf; hanya jalur kode yang ditetapkan,
// sisanya llm.
var Staf = []Kasus{
	// --- status koneksi integrasi ---
	{"status billing", StafNOC, "sudah bisa terhubung ke billing?", HStatusIntegrasi, "pesan produksi"},
	{"status billing typo", StafNOC, "sudab bisa baca billing?", HStatusIntegrasi, "pesan produksi (typo)"},
	{"status radius", StafNOC, "radius aktif gak", HStatusIntegrasi, ""},
	{"status mikrotik", StafNOC, "mikrotik sudah terhubung?", HStatusIntegrasi, ""},
	{"status semua", StafNOC, "semua integrasi normal?", HStatusIntegrasi, ""},
	{"status gabungan", StafNOC, "billing dan radius ok?", HStatusIntegrasi, ""},
	// --- daftar / ringkasan pelanggan ---
	{"daftar ringkasan", StafNOC, "daftar pelanggan", HDaftarPelanggan, ""},
	{"daftar typo + riwayat", StafNOC, "terkait dafta pelanggan dan history bayar?", HDaftarPelanggan, "pesan produksi (typo)"},
	{"jumlah aktif", StafNOC, "jumlah pelanggan aktif", HDaftarPelanggan, ""},
	{"isolir", StafNOC, "pelanggan isolir", HDaftarPelanggan, ""},
	{"nonaktif", StafNOC, "pelanggan nonaktif", HDaftarPelanggan, ""},
	{"cari", StafNOC, "cari uji-01", HDaftarPelanggan, ""},
	// --- perintah cek ---
	{"cek billing", StafNOC, "cek billing pelanggan-uji", HPerintahCek, ""},
	{"cek tagihan", StafNOC, "cek tagihan pelanggan-uji", HPerintahCek, ""},
	{"riwayat", StafNOC, "riwayat pelanggan-uji", HPerintahCek, ""},
	{"history bayar", StafNOC, "history bayar pelanggan-uji", HPerintahCek, ""},
	{"cek pppoe typo", StafNOC, "cek user pppor pelanggan-uji", HPerintahCek, "pesan produksi (typo)"},
	{"cek pppoe", StafNOC, "Cek user PPPoE Pelanggan-Uji", HPerintahCek, ""},
	{"cek traffic typo", StafNOC, "cek traffict", HPerintahCek, "pesan produksi (typo); target dari sesi"},
	{"cek traffic target", StafNOC, "cek traffic pelanggan-uji", HPerintahCek, ""},
	// --- tidak cocok pola -> LLM ---
	{"sapaan staf", StafNOC, "halo", HLLM, ""},
	{"ping ip", StafNOC, "cek koneksi 8.8.8.8", HLLM, "bukan perintah pelanggan"},
	{"pertanyaan bebas", StafNOC, "kenapa billing si uji-01 mahal banget bulan ini padahal pemakaian biasa", HLLM, "tak cocok pola"},
	{"kosong", StafNOC, "", HLLM, ""},
}

// Pelanggan: pesan dari pelanggan TIDAK PERNAH boleh ditangani jalur perintah
// staf. Harapan selalu HLLM (jalur agen CS) — termasuk pesan yang kebetulan
// memuat kata kunci perintah staf.
var PelangganKasus = []Kasus{
	{"mati", Pelanggan, "internet saya mati dari tadi pagi", HLLM, ""},
	{"lemot", Pelanggan, "internet lemot banget sejak semalam", HLLM, ""},
	{"sapaan", Pelanggan, "halo kak", HLLM, ""},
	{"terima kasih", Pelanggan, "terima kasih ya", HLLM, ""},
	{"tagihan biasa", Pelanggan, "tagihan bulan ini berapa ya", HLLM, ""},
	{"mirip perintah cek billing", Pelanggan, "cek billing pelanggan-uji", HLLM, "pelanggan tak boleh memakai jalur staf"},
	{"mirip perintah daftar", Pelanggan, "daftar pelanggan isolir", HLLM, "pelanggan tak boleh melihat daftar"},
	{"mirip status integrasi", Pelanggan, "sudah bisa terhubung ke billing?", HLLM, "pelanggan tak boleh memicu cek integrasi"},
	{"mirip cek pppoe", Pelanggan, "cek user pppoe pelanggan-uji", HLLM, ""},
	{"keluhan panjang kata kunci", Pelanggan, "pembayaran saya sudah masuk tapi internet masih mati dari kemarin sore sampai sekarang", HLLM, ""},
	{"wifi", Pelanggan, "wifi di rumah sering putus-putus", HLLM, ""},
	{"kosong", Pelanggan, "", HLLM, ""},
}

// KeluhanPanjang: tidak boleh pernah terbaca sebagai perintah (staf maupun
// pelanggan). Dipakai uji properti.
var KeluhanPanjang = []string{
	"tolong cek kenapa internet saya mati dari kemarin sore sampai sekarang belum nyala",
	"pelanggan saya mengeluh internet lambat sejak pagi tadi di area barat",
	"billing saya kok mahal banget ya bulan ini padahal pemakaian biasa saja dan normal",
	"history chat kemarin",
	"internet lemot",
}
