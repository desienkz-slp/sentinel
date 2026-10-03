package directory

import "strings"

// Pertanyaan status koneksi integrasi dari staf ("sudah bisa terhubung ke
// billing?"). Dikenali KODE, dijawab dari pengecekan kesehatan adaptor yang
// sebenarnya — bukan dari LLM, yang tanpa tool call hanya menebak dan pernah
// menjawab "belum ada pengecekan ke server billing" padahal billing terhubung.

var (
	// Kata penanda pertanyaan status/koneksi (token utuh, huruf kecil).
	statusWords = map[string]bool{
		"terhubung": true, "tersambung": true, "nyambung": true, "konek": true,
		"koneksi": true, "connect": true, "connected": true, "online": true,
		"offline": true, "aktif": true, "jalan": true, "berjalan": true,
		"normal": true, "ok": true, "baca": true, "membaca": true, "akses": true,
		"error": true, "down": true, "sehat": true, "health": true,
	}
	// Kata domain -> nama domain adaptor.
	domainWords = map[string]string{
		"billing": "billing", "netora": "billing", "tagihan": "billing",
		"radius":   "radius",
		"mikrotik": "mikrotik", "router": "mikrotik",
		"genieacs": "genieacs", "acs": "genieacs", "tr069": "genieacs", "tr-069": "genieacs",
	}
	// Kata yang berarti "semua integrasi".
	allWords = map[string]bool{"integrasi": true, "adapter": true, "adaptor": true, "semua": true}
)

// ParseStatusQuestion mengembalikan domain yang ditanyakan ("billing",
// "radius", ...), atau nil bila pesan bukan pertanyaan status koneksi.
// all=true berarti semua integrasi. Aturan ketat: pesan pendek, memuat kata
// domain (atau kata "integrasi") DAN kata status.
func ParseStatusQuestion(msg string) (domains []string, all bool) {
	raw := strings.Fields(strings.TrimSpace(msg))
	if len(raw) == 0 || len(raw) > 10 {
		return nil, false
	}
	hasStatus := false
	seen := map[string]bool{}
	for _, w := range raw {
		t := strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]*_"))
		if statusWords[t] {
			hasStatus = true
		}
		if d, ok := domainWords[t]; ok && !seen[d] {
			seen[d] = true
			domains = append(domains, d)
		}
		if allWords[t] {
			all = true
		}
	}
	if !hasStatus {
		return nil, false
	}
	if len(domains) == 0 && !all {
		return nil, false
	}
	return domains, all
}
