package handoff

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func contoh() Handoff {
	return Handoff{CaseID: "CASE-20261003-ABC234", Customer: "628111000111", Domain: "network",
		Complaint: "internet mati", Evidence: map[string]string{"radius": "sesi tidak ada"}}
}

func TestOpenMelengkapiBuktiDenganUnknown(t *testing.T) {
	l := New("")
	ok, _ := l.Open(contoh())
	if !ok {
		t.Fatal("open pertama harus true")
	}
	h, _ := l.Get("case-20261003-abc234") // id tak peka huruf
	for _, s := range Systems {
		if h.Evidence[s] == "" {
			t.Errorf("bukti %s kosong", s)
		}
	}
	if h.Evidence["radius"] != "sesi tidak ada" || h.Evidence["billing"] != Unknown {
		t.Errorf("bukti = %+v", h.Evidence)
	}
	if h.Status != StatusOpen {
		t.Errorf("status = %s", h.Status)
	}
}

func TestOpenIdempoten(t *testing.T) {
	l := New("")
	l.Open(contoh())
	h2 := contoh()
	h2.Complaint = "DITIMPA"
	if ok, _ := l.Open(h2); ok {
		t.Fatal("open kedua tidak boleh menimpa")
	}
	if h, _ := l.Get("CASE-20261003-ABC234"); h.Complaint != "internet mati" {
		t.Fatalf("keluhan tertimpa: %q", h.Complaint)
	}
}

func TestTutupButuhPesan(t *testing.T) {
	l := New("")
	l.Open(contoh())
	if _, _, err := l.Apply("CASE-20261003-ABC234", "noc", StatusClosed, "  "); !errors.Is(err, ErrNeedMessage) {
		t.Fatalf("err = %v", err)
	}
	if h, _ := l.Get("CASE-20261003-ABC234"); h.Status != StatusOpen {
		t.Fatalf("kasus berubah walau ditolak: %s", h.Status)
	}
}

func TestAlurPembaruanDanTutup(t *testing.T) {
	l := New("")
	l.Open(contoh())
	if _, i, err := l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, "Sedang kami periksa."); err != nil || i != 0 {
		t.Fatalf("i=%d err=%v", i, err)
	}
	h, i, err := l.Apply("CASE-20261003-ABC234", "noc", StatusClosed, "Sudah normal kembali.")
	if err != nil || i != 1 || h.Status != StatusClosed || len(h.Updates) != 2 {
		t.Fatalf("h=%+v i=%d err=%v", h, i, err)
	}
	if _, _, err := l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, "lagi"); !errors.Is(err, ErrClosed) {
		t.Fatalf("kasus tertutup harus menolak: %v", err)
	}
	if len(l.ListOpen()) != 0 {
		t.Fatal("kasus tertutup tak boleh di daftar terbuka")
	}
}

func TestDuplikatDitolak(t *testing.T) {
	l := New("")
	l.Open(contoh())
	l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, "Sedang kami periksa.")
	if _, _, err := l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, "Sedang kami periksa."); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("err = %v", err)
	}
	// pesan berbeda -> boleh
	if _, _, err := l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, "Teknisi menuju lokasi."); err != nil {
		t.Fatal(err)
	}
}

func TestStatusDanKasusTakDikenal(t *testing.T) {
	l := New("")
	l.Open(contoh())
	if _, _, err := l.Apply("CASE-20261003-ABC234", "noc", Status("ngawur"), "x"); !errors.Is(err, ErrBadStatus) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := l.Apply("CASE-TIDAK-ADA", "noc", StatusClosed, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestPersistDanMuatUlang(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "handoffs.json")
	l := New(p)
	l.Open(contoh())
	_, i, _ := l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, "Sedang kami periksa.")
	if err := l.MarkNotified("CASE-20261003-ABC234", i, ""); err != nil {
		t.Fatal(err)
	}
	l2 := New(p)
	h, ok := l2.Get("CASE-20261003-ABC234")
	if !ok || h.Status != StatusInvestigate || len(h.Updates) != 1 || !h.Updates[0].Notified {
		t.Fatalf("muat ulang: ok=%v h=%+v", ok, h)
	}
}

func TestBerkasRusakTidakPanik(t *testing.T) {
	p := filepath.Join(t.TempDir(), "h.json")
	l := New(p)
	l.Open(contoh())
	// rusakkan berkas
	if err := writeRaw(p, "{bukan json"); err != nil {
		t.Fatal(err)
	}
	if got := New(p); len(got.ListOpen()) != 0 {
		t.Fatal("berkas rusak harus menghasilkan ledger kosong")
	}
}

func TestSalinanTidakMengubahPenyimpanan(t *testing.T) {
	l := New("")
	l.Open(contoh())
	h, _ := l.Get("CASE-20261003-ABC234")
	h.Evidence["radius"] = "DIUBAH"
	h.Status = StatusClosed
	if g, _ := l.Get("CASE-20261003-ABC234"); g.Evidence["radius"] == "DIUBAH" || g.Status == StatusClosed {
		t.Fatal("Get harus mengembalikan salinan")
	}
}

func TestApplyBersamaan(t *testing.T) {
	l := New(filepath.Join(t.TempDir(), "h.json"))
	l.Open(contoh())
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.Apply("CASE-20261003-ABC234", "noc", StatusInvestigate, string(rune('a'+i%26))+string(rune('a'+i/26)))
		}(i)
	}
	wg.Wait()
	h, _ := l.Get("CASE-20261003-ABC234")
	if len(h.Updates) == 0 || len(h.Updates) > 30 {
		t.Fatalf("updates = %d", len(h.Updates))
	}
}

func TestNilAman(t *testing.T) {
	var l *Ledger
	if _, ok := l.Get("x"); ok {
		t.Fatal()
	}
	if _, _, err := l.Apply("x", "a", StatusClosed, "m"); err == nil {
		t.Fatal()
	}
	if ok, _ := l.Open(contoh()); ok {
		t.Fatal()
	}
	_ = l.ListOpen()
	_ = l.MarkNotified("x", 0, "")
}

func writeRaw(p, s string) error { return os.WriteFile(p, []byte(s), 0o600) }
