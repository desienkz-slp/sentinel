package presentation

import (
	"strings"
	"testing"
)

// ---- POSITIF: kebocoran harus tersunting --------------------------------

func TestSaringKebocoran(t *testing.T) {
	cases := []struct {
		nama, masuk, kategori string
		tidakBoleh            string // potongan yang tidak boleh tersisa
	}{
		{"nomor 62", "hubungi 628111222333 ya", KatNomor, "628111222333"},
		{"nomor +62 spasi", "nomor +62 811-1222-333 bermasalah", KatNomor, "1222"},
		{"nomor 08", "telepon 08111222333 sekarang", KatNomor, "08111222333"},
		{"nomor titik", "WA 0811.1222.333 ya", KatNomor, "1222"},
		{"ip privat 172", "server di 172.16.0.10 bermasalah", KatHost, "172.16.0.10"},
		{"ip privat 10", "cek 10.10.10.1:8728 ya", KatHost, "10.10.10.1"},
		{"ip privat 192", "router 192.168.1.1 mati", KatHost, "192.168.1.1"},
		{"host lokal", "buka http://noc.internal:8090/settings", KatHost, "noc.internal"},
		{"localhost", "lihat localhost:8090", KatHost, "localhost"},
		{"api key kv", "pakai api_key: abcd1234efgh5678 untuk akses", KatSecret, "abcd1234efgh5678"},
		{"token bearer", "Authorization: Bearer abcdef0123456789xyz", KatSecret, "abcdef0123456789xyz"},
		{"hex 64", "kunci 3f2a9c1e8b7d4a6f5e0c9b8a7d6e5f4c3b2a1908f7e6d5c4b3a2918070605040 itu", KatSecret, "3f2a9c1e8b7d4a6f"},
		{"sanctum", "token 12|abcdefghijklmnopqrstuvwxyz0123456789ABCD", KatSecret, "abcdefghijklmnop"},
		{"nama tool", "saya panggil billing.get_customer tadi", KatToolName, "billing.get_customer"},
		{"nama tool radius", "hasil radius.get_session kosong", KatToolName, "radius.get_session"},
		{"jid lid", "dari 100000000000001@lid tadi", KatJID, "100000000000001"},
		{"jid wa", "kirim ke 628111222333@s.whatsapp.net", KatJID, "628111222333"},
	}
	for _, c := range cases {
		h := SaringPelanggan(c.masuk, Opsi{})
		if strings.Contains(h.Teks, c.tidakBoleh) {
			t.Errorf("[%s] masih bocor %q: %q", c.nama, c.tidakBoleh, h.Teks)
		}
		ada := false
		for _, f := range h.Temuan {
			if f.Kategori == c.kategori {
				ada = true
			}
		}
		if !ada {
			t.Errorf("[%s] kategori %s tak terlapor: %+v", c.nama, c.kategori, h.Temuan)
		}
		if !h.Diubah {
			t.Errorf("[%s] Diubah harus true", c.nama)
		}
	}
}

// ---- NEGATIF: teks sah TIDAK boleh berubah --------------------------------

func TestSaringTidakMerusakTeksSah(t *testing.T) {
	sah := []string{
		"Tagihan bulan ini Rp150.000, jatuh tempo tanggal 10.",
		"Paket 20 Mbps, harga Rp 250.000 per bulan.",
		"Tunggakan 2 bulan, total Rp300.000 (2026-08 dan 2026-09).",
		"Sudah kami cek: akun Anda aktif dan sesi terhubung.",
		"Mohon restart modem dulu, tunggu 2 menit, lalu coba lagi.",
		"Ping rata-rata 12.9 ms dengan 0% packet loss.",
		"Latensi 45 ms, jitter 3 ms, tidak ada paket yang hilang.",
		"Kode tagihan 20260915 sudah tercatat.",
		"Terakhir lunas: 2026-07. Dibayar di muka: 2 bulan.",
		"Halo Kak, ada yang bisa dibantu?",
		"Versi 1.2.3 sudah terpasang. Port 8080 dipakai aplikasi.",
		"Sesi terakhir 3 jam yang lalu, kecepatan 18.5 Mbps.",
		"Hubungi kami kapan saja, kami siap membantu.",
		"Kendala ini sudah diteruskan ke tim teknis kami.",
	}
	for _, s := range sah {
		h := SaringPelanggan(s, Opsi{})
		if h.Teks != s || h.Diubah || len(h.Temuan) != 0 {
			t.Errorf("teks sah berubah:\n  masuk : %q\n  keluar: %q\n  temuan: %+v", s, h.Teks, h.Temuan)
		}
	}
}

// Nomor pelanggan itu sendiri BOLEH disebut (mis. konfirmasi), dalam format apa pun.
func TestNomorMilikSendiriBoleh(t *testing.T) {
	o := Opsi{NomorBoleh: []string{"628111222333"}}
	for _, s := range []string{"nomor Anda 628111222333", "nomor Anda 08111222333", "nomor Anda +62 811-1222-333"} {
		if h := SaringPelanggan(s, o); h.Teks != s {
			t.Errorf("nomor sendiri tak boleh disunting: %q -> %q", s, h.Teks)
		}
	}
	// nomor LAIN tetap disunting walau ada nomor sendiri yang diizinkan
	h := SaringPelanggan("nomor Anda 628111222333, nomor lain 628999000111", o)
	if strings.Contains(h.Teks, "628999000111") || !strings.Contains(h.Teks, "628111222333") {
		t.Errorf("hasil: %q", h.Teks)
	}
}

func TestNamaStafDisunting(t *testing.T) {
	h := SaringPelanggan("Sudah saya teruskan ke Budi Santoso dan Sari ya.", Opsi{NamaStaf: []string{"Budi Santoso", "Sari"}})
	if strings.Contains(h.Teks, "Budi") || strings.Contains(h.Teks, "Sari") {
		t.Errorf("nama staf bocor: %q", h.Teks)
	}
	if !strings.Contains(h.Teks, "tim kami") {
		t.Errorf("harus diganti 'tim kami': %q", h.Teks)
	}
	// nama pendek (<3) diabaikan agar tidak merusak kata lain
	if h := SaringPelanggan("di sana ada", Opsi{NamaStaf: []string{"di"}}); h.Teks != "di sana ada" {
		t.Errorf("nama terlalu pendek harus diabaikan: %q", h.Teks)
	}
	// kata utuh saja: "Sarimin" bukan "Sari"
	if h := SaringPelanggan("Pak Sarimin datang", Opsi{NamaStaf: []string{"Sari"}}); h.Teks != "Pak Sarimin datang" {
		t.Errorf("harus cocok kata utuh: %q", h.Teks)
	}
}

// Penyaring menyunting, TIDAK mengosongkan: balasan tetap ada dan bermakna.
func TestTidakPernahKosong(t *testing.T) {
	for _, s := range []string{"628111222333", "172.16.0.10", "billing.get_customer", "api_key: abcdefgh12345678"} {
		if h := SaringPelanggan(s, Opsi{}); strings.TrimSpace(h.Teks) == "" {
			t.Errorf("penyaring mengosongkan balasan %q", s)
		}
	}
}

func TestTemuanTanpaIsiAsli(t *testing.T) {
	h := SaringPelanggan("hubungi 628111222333 di 172.16.0.10", Opsi{})
	for _, f := range h.Temuan {
		if strings.ContainsAny(f.Kategori, "0123456789.") && f.Kategori != KatNomor {
			t.Errorf("kategori memuat data: %q", f.Kategori)
		}
	}
	if len(h.Temuan) != 2 {
		t.Errorf("temuan = %+v, mau 2 kategori", h.Temuan)
	}
}

func TestPemotonganDiBatasKata(t *testing.T) {
	teks := strings.Repeat("kata ", 200)
	h := SaringPelanggan(teks, Opsi{MaxLen: 100})
	if len(h.Teks) > 110 || !h.Dipotong || !strings.HasSuffix(h.Teks, "…") {
		t.Errorf("len=%d dipotong=%v akhir=%q", len(h.Teks), h.Dipotong, h.Teks[len(h.Teks)-5:])
	}
	if strings.Contains(h.Teks, "kat…") {
		t.Errorf("tidak boleh memotong di tengah kata: %q", h.Teks[len(h.Teks)-12:])
	}
}

func TestIdempoten(t *testing.T) {
	in := "hubungi 628111222333 di 172.16.0.10 pakai token: abcdefgh12345678 via billing.get_customer"
	a := SaringPelanggan(in, Opsi{})
	b := SaringPelanggan(a.Teks, Opsi{})
	if a.Teks != b.Teks || b.Diubah {
		t.Errorf("penyaringan kedua mengubah lagi:\n1: %q\n2: %q", a.Teks, b.Teks)
	}
}

func TestMasukanAnehTidakPanik(t *testing.T) {
	for _, s := range []string{"", " ", "\x00\x00", "🙂🙂🙂", strings.Repeat("a", 100000), strings.Repeat("1", 5000), "@@@@", "|||"} {
		_ = SaringPelanggan(s, Opsi{NamaStaf: []string{"", " ", "x"}, NomorBoleh: []string{"", "abc"}})
	}
}
