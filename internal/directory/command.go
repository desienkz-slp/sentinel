package directory

import (
	"regexp"
	"strings"
)

// Perintah singkat dari staf internal lewat WhatsApp (mis. "cek user pppoe
// pelanggan-uji", "cek traffic"). Dikenali oleh KODE (bukan model) supaya perilaku
// agen terhadap NOC Senior/Admin tetap sama untuk model apa pun: staf bukan
// pelanggan, jadi pesan ini tidak boleh dijawab dengan "apa kendala Anda?".

// CommandKind adalah jenis perintah staf.
type CommandKind string

const (
	CmdNone    CommandKind = ""        // bukan perintah staf -> alur biasa
	CmdPPPoE   CommandKind = "pppoe"   // cek akun/sesi PPPoE satu user
	CmdTraffic CommandKind = "traffic" // cek traffic satu user
)

// Command adalah hasil parsing pesan staf.
type Command struct {
	Kind   CommandKind
	Target string // username PPPoE; kosong = pakai target terakhir percakapan
}

var (
	cmdVerbs = map[string]bool{
		"cek": true, "check": true, "cekin": true, "status": true, "lihat": true,
		"cari": true, "tampilkan": true, "info": true, "monitor": true, "tolong": true,
	}
	trafficWords = map[string]bool{
		"traffic": true, "traffict": true, "trafic": true, "trafik": true,
		"bandwidth": true, "usage": true, "pemakaian": true,
	}
	// Kata pengisi yang tidak mungkin menjadi username.
	cmdStop = map[string]bool{
		"user": true, "pppoe": true, "ppp": true, "akun": true, "pelanggan": true,
		"dong": true, "ya": true, "nya": true, "si": true, "untuk": true, "milik": true,
		"yang": true, "dari": true, "di": true, "ini": true, "tadi": true, "lagi": true,
		"mohon": true, "bantu": true, "pak": true, "bu": true, "kak": true, "mas": true,
	}
	reUsername = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{2,63}$`)
)

func isPPPoEWord(w string) bool {
	// "pppoe", "pppor" (typo), "ppp", "pppoa" ... semuanya berawalan "ppp".
	return strings.HasPrefix(w, "ppp") || w == "user" || w == "akun"
}

// ParseCommand mengenali perintah staf dari teks pesan. Aturan sengaja ketat
// supaya keluhan biasa ("internet lemot") tidak salah dibaca sebagai perintah:
//   - kata pertama harus kata kerja perintah (cek/status/...) atau kata kunci
//     (traffic/pppoe/user), DAN
//   - harus memuat kata kunci jenis cek (pppoe/user/akun atau traffic).
func ParseCommand(msg string) Command {
	raw := strings.Fields(strings.TrimSpace(msg))
	if len(raw) == 0 || len(raw) > 12 {
		return Command{}
	}
	toks := make([]string, len(raw))
	for i, w := range raw {
		toks[i] = strings.ToLower(strings.Trim(w, ".,;:!?\"'()[]*_"))
	}

	first := toks[0]
	hasTraffic, hasPPPoE := false, false
	for _, t := range toks {
		if trafficWords[t] {
			hasTraffic = true
		}
		if isPPPoEWord(t) {
			hasPPPoE = true
		}
	}
	if !cmdVerbs[first] && !trafficWords[first] && !isPPPoEWord(first) {
		return Command{}
	}
	if !hasTraffic && !hasPPPoE {
		return Command{}
	}

	// Target = token pertama (kapitalisasi asli) yang bukan kata perintah/pengisi.
	target := ""
	for i, t := range toks {
		if cmdVerbs[t] || trafficWords[t] || isPPPoEWord(t) || cmdStop[t] {
			continue
		}
		cand := strings.Trim(raw[i], ".,;:!?\"'()[]*_")
		if reUsername.MatchString(cand) {
			target = cand
			break
		}
	}

	if hasTraffic {
		return Command{Kind: CmdTraffic, Target: target}
	}
	return Command{Kind: CmdPPPoE, Target: target}
}
