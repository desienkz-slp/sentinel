package session

import (
	"strings"
	"testing"
	"time"
)

// Kunci sesi harus menyatukan semua bentuk identitas nomor yang sama.
func TestKey(t *testing.T) {
	cases := []struct{ in, mau string }{
		{"628123456789@s.whatsapp.net", "628123456789"},
		{"628123456789@c.us", "628123456789"},
		{"628123456789:79@s.whatsapp.net", "628123456789"}, // device id dibuang
		{"+62 812-3456-789", "628123456789"},
		{"628123456789", "628123456789"},
		{"120363000000000000@g.us", "120363000000000000"},
		{"", "unknown"},
	}
	for _, c := range cases {
		if got := Key(c.in); got != c.mau {
			t.Errorf("Key(%q) = %q, mau %q", c.in, got, c.mau)
		}
	}
}

// INTI ISOLASI: percakapan dua nomor berbeda tidak boleh saling bocor.
func TestIsolasiPerNomor(t *testing.T) {
	s := New(DefaultConfig())
	a := Key("628111111111@s.whatsapp.net")
	b := Key("628222222222@s.whatsapp.net")

	s.Append(a, "user", "internet saya lambat", "COMPLAINT", "", "8.8.8.8")
	s.Append(a, "assistant", "VERDICT: DEGRADASI", "COMPLAINT", "DEGRADASI", "8.8.8.8")
	s.Append(b, "user", "wifi mati", "COMPLAINT", "", "192.168.1.1")

	histA := s.History(a)
	histB := s.History(b)

	if len(histA) != 2 {
		t.Fatalf("riwayat A = %d, mau 2", len(histA))
	}
	if len(histB) != 1 {
		t.Fatalf("riwayat B = %d, mau 1", len(histB))
	}
	// Pesan B tidak boleh muncul di riwayat A.
	for _, m := range histA {
		if strings.Contains(m.Content, "wifi mati") {
			t.Error("konteks nomor B bocor ke nomor A")
		}
	}
	// Target terakhir juga harus terpisah.
	if got := s.LastTarget(a); got != "8.8.8.8" {
		t.Errorf("LastTarget(A) = %q, mau 8.8.8.8", got)
	}
	if got := s.LastTarget(b); got != "192.168.1.1" {
		t.Errorf("LastTarget(B) = %q, mau 192.168.1.1", got)
	}
}

// Cache juga harus terpisah per nomor: pertanyaan sama dari nomor berbeda
// tidak boleh berbagi jawaban.
func TestCacheTerpisahPerNomor(t *testing.T) {
	s := New(DefaultConfig())
	q := "internet lambat"

	kA := CacheKey("628111111111@s.whatsapp.net", q)
	kB := CacheKey("628222222222@s.whatsapp.net", q)
	if kA == kB {
		t.Fatal("kunci cache nomor berbeda harus berbeda")
	}

	s.CacheStore(kA, "jawaban untuk A", "DEGRADASI", 80, "llm", 30000, []string{"ping"})

	if _, _, _, _, _, _, ok := s.CacheLookup(kA); !ok {
		t.Error("cache A harus ditemukan")
	}
	if _, _, _, _, _, _, ok := s.CacheLookup(kB); ok {
		t.Error("cache B tidak boleh menemukan entri milik A")
	}
}

// Normalisasi pertanyaan membuat variasi penulisan berbagi entri cache yang sama.
func TestCacheNormalisasiPertanyaan(t *testing.T) {
	s := New(DefaultConfig())
	id := "628111111111@s.whatsapp.net"

	k1 := CacheKey(id, "Internet Lambat!")
	k2 := CacheKey(id, "internet   lambat")
	if k1 != k2 {
		t.Error("variasi penulisan harus menghasilkan kunci cache sama")
	}

	s.CacheStore(k1, "jawaban", "DEGRADASI", 80, "llm", 1000, nil)
	if _, _, _, _, _, _, ok := s.CacheLookup(k2); !ok {
		t.Error("pertanyaan setara harus mengenai cache yang sama")
	}
}

func TestCacheKedaluwarsa(t *testing.T) {
	s := New(Config{CacheTTL: 1 * time.Millisecond, MaxTurns: 5, TTL: time.Hour, MaxConvs: 10})
	k := CacheKey("628111", "cek")
	s.CacheStore(k, "jawaban lama", "SEHAT", 90, "llm", 500, nil)
	time.Sleep(10 * time.Millisecond)
	if _, _, _, _, _, _, ok := s.CacheLookup(k); ok {
		t.Error("cache harus kedaluwarsa")
	}
}

func TestCacheClearAll(t *testing.T) {
	s := New(DefaultConfig())
	k := CacheKey("628111", "cek")
	s.CacheStore(k, "jawaban", "SEHAT", 90, "llm", 500, nil)
	s.CacheClearAll()
	if _, _, _, _, _, _, ok := s.CacheLookup(k); ok {
		t.Error("cache harus kosong setelah CacheClearAll (dipakai saat model berganti)")
	}
}

// Riwayat yang sudah melewati TTL dianggap percakapan baru.
func TestRiwayatKedaluwarsa(t *testing.T) {
	s := New(Config{TTL: 1 * time.Millisecond, MaxTurns: 5, CacheTTL: time.Hour, MaxConvs: 10})
	id := "628111"
	s.Append(id, "user", "halo", "CHAT", "", "")
	time.Sleep(10 * time.Millisecond)
	if h := s.History(id); len(h) != 0 {
		t.Errorf("riwayat kedaluwarsa harus kosong, dapat %d", len(h))
	}
	if got := s.LastTarget(id); got != "" {
		t.Errorf("LastTarget kedaluwarsa harus kosong, dapat %q", got)
	}
}

// Riwayat dibatasi jumlah giliran supaya memori tidak tumbuh tanpa batas.
func TestRiwayatDibatasi(t *testing.T) {
	s := New(Config{MaxTurns: 3, TTL: time.Hour, CacheTTL: time.Hour, MaxConvs: 10})
	id := "628111"
	for i := 0; i < 50; i++ {
		s.Append(id, "user", "pesan", "COMPLAINT", "", "")
		s.Append(id, "assistant", "balasan", "COMPLAINT", "", "")
	}
	if h := s.History(id); len(h) > 6 {
		t.Errorf("riwayat %d melebihi batas 6 (3 giliran x 2)", len(h))
	}
}

func TestStats(t *testing.T) {
	s := New(DefaultConfig())
	s.Append("628111", "user", "halo", "CHAT", "", "")
	s.CacheStore(CacheKey("628111", "halo"), "Halo!", "", 0, "llm", 100, nil)

	st := s.Stats()
	if st["percakapan_aktif"].(int) != 1 {
		t.Errorf("percakapan_aktif = %v", st["percakapan_aktif"])
	}
	if st["cache_entri"].(int) != 1 {
		t.Errorf("cache_entri = %v", st["cache_entri"])
	}
}

func TestConversations(t *testing.T) {
	s := New(DefaultConfig())
	s.Append("628111", "user", "internet lambat", "COMPLAINT", "", "8.8.8.8")
	s.Append("628111", "assistant", "VERDICT: DEGRADASI", "COMPLAINT", "DEGRADASI", "8.8.8.8")

	list := s.Conversations()
	if len(list) != 1 {
		t.Fatalf("percakapan = %d, mau 1", len(list))
	}
	c := list[0]
	if c["nomor"] != "628111" {
		t.Errorf("nomor = %v", c["nomor"])
	}
	if c["giliran"].(int) != 2 {
		t.Errorf("giliran = %v", c["giliran"])
	}
	if c["target"] != "8.8.8.8" {
		t.Errorf("target = %v", c["target"])
	}
}
