// Package presentation menegakkan BENTUK dan KEBERSIHAN balasan ke pelanggan di
// level kode, sehingga tidak bergantung pada model (docs/PLAN_DUA_TIM_CS_NOC.md,
// Fase 3).
//
// Prinsip: penyaring bersifat MENYUNTING, bukan memblokir. Bagian yang bocor
// diganti penanda netral dan balasan tetap terkirim; pelanggan tidak pernah
// dibiarkan tanpa jawaban hanya karena penyaring curiga. Setiap penyuntingan
// dilaporkan (kategori saja — tidak pernah isi aslinya) untuk metrik dan audit.
//
// Paket ini murni (tanpa I/O): hasilnya hanya bergantung pada argumen.
package presentation

import (
	"regexp"
	"strings"
)

// Kategori kebocoran yang disaring dari balasan PELANGGAN.
const (
	KatNomor    = "nomor_telepon" // nomor telepon (pelanggan lain / staf)
	KatHost     = "host_internal" // IP privat, hostname/URL internal
	KatSecret   = "kredensial"    // token, API key, password, hash
	KatToolName = "nama_tool"     // nama tool internal (billing.get_customer, dst.)
	KatJID      = "jid_wa"        // JID WhatsApp (@lid, @s.whatsapp.net, @g.us)
	KatStaf     = "identitas_staf"
)

// Temuan = satu kategori yang disunting beserta jumlahnya. Tidak memuat isi.
type Temuan struct {
	Kategori string
	Jumlah   int
}

// Hasil penyaringan.
type Hasil struct {
	Teks     string   // teks setelah disunting
	Temuan   []Temuan // kategori yang ditemukan (kosong = bersih)
	Diubah   bool
	Dipotong bool
}

var (
	// 62xxxxxxxxx / +62 xxx-xxxx-xxxx / 08xx-xxxx-xxxx.
	reNomor = regexp.MustCompile(`(?:\+?62|\b0)[\s.\-]?8[0-9](?:[\s.\-]?[0-9]){6,11}\b`)
	// IP privat RFC1918 + loopback + link-local.
	rePrivIP = regexp.MustCompile(`\b(?:10\.\d{1,3}\.\d{1,3}\.\d{1,3}|172\.(?:1[6-9]|2\d|3[01])\.\d{1,3}\.\d{1,3}|192\.168\.\d{1,3}\.\d{1,3}|127\.\d{1,3}\.\d{1,3}\.\d{1,3}|169\.254\.\d{1,3}\.\d{1,3})(?::\d{2,5})?\b`)
	// URL/host internal: *.local, *.internal, *.lan, localhost.
	reHostInt = regexp.MustCompile(`(?i)\b(?:https?://)?(?:localhost|[a-z0-9\-]+\.(?:local|internal|lan|corp))(?::\d{2,5})?(?:/\S*)?`)
	// Token/kunci: kata kunci eksplisit diikuti nilai, atau string panjang.
	reSecretKV = regexp.MustCompile(`(?i)\b(?:authorization|x-noc-api-key|api[_ -]?key|secret|password|passwd|token|bearer|pass)\b[ \t]*[:=]?[ \t]*(?:bearer[ \t]+)?[A-Za-z0-9_.=+/-]{6,}`)
	reHexLong  = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	reJWTLike  = regexp.MustCompile(`\b[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{20,}\.[A-Za-z0-9_\-]{10,}\b`)
	reSanctum  = regexp.MustCompile(`\b\d{1,6}\|[A-Za-z0-9]{30,}\b`)
	reToolName = regexp.MustCompile(`\b(?:billing|radius|mikrotik|genieacs)\.[a-z_]{3,}\b`)
	reJID      = regexp.MustCompile(`\b\d{6,}(?::\d+)?@(?:lid|s\.whatsapp\.net|g\.us|c\.us)\b`)
)

const (
	penandaNomor  = "[nomor disembunyikan]"
	penandaHost   = "[alamat internal]"
	penandaSecret = "[disembunyikan]"
	penandaTool   = "[sistem internal]"
	penandaJID    = "[id disembunyikan]"
	penandaStaf   = "tim kami"
)

// Opsi penyaringan.
type Opsi struct {
	// NomorBoleh = nomor telepon yang BOLEH muncul (mis. nomor pelanggan itu sendiri).
	NomorBoleh []string
	// NamaStaf = nama/nomor staf yang tidak boleh disebut ke pelanggan.
	NamaStaf []string
	// MaxLen > 0 memotong balasan melebihi batas (di batas kata).
	MaxLen int
}

// digits mengambil digit saja.
func digits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// norm62 menormalkan nomor Indonesia ke bentuk 62xxxxxxxxxx.
func norm62(s string) string {
	d := digits(s)
	switch {
	case strings.HasPrefix(d, "62"):
		return d
	case strings.HasPrefix(d, "0"):
		return "62" + d[1:]
	}
	return d
}

// SaringPelanggan menyunting balasan yang akan dikirim ke PELANGGAN.
func SaringPelanggan(teks string, o Opsi) Hasil {
	h := Hasil{Teks: teks}
	count := map[string]int{}
	boleh := map[string]bool{}
	for _, n := range o.NomorBoleh {
		if d := norm62(n); d != "" {
			boleh[d] = true
		}
	}

	rep := func(re *regexp.Regexp, kat, penanda string, keep func(string) bool) {
		h.Teks = re.ReplaceAllStringFunc(h.Teks, func(m string) string {
			if keep != nil && keep(m) {
				return m
			}
			count[kat]++
			return penanda
		})
	}

	// Urutan penting: pola paling spesifik dulu (JID sebelum nomor).
	rep(reJID, KatJID, penandaJID, nil)
	rep(reSanctum, KatSecret, penandaSecret, nil)
	rep(reJWTLike, KatSecret, penandaSecret, nil)
	rep(reSecretKV, KatSecret, penandaSecret, nil)
	rep(reHexLong, KatSecret, penandaSecret, nil)
	rep(reToolName, KatToolName, penandaTool, nil)
	rep(rePrivIP, KatHost, penandaHost, nil)
	rep(reHostInt, KatHost, penandaHost, nil)
	rep(reNomor, KatNomor, penandaNomor, func(m string) bool { return boleh[norm62(m)] })

	for _, s := range o.NamaStaf {
		s = strings.TrimSpace(s)
		if len(s) < 3 {
			continue
		}
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(s) + `\b`)
		h.Teks = re.ReplaceAllStringFunc(h.Teks, func(string) string {
			count[KatStaf]++
			return penandaStaf
		})
	}

	if o.MaxLen > 0 && len(h.Teks) > o.MaxLen {
		cut := strings.LastIndex(h.Teks[:o.MaxLen], " ")
		if cut < o.MaxLen/2 {
			cut = o.MaxLen
		}
		h.Teks = strings.TrimSpace(h.Teks[:cut]) + "…"
		h.Dipotong = true
	}

	for _, k := range []string{KatJID, KatSecret, KatToolName, KatHost, KatNomor, KatStaf} {
		if n := count[k]; n > 0 {
			h.Temuan = append(h.Temuan, Temuan{Kategori: k, Jumlah: n})
		}
	}
	h.Diubah = h.Teks != teks
	return h
}
