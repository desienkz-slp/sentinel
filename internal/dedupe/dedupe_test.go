package dedupe

import (
	"testing"
	"time"
)

func TestSeenOnceThenDuplicate(t *testing.T) {
	s := New(time.Minute, 100)
	fp := Fingerprint("whatsapp", "628111", "internet mati")
	if s.Seen(fp) {
		t.Error("pertama kali harus false (bukan duplikat)")
	}
	if !s.Seen(fp) {
		t.Error("kedua kali harus true (duplikat)")
	}
}

func TestDifferentEventsNotDuplicate(t *testing.T) {
	s := New(time.Minute, 100)
	a := Fingerprint("whatsapp", "628111", "internet mati")
	b := Fingerprint("whatsapp", "628222", "internet mati")
	if s.Seen(a) {
		t.Fatal("a pertama harus false")
	}
	if s.Seen(b) {
		t.Error("b berbeda, harus false")
	}
}

func TestTTLExpiry(t *testing.T) {
	s := New(20*time.Millisecond, 100)
	fp := Fingerprint("x")
	s.Seen(fp)
	if !s.Seen(fp) {
		t.Error("dalam TTL harus duplikat")
	}
	time.Sleep(30 * time.Millisecond)
	if s.Seen(fp) {
		t.Error("setelah TTL habis, harus dianggap event baru")
	}
}

func TestFingerprintDeterministicAndIgnoresEmpty(t *testing.T) {
	a := Fingerprint("whatsapp", "628111", "hello", "")
	b := Fingerprint("whatsapp", "628111", "hello")
	if a != b {
		t.Errorf("fingerprint harus sama meski ada field kosong: %s vs %s", a, b)
	}
	if a == "" {
		t.Error("fingerprint tidak boleh kosong")
	}
}

func TestCapEvictsOldest(t *testing.T) {
	s := New(time.Hour, 3)
	// Masukkan 5 fingerprint berbeda; kapasitas 3, jadi 2 paling lama dibuang.
	for i := 0; i < 5; i++ {
		s.Seen(Fingerprint("k", string(rune('a'+i))))
	}
	if s.Len() > 3 {
		t.Errorf("Len = %d, mau <= 3 (cap evict)", s.Len())
	}
}

func TestReset(t *testing.T) {
	s := New(time.Minute, 100)
	s.Seen(Fingerprint("a"))
	s.Reset()
	if s.Len() != 0 {
		t.Errorf("Len setelah Reset = %d, mau 0", s.Len())
	}
}
