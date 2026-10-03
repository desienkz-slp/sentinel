package directory

import "testing"

func TestParseHandoffCommand(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want HandoffCmd
	}{
		{"tutup CASE-20261003-ABC234 Sudah normal, silakan dicoba", true, HandoffCmd{"CASE-20261003-ABC234", "selesai", "Sudah normal, silakan dicoba"}},
		{"Tutup case-20261003-abc234 ok", true, HandoffCmd{"CASE-20261003-ABC234", "selesai", "ok"}},
		{"selesai CASE-20261003-ABC234", true, HandoffCmd{"CASE-20261003-ABC234", "selesai", ""}},
		{"update CASE-20261003-ABC234 lapangan Teknisi menuju lokasi", true, HandoffCmd{"CASE-20261003-ABC234", "butuh_lapangan", "Teknisi menuju lokasi"}},
		{"update CASE-20261003-ABC234 info Mohon kirim foto modem", true, HandoffCmd{"CASE-20261003-ABC234", "butuh_info", "Mohon kirim foto modem"}},
		{"update CASE-20261003-ABC234 Sedang kami periksa", true, HandoffCmd{"CASE-20261003-ABC234", "diselidiki", "Sedang kami periksa"}},
		{"update CASE-20261003-ABC234", true, HandoffCmd{"CASE-20261003-ABC234", "diselidiki", ""}},
		{"kabari CASE-20261003-ABC234 selidiki Masih dicek", true, HandoffCmd{"CASE-20261003-ABC234", "diselidiki", "Masih dicek"}},
		{"update CASE-20261003-ABC234 selesai Beres", true, HandoffCmd{"CASE-20261003-ABC234", "selesai", "Beres"}},
		// bukan perintah
		{"tutup", false, HandoffCmd{}},
		{"tutup pintu", false, HandoffCmd{}},
		{"tutup CASE-123 ok", false, HandoffCmd{}},
		{"CASE-20261003-ABC234 tutup", false, HandoffCmd{}},
		{"tolong tutup CASE-20261003-ABC234", false, HandoffCmd{}},
		{"update paket saya", false, HandoffCmd{}},
		{"cek billing pelanggan-uji", false, HandoffCmd{}},
		{"status CASE-20261003-ABC234", false, HandoffCmd{}},
		{"tutup CASE-20261003-ABC23 ok", false, HandoffCmd{}}, // 5 huruf
		{"", false, HandoffCmd{}},
	}
	for _, c := range cases {
		got, ok := ParseHandoffCommand(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%q -> (%+v,%v), mau (%+v,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}
