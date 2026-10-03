package directory

import "testing"

func TestParseListQuery(t *testing.T) {
	cases := []struct {
		in     string
		filter string
		search string
		ok     bool
	}{
		{"daftar pelanggan", "", "", true},
		{"terkait dafta pelanggan dan history bayar?", "", "", true}, // pesan produksi persis (typo "dafta")
		{"terkait daftar pelanggan dan history bayar?", "", "", true},
		{"berapa jumlah pelanggan", "", "", true},
		{"pelanggan isolir", "isolir", "", true},
		{"daftar pelanggan isolir", "isolir", "", true},
		{"cek pelanggan terisolir", "isolir", "", true},
		{"jumlah pelanggan aktif", "aktif", "", true},
		{"pelanggan nonaktif", "nonaktif", "", true},
		{"cari budi", "cari", "budi", true},
		{"cari pelanggan budi", "cari", "budi", true},
		// bukan daftar -> jalur lain
		{"cek billing pelanggan-uji", "", "", false},
		{"cek user pppoe pelanggan-uji", "", "", false},
		{"cek tagihan pelanggan-uji", "", "", false},
		{"internet lemot", "", "", false},
		{"halo", "", "", false},
		{"", "", "", false},
		{"cari", "", "", false},
		{"pelanggan saya mengeluh internet lambat sejak pagi tadi di area barat", "", "", false},
	}
	for _, c := range cases {
		q, ok := ParseListQuery(c.in)
		if ok != c.ok || (ok && (q.Filter != c.filter || q.Search != c.search)) {
			t.Errorf("ParseListQuery(%q) = %+v,%v ; mau {%q %q},%v", c.in, q, ok, c.filter, c.search, c.ok)
		}
	}
}

func TestHasHistoryWords(t *testing.T) {
	for in, want := range map[string]bool{
		"history bayar": true, "riwayat pembayaran budi": true, "cek payment": true,
		"cek billing budi": false, "halo": false,
	} {
		if got := HasHistoryWords(in); got != want {
			t.Errorf("HasHistoryWords(%q) = %v, mau %v", in, got, want)
		}
	}
}
