package directory

import "testing"

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in   string
		kind CommandKind
		tgt  string
	}{
		{"cek user pppor pelanggan-uji", CmdPPPoE, "pelanggan-uji"},
		{"Cek user PPPoE JttJavic", CmdPPPoE, "JttJavic"},
		{"cek traffict", CmdTraffic, ""},
		{"cek traffic pelanggan-uji", CmdTraffic, "pelanggan-uji"},
		{"traffic pelanggan-uji", CmdTraffic, "pelanggan-uji"},
		{"cek pppoe budi.santoso@isp", CmdPPPoE, "budi.santoso@isp"},
		{"status akun pelanggan-uji dong", CmdPPPoE, "pelanggan-uji"},
		// bukan perintah staf -> alur biasa
		{"internet lemot", CmdNone, ""},
		{"halo", CmdNone, ""},
		{"cek koneksi 8.8.8.8", CmdNone, ""},
		{"", CmdNone, ""},
		{"tolong cek kenapa internet saya mati dari kemarin sore sampai sekarang belum nyala", CmdNone, ""},
	}
	for _, c := range cases {
		got := ParseCommand(c.in)
		if got.Kind != c.kind || got.Target != c.tgt {
			t.Errorf("ParseCommand(%q) = %+v, mau {%q %q}", c.in, got, c.kind, c.tgt)
		}
	}
}
