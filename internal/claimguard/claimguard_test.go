package claimguard

import "testing"

func TestKlaimTanpaToolDiganti(t *testing.T) {
	klaim := []string{
		"Billing user budi tertunggak 2 bulan, total Rp 300.000.",
		"Ada 12 pelanggan yang sedang offline di area A.",
		"Sesi radius budi masih aktif.",
		"Router 172.16.0.1 normal.",
		"PPPoE pelanggan itu terputus sejak jam 10.",
		"Latensi 45 ms ke upstream.",
		"Koneksi internetnya lancar, redaman -22 dBm.",
		"Modem pelanggan sedang offline.",
	}
	for _, k := range klaim {
		out, replaced := Guard(k, false)
		if !replaced || out != Notice {
			t.Errorf("harus diganti: %q", k)
		}
	}
}

func TestDenganToolDibiarkan(t *testing.T) {
	for _, k := range []string{"Ada 12 pelanggan offline.", "Router normal."} {
		if out, replaced := Guard(k, true); replaced || out != k {
			t.Errorf("dengan tool tidak boleh diubah: %q", k)
		}
	}
}

// Obrolan biasa tanpa klaim sistem tidak boleh terganggu.
func TestObrolanTidakDiganggu(t *testing.T) {
	aman := []string{
		"Bisa, Mas. Mau saya cek pelanggan yang mana?",
		"Anda adalah Fahri, Super Admin.",
		"Pelanggan mana yang dicek? Kirim username PPPoE.",
		"Siap membantu jika ada yang perlu dicek.",
		"Belum diperiksa, silakan sebutkan pelanggannya.",
		"Halo, ada yang bisa dibantu?",
		"",
	}
	for _, k := range aman {
		if out, replaced := Guard(k, false); replaced || out != k {
			t.Errorf("tidak boleh diganti: %q", k)
		}
	}
}

func TestAlasanTidakMengutipIsi(t *testing.T) {
	r := Detect("Router 172.16.0.1 normal, 30 ms.")
	if !r.Flagged {
		t.Fatal("harus ditandai")
	}
	for _, s := range r.Reasons {
		if s == "" || len(s) > 20 {
			t.Errorf("alasan harus label pendek: %q", s)
		}
	}
}

func TestDeterministik(t *testing.T) {
	for i := 0; i < 50; i++ {
		if a, _ := Guard("Ada 3 pelanggan offline.", false); a != Notice {
			t.Fatal("tidak deterministik")
		}
	}
}
