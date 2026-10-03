package directory

import "strings"

// Pertanyaan daftar/ringkasan pelanggan dari staf ("daftar pelanggan",
// "pelanggan isolir", "berapa pelanggan aktif", "cari budi"). Dikenali KODE
// lalu dijawab dari tool billing.list_customers — bukan LLM, yang tanpa tool
// call pernah menjawab "tidak punya akses ke billing".

// ListQuery adalah hasil parsing.
type ListQuery struct {
	Filter string // "" (ringkasan) | aktif | nonaktif | isolir | cari
	Search string // kata kunci untuk Filter "cari"
}

var (
	listWords = map[string]bool{
		"daftar": true, "list": true, "jumlah": true, "berapa": true, "total": true,
		"semua": true, "ringkasan": true, "rekap": true, "statistik": true,
	}
	customerWords = map[string]bool{
		"pelanggan": true, "customer": true, "customers": true, "pelanggannya": true,
	}
	searchVerbs = map[string]bool{"cari": true, "search": true, "temukan": true}
)

// ParseListQuery mengenali pertanyaan daftar/ringkasan pelanggan.
// Aturan ketat: pesan pendek, memuat kata "pelanggan" + kata daftar/jumlah
// (atau filter status), atau diawali kata kerja cari + kata kunci.
func ParseListQuery(msg string) (ListQuery, bool) {
	raw := strings.Fields(strings.TrimSpace(msg))
	if len(raw) == 0 || len(raw) > 8 {
		return ListQuery{}, false
	}
	toks := make([]string, len(raw))
	for i, w := range raw {
		toks[i] = strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]*_"))
	}

	// "cari budi" / "cari pelanggan budi"
	if searchVerbs[toks[0]] {
		for i := 1; i < len(toks); i++ {
			t := toks[i]
			if customerWords[t] || cmdStop[t] || cmdVerbs[t] {
				continue
			}
			cand := strings.Trim(raw[i], ".,;:!?\"'()[]*_")
			if len(cand) >= 3 {
				return ListQuery{Filter: "cari", Search: cand}, true
			}
		}
		return ListQuery{}, false
	}

	hasCustomer, hasList := false, false
	filter := ""
	for _, t := range toks {
		switch {
		case customerWords[t]:
			hasCustomer = true
		case listWords[t] || strings.HasPrefix(t, "daft"): // "dafta", "daftr", "daftar"
			hasList = true
		case t == "isolir" || t == "terisolir" || t == "isolasi":
			filter = "isolir"
		case t == "aktif" || t == "active":
			filter = "aktif"
		case t == "nonaktif" || t == "non-aktif" || t == "inactive" || t == "mati":
			filter = "nonaktif"
		}
	}
	// Perintah detail ("cek billing pelanggan-uji") bukan daftar.
	if !hasCustomer {
		return ListQuery{}, false
	}
	if !hasList && filter == "" {
		return ListQuery{}, false
	}
	// "cek pelanggan isolir"/"status pelanggan aktif" sah; tapi kata kerja cek +
	// target username (token lain di luar kata kunci) bukan daftar.
	for i, t := range toks {
		if customerWords[t] || listWords[t] || strings.HasPrefix(t, "daft") || cmdVerbs[t] || cmdStop[t] ||
			t == "isolir" || t == "terisolir" || t == "isolasi" || t == "aktif" || t == "active" ||
			t == "nonaktif" || t == "non-aktif" || t == "inactive" || t == "mati" ||
			t == "dan" || t == "history" || t == "riwayat" || t == "bayar" || t == "pembayaran" ||
			t == "terkait" || t == "soal" || t == "tentang" || t == "yang" {
			continue
		}
		if reUsername.MatchString(strings.Trim(raw[i], ".,;:!?\"'()[]*_")) && filter == "" && !hasList {
			return ListQuery{}, false
		}
	}
	return ListQuery{Filter: filter}, true
}

// HasHistoryWords melaporkan apakah pesan menyebut riwayat/pembayaran.
func HasHistoryWords(msg string) bool {
	for _, w := range strings.Fields(strings.ToLower(msg)) {
		t := strings.Trim(w, ".,;:!?\"'()[]*_")
		if historyWords[t] {
			return true
		}
	}
	return false
}

var historyWords = map[string]bool{
	"history": true, "histori": true, "riwayat": true, "pembayaran": true,
	"bayar": true, "payment": true,
}
