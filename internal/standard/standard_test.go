package standard

import "testing"

// TestClassify adalah inti standar: keputusan intent harus konsisten untuk
// model apa pun karena ditentukan kode, bukan penilaian LLM.
func TestClassify(t *testing.T) {
	cases := []struct {
		nama string
		msg  string
		mau  Intent
	}{
		// Sapaan & obrolan ringan -> CHAT
		{"sapaan halo", "halo", IntentChat},
		{"sapaan pagi", "pagi pak", IntentChat},
		{"terima kasih", "terima kasih", IntentChat},
		{"makasih panjang", "terimakasih banyak ya pak", IntentChat},
		{"tes", "tes", IntentChat},
		{"ok", "oke siap", IntentChat},

		// Broadcast/laporan otomatis -> INFO
		{"status user", "📡 STATUS USER: SERVER KABUH\nTotal User Terputus : 2", IntentInfo},
		{"terhubung kembali", "✅ TERHUBUNG KEMBALI\nRouter: SERVER PUSAT", IntentInfo},
		{"powered by", "Laporan otomatis\n_Powered by JFN_", IntentInfo},

		// Keluhan nyata -> COMPLAINT
		{"lambat", "internet saya lambat sejak pagi", IntentComplaint},
		{"lemot", "koneksi lemot banget", IntentComplaint},
		{"mati total", "internet mati total", IntentComplaint},
		{"tidak bisa buka", "tidak bisa buka google", IntentComplaint},
		{"putus nyambung", "wifi putus nyambung terus", IntentComplaint},
		{"pppoe", "login pppoe gagal terus", IntentComplaint},
		{"minta cek", "tolong cek koneksi ke 8.8.8.8", IntentComplaint},
		{"loss", "ping tinggi dan packet loss", IntentComplaint},

		// Tidak bisa ditentukan -> UNCLEAR
		{"tidak jelas", "tahlil kan ?", IntentUnclear},
		{"pertanyaan umum", "apakah nomor ini bisa dipakai cek jaringan?", IntentUnclear},
		{"kosong", "", IntentUnclear},
		{"acak", "asdkjhasd", IntentUnclear},
	}
	for _, c := range cases {
		t.Run(c.nama, func(t *testing.T) {
			if got := Classify(c.msg); got != c.mau {
				t.Errorf("Classify(%q) = %q, mau %q", c.msg, got, c.mau)
			}
		})
	}
}

// Hanya keluhan yang boleh memicu probe. Ini gerbang keras yang membuat standar
// tidak bisa dilanggar model.
func TestBolehProbe(t *testing.T) {
	boleh := []Intent{IntentComplaint}
	tidakBoleh := []Intent{IntentChat, IntentInfo, IntentUnclear}
	for _, i := range boleh {
		if !BolehProbe(i) {
			t.Errorf("BolehProbe(%q) = false, mau true", i)
		}
	}
	for _, i := range tidakBoleh {
		if BolehProbe(i) {
			t.Errorf("BolehProbe(%q) = true, mau false", i)
		}
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, mau string }{
		{"Internet LAMBAT!", "internet lambat"},
		{"  halo,,,  ", "halo"},
		{"Cek   ke   8.8.8.8", "cek ke 8 8 8 8"},
		{"", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.mau {
			t.Errorf("Normalize(%q) = %q, mau %q", c.in, got, c.mau)
		}
	}
}

// Normalisasi harus stabil supaya kunci cache konsisten untuk variasi penulisan.
func TestNormalizeStabilUntukCache(t *testing.T) {
	a := Normalize("Internet Lambat!!")
	b := Normalize("internet   lambat")
	if a != b {
		t.Errorf("normalisasi tidak stabil: %q vs %q", a, b)
	}
}

// Dokumen standar harus selalu tersedia (di-embed) dan memuat aturan inti.
func TestDocTersedia(t *testing.T) {
	doc := Doc("")
	if len(doc) < 500 {
		t.Fatalf("dokumen standar terlalu pendek: %d karakter", len(doc))
	}
	for _, wajib := range []string{"TAHAP 1", "TAHAP 2", "TAHAP 3", "VERDICT", "per nomor"} {
		if !contains(doc, wajib) {
			t.Errorf("dokumen standar kehilangan bagian %q", wajib)
		}
	}
}

// Path override yang tidak terbaca harus jatuh ke standar bawaan, bukan kosong —
// supaya sistem tidak pernah kehilangan standar.
func TestDocFallbackSaatPathSalah(t *testing.T) {
	doc := Doc("/path/tidak/ada/standar.md")
	if doc != Doc("") {
		t.Error("path salah harus memakai standar bawaan")
	}
	if len(doc) < 500 {
		t.Error("standar bawaan kosong saat path salah")
	}
}

func TestVersion(t *testing.T) {
	if Version == "" {
		t.Error("Version harus terisi")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
