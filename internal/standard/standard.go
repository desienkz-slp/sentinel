// Package standard menegakkan STANDAR KONTEKS secara deterministik di dalam kode,
// bukan bergantung pada penilaian model.
//
// Alasan: perilaku agen tidak boleh berubah hanya karena model diganti. Klasifikasi
// intent dan aturan kapan boleh melakukan probe dijalankan oleh kode Go dengan
// aturan yang tetap, sehingga standar tetap sama untuk model apa pun.
//
// Standar lengkap (bahasa manusia) ada di standards/context-standard.md dan
// disuntikkan ke prompt sebagai kontrak.
package standard

import (
	_ "embed"
	"os"
	"regexp"
	"strings"
)

//go:embed context-standard.md
var defaultDoc string

// Version dinaikkan setiap kali aturan berubah, supaya laporan bisa dilacak
// dihasilkan oleh standar versi berapa.
const Version = "1.0.0"

// Intent adalah kategori pesan masuk, ditentukan KODE (bukan model).
type Intent string

const (
	IntentChat      Intent = "CHAT"      // sapaan, terima kasih, obrolan ringan
	IntentInfo      Intent = "INFO"      // broadcast / laporan otomatis
	IntentComplaint Intent = "COMPLAINT" // keluhan gangguan jaringan
	IntentUnclear   Intent = "UNCLEAR"   // tidak bisa ditentukan -> tanya balik
)

// Doc mengembalikan teks standar. override dipakai bila diisi (mis. file di disk
// yang bisa diedit operator tanpa build ulang). Bila path tidak terbaca,
// standar bawaan tetap dipakai supaya sistem tidak pernah kehilangan standar.
func Doc(override string) string {
	if p := strings.TrimSpace(override); p != "" {
		if b, err := os.ReadFile(p); err == nil && strings.TrimSpace(string(b)) != "" {
			return string(b)
		}
	}
	return defaultDoc
}

// ---- aturan klasifikasi ----
//
// Dua tingkat kata keluhan, karena pencocokan kasar menyebabkan salah kelas:
//   - strongComplaint: gejala gangguan nyata -> langsung COMPLAINT
//   - weakContext: kata seputar jaringan yang bisa muncul di pertanyaan umum
//     (mis. "apakah bisa cek jaringan?") -> hanya COMPLAINT bila bukan pertanyaan
var strongComplaint = []string{
	// lambat
	"lambat", "lelet", "lemot", "lamban", "slow", "lola", "ngadat",
	// tidak bisa / gagal
	"tidak bisa", "gak bisa", "ga bisa", "nggak bisa", "tidak dapat", "gagal",
	"tidak jalan", "gak jalan", "error", "eror", "tidak bisa login", "login gagal",
	// putus / mati
	"mati", "putus", "terputus", "nyambung", "kedip", "dc", "disconnect",
	"offline", "rto", "timeout", "timed out", "tidak terhubung",
	"tidak konek", "gak konek", "no internet", "tidak ada internet",
	// kualitas
	"loss", "packet loss", "latency", "ping tinggi", "jitter", "buffering",
	"lag", "patah patah", "loading terus", "berat",
	// perangkat
	"rusak", "kendala", "gangguan", "trouble", "troubleshoot", "isolir",
}

var weakContext = []string{
	"internet", "koneksi", "jaringan", "wifi", "wlan", "lan", "kabel",
	"router", "modem", "ont", "cpe", "pppoe", "radius", "dns",
	"cek koneksi", "cek jaringan", "ping", "bandwidth", "speedtest",
}

// chatWords: sapaan & obrolan ringan. Dicocokkan PER KATA UTUH (bukan substring)
// supaya "p" tidak cocok dengan "pppoe" atau "hi" tidak cocok dengan "wifi".
var chatWords = []string{
	"halo", "hallo", "helo", "hai", "hei", "pagi", "siang", "sore", "malam",
	"terima", "kasih", "terimakasih", "makasih", "thanks", "thank", "tq", "thx",
	"assalamualaikum", "salam", "permisi", "selamat", "mohon",
	"oke", "ok", "okay", "okey", "siap", "baik", "noted", "sip", "ya", "yap",
	"tes", "test", "testing",
	"bantu", "bisa",
}

// infoMarkers: penanda broadcast/laporan otomatis dari sistem monitoring.
var infoMarkers = []string{
	"status user", "total user", "terhubung kembali", "powered by",
	"router:", "server pusat", "laporan otomatis", "notifikasi sistem",
	"user terputus", "user online",
}

// questionWords: pesan yang menanyakan sesuatu (bukan melaporkan gangguan).
var questionWords = []string{
	"apakah", "berapa", "kapan", "dimana", "di mana", "siapa", "mengapa", "kenapa",
	"bagaimana", "gimana", "cara", "boleh", "bisa kah", "bisakah",
}

var (
	reSpace = regexp.MustCompile(`\s+`)
	rePunct = regexp.MustCompile(`[^\p{L}\p{N}\s]+`)
)

// Normalize menyiapkan teks untuk pencocokan: huruf kecil, tanda baca dibuang,
// spasi dirapatkan. Dipakai klasifikasi dan kunci cache agar konsisten.
func Normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = rePunct.ReplaceAllString(s, " ")
	s = reSpace.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// hasWord mencocokkan kata/frasa utuh dengan batas spasi, bukan substring.
// Ini mencegah "p" cocok di "pppoe" dan "hi" cocok di "wifi".
func hasWord(padded, w string) bool {
	w = strings.TrimSpace(w)
	if w == "" {
		return false
	}
	return strings.Contains(padded, " "+w+" ")
}

// hasAny memeriksa daftar kata/frasa utuh.
func hasAny(padded string, words []string) bool {
	for _, w := range words {
		if hasWord(padded, w) {
			return true
		}
	}
	return false
}

// Classify menentukan intent pesan. Urutan pemeriksaan penting:
// INFO -> keluhan kuat -> pertanyaan -> konteks jaringan -> CHAT -> UNCLEAR.
//
// Pertanyaan diperiksa SEBELUM konteks jaringan, supaya "apakah nomor ini bisa
// dipakai cek jaringan?" tidak salah dibaca sebagai keluhan.
func Classify(msg string) Intent {
	n := Normalize(msg)
	if n == "" {
		return IntentUnclear
	}
	padded := " " + n + " "

	for _, m := range infoMarkers {
		if strings.Contains(n, m) {
			return IntentInfo
		}
	}

	// Gejala gangguan nyata -> keluhan, apa pun bentuk kalimatnya.
	if hasAny(padded, strongComplaint) {
		return IntentComplaint
	}

	// Pertanyaan tanpa gejala gangguan -> minta perjelas.
	bertanya := strings.Contains(msg, "?") || hasAny(padded, questionWords)
	if bertanya {
		return IntentUnclear
	}

	// Kata seputar jaringan tanpa gejala (mis. "tolong cek koneksi ke 8.8.8.8").
	if hasAny(padded, weakContext) {
		return IntentComplaint
	}

	// Sapaan/obrolan ringan, dicocokkan per kata utuh.
	if hasAny(padded, chatWords) {
		return IntentChat
	}

	return IntentUnclear
}

// BolehProbe: hanya keluhan nyata yang boleh memicu pengecekan jaringan.
// Ini gerbang keras — model tidak bisa memaksa probe pada pesan obrolan.
func BolehProbe(i Intent) bool { return i == IntentComplaint }
