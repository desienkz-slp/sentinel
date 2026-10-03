package directory

import (
	"reflect"
	"testing"
)

func TestParseStatusQuestion(t *testing.T) {
	cases := []struct {
		in      string
		domains []string
		all     bool
	}{
		{"sudah bisa terhubung ke billing?", []string{"billing"}, false},
		{"sudab bisa baca billing?", []string{"billing"}, false},
		{"billing konek?", []string{"billing"}, false},
		{"cek koneksi billing", []string{"billing"}, false},
		{"radius aktif gak", []string{"radius"}, false},
		{"mikrotik sudah terhubung?", []string{"mikrotik"}, false},
		{"billing dan radius ok?", []string{"billing", "radius"}, false},
		{"semua integrasi normal?", nil, true},
		// bukan pertanyaan status -> alur lain
		{"cek billing pelanggan-uji", nil, false},
		{"cek tagihan pelanggan-uji", nil, false},
		{"cek user pppoe pelanggan-uji", nil, false},
		{"internet lemot", nil, false},
		{"halo", nil, false},
		{"", nil, false},
		{"billing saya kok mahal banget ya bulan ini padahal pemakaian biasa saja dan normal", nil, false},
	}
	for _, c := range cases {
		d, all := ParseStatusQuestion(c.in)
		if !reflect.DeepEqual(d, c.domains) || all != c.all {
			t.Errorf("ParseStatusQuestion(%q) = %v,%v ; mau %v,%v", c.in, d, all, c.domains, c.all)
		}
	}
}
