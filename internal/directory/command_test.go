package directory

import "testing"

func TestParseCommand(t *testing.T) {
	cases := []struct {
		in   string
		kind CommandKind
		tgt  string
	}{
		{"cek user pppor pelanggan-uji", CmdPPPoE, "pelanggan-uji"},
		{"Cek user PPPoE Pelanggan-Uji", CmdPPPoE, "Pelanggan-Uji"},
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

func TestParseCommandBilling(t *testing.T) {
	cases := []struct {
		in   string
		kind CommandKind
		tgt  string
	}{
		{"cek billing pelanggan-uji", CmdBilling, "pelanggan-uji"},
		{"tagihan pelanggan-uji", CmdBilling, "pelanggan-uji"},
		{"cek tunggakan budi", CmdBilling, "budi"},
		{"cek user pppoe pelanggan-uji", CmdPPPoE, "pelanggan-uji"},
		{"cek traffic pelanggan-uji", CmdTraffic, "pelanggan-uji"},
		{"tagihan saya bulan ini kok mahal sekali ya kenapa bisa begitu", CmdNone, ""},
	}
	for _, c := range cases {
		got := ParseCommand(c.in)
		if got.Kind != c.kind || got.Target != c.tgt {
			t.Errorf("ParseCommand(%q) = %+v, mau {%s %s}", c.in, got, c.kind, c.tgt)
		}
	}
}

func TestParseCommandRiwayat(t *testing.T) {
	for in, want := range map[string]Command{
		"riwayat pelanggan-uji":        {Kind: CmdBilling, Target: "pelanggan-uji"},
		"history bayar pelanggan-uji":  {Kind: CmdBilling, Target: "pelanggan-uji"},
		"cek pembayaran pelanggan-uji": {Kind: CmdBilling, Target: "pelanggan-uji"},
	} {
		if got := ParseCommand(in); got != want {
			t.Errorf("ParseCommand(%q) = %+v, mau %+v", in, got, want)
		}
	}
	// keluhan biasa tidak boleh terbaca perintah
	for _, in := range []string{"pembayaran saya sudah masuk tapi internet masih mati dari kemarin", "history chat kemarin"} {
		if got := ParseCommand(in); got.Kind != CmdNone {
			t.Errorf("ParseCommand(%q) = %+v, mau CmdNone", in, got)
		}
	}
}
