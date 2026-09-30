package learning

import (
	"testing"
)

// Signature adalah dasar seluruh pembelajaran — harus konsisten.
func TestSignatureOf(t *testing.T) {
	cases := []struct {
		msg string
		mau Signature
	}{
		{"internet saya lambat", SigLambat},
		{"lemot banget", SigLambat},
		{"ping tinggi dan packet loss", SigLambat},
		{"internet mati total", SigMatiTotal},
		{"tidak konek sama sekali", SigMatiTotal},
		{"tidak bisa buka google", SigDNS},
		{"dns error", SigDNS},
		{"login pppoe gagal terus", SigPPPoE},
		{"radius tidak merespons", SigPPPoE},
		{"wifi lemah di kamar", SigWifi},
		{"sinyal hilang", SigWifi},
		{"ont lampu los merah", SigPerangkat},
		{"kabel lan rusak", SigPerangkat},
		{"putus nyambung terus", SigPutusNyambung},
		{"koneksi kedip-kedip", SigPutusNyambung},
		{"halo", SigUmum},
		{"", SigUmum},
	}
	for _, c := range cases {
		if got := SignatureOf(c.msg); got != c.mau {
			t.Errorf("SignatureOf(%q) = %q, mau %q", c.msg, got, c.mau)
		}
	}
}

// Playbook bawaan dipakai saat data belum cukup.
func TestPlaybookBawaanSaatDataSedikit(t *testing.T) {
	s := New()
	got := s.Playbook(SigLambat)
	if len(got) == 0 {
		t.Fatal("playbook kosong saat belum ada data")
	}
	want := Bawaan(SigLambat)
	if got[0] != want[0] {
		t.Errorf("playbook[0] = %q, mau %q (bawaan)", got[0], want[0])
	}
}

// Setelah cukup data, playbook harus mengikuti probe yang terbukti berguna.
func TestPlaybookBelajarDariHasil(t *testing.T) {
	s := New()
	// dns selalu menghasilkan verdict pasti; interface tidak pernah dipakai lagi.
	for i := 0; i < 5; i++ {
		s.Record(SigDNS, "SEHAT", []string{"dns", "interface"}, map[string]int64{"dns": 50, "interface": 3000})
	}
	pb := s.Playbook(SigDNS)
	if len(pb) == 0 {
		t.Fatal("playbook kosong")
	}
	if pb[0] != "dns" {
		t.Errorf("playbook[0] = %q, mau 'dns' (paling berguna)", pb[0])
	}
}

// Verdict tidak pasti tidak dihitung sebagai 'berguna', dan tidak dihitung konklusif.
func TestVerdictTidakPastiTidakDihitungKonklusif(t *testing.T) {
	s := New()
	for i := 0; i < 5; i++ {
		s.Record(SigLambat, "TIDAK DIKETAHUI", []string{"ping"}, nil)
	}
	st := s.Stats()
	if st["konklusif"].(int) != 0 {
		t.Errorf("konklusif = %v, mau 0 (verdict tidak pasti)", st["konklusif"])
	}
	if v, _ := s.DominanVerdict(SigLambat); v != "" {
		t.Errorf("dominan verdict = %q, mau kosong (data belum konklusif)", v)
	}
}

func TestDominanVerdict(t *testing.T) {
	s := New()
	for i := 0; i < 3; i++ {
		s.Record(SigLambat, "DEGRADASI", []string{"ping"}, nil)
	}
	s.Record(SigLambat, "SEHAT", []string{"ping"}, nil)
	v, n := s.DominanVerdict(SigLambat)
	if v != "DEGRADASI" {
		t.Errorf("dominan = %q, mau DEGRADASI", v)
	}
	if n != 3 {
		t.Errorf("jumlah = %d, mau 3", n)
	}
}

// Statistik per signature harus terpisah.
func TestStatistikTerpisahPerSignature(t *testing.T) {
	s := New()
	s.Record(SigLambat, "DEGRADASI", []string{"ping"}, nil)
	s.Record(SigDNS, "SEHAT", []string{"dns"}, nil)
	s.Record(SigDNS, "SEHAT", []string{"dns"}, nil)

	semua := s.Semua()
	if len(semua) != 2 {
		t.Fatalf("signature = %d, mau 2", len(semua))
	}
	// Diurutkan dari yang paling banyak.
	if semua[0]["signature"] != SigDNS {
		t.Errorf("signature terbanyak = %v, mau DNS", semua[0]["signature"])
	}
	if semua[0]["total"].(int) != 2 {
		t.Errorf("total DNS = %v, mau 2", semua[0]["total"])
	}
}

func TestRataRataDurasi(t *testing.T) {
	s := New()
	s.Record(SigLambat, "SEHAT", []string{"ping"}, map[string]int64{"ping": 1000})
	s.Record(SigLambat, "SEHAT", []string{"ping"}, map[string]int64{"ping": 1000})
	semua := s.Semua()
	probes := semua[0]["probes"].([]map[string]any)
	if probes[0]["rata_ms"].(int64) != 1000 {
		t.Errorf("rata_ms = %v, mau 1000", probes[0]["rata_ms"])
	}
}

// Setiap signature punya playbook bawaan yang tidak kosong.
func TestBawaanSemuaSignature(t *testing.T) {
	sigs := []Signature{SigLambat, SigMatiTotal, SigDNS, SigPPPoE, SigWifi, SigPutusNyambung, SigPerangkat, SigUmum}
	for _, s := range sigs {
		if len(Bawaan(s)) == 0 {
			t.Errorf("playbook bawaan %q kosong", s)
		}
	}
}
