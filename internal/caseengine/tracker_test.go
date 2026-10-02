package caseengine

import (
	"strings"
	"testing"
	"time"
)

func TestTrackerCreatesCasePerIdentity(t *testing.T) {
	tr := NewTracker()
	tr.now = func() time.Time { return time.Date(2026, time.October, 1, 10, 0, 0, 0, time.UTC) }

	c := tr.Replace("628111222333@s.whatsapp.net", "whatsapp")
	if c == nil {
		t.Fatal("Replace harus mengembalikan case")
	}
	if string(c.Identity) != "628111222333" {
		t.Fatalf("identity ternormalisasi = %q, ingin 628111222333", c.Identity)
	}
	if c.State() != StateNew {
		t.Fatalf("state awal = %s, ingin NEW", c.State())
	}
	if got, ok := tr.Get("628111222333"); !ok || got != c {
		t.Fatal("Get tidak menemukan case yang sama untuk identity ternormalisasi")
	}
	if got, ok := tr.Get("628111222333@s.whatsapp.net"); !ok || got != c {
		t.Fatal("Get tidak menemukan case yang sama untuk identity mentah")
	}
}

func TestTrackerGetMissingIdentity(t *testing.T) {
	tr := NewTracker()
	if _, ok := tr.Get("628999000111"); ok {
		t.Fatal("Get untuk identity tak dikenal harus false")
	}
}

func TestTrackerReplaceResetsCase(t *testing.T) {
	tr := NewTracker()
	first := tr.Replace("628111222333", "whatsapp")
	if err := first.Transition(StateIdentifying, "system", "tahap identifikasi"); err != nil {
		t.Fatalf("transisi gagal: %v", err)
	}
	second := tr.Replace("628111222333", "whatsapp")
	if second == first {
		t.Fatal("Replace harus membuat case baru, bukan mengembalikan case lama")
	}
	if second.State() != StateNew {
		t.Fatalf("case baru harus NEW, dapat %s", second.State())
	}
	if tr.Count() != 1 {
		t.Fatalf("Count = %d, ingin 1 (satu identity satu case aktif)", tr.Count())
	}
}

func TestTrackerAllReturnsSnapshotNewestFirst(t *testing.T) {
	tr := NewTracker()
	base := time.Date(2026, time.October, 1, 10, 0, 0, 0, time.UTC)
	tr.now = func() time.Time { return base }
	c1 := tr.Replace("628111222333", "whatsapp")
	tr.now = func() time.Time { return base.Add(time.Hour) }
	c2 := tr.Replace("628444555666", "whatsapp")
	all := tr.All()
	if len(all) != 2 {
		t.Fatalf("All = %d case, ingin 2", len(all))
	}
	if all[0] != c2 || all[1] != c1 {
		t.Fatal("All harus terurut terbaru dulu (CreatedAt)")
	}
}

func TestNormalizeIdentityStripsSuffixes(t *testing.T) {
	cases := map[string]string{
		"628111222333@s.whatsapp.net": "628111222333",
		"628111222333@c.us":           "628111222333",
		"628111222333:79":             "628111222333",
		"+62 811-222-333":             "62811222333",
		"unknown@lid":                 "unknown",
	}
	for in, want := range cases {
		if got := normalizeIdentity(in); got != want {
			t.Errorf("normalizeIdentity(%q) = %q, ingin %q", in, got, want)
		}
	}
}

func TestTrackerNilSafe(t *testing.T) {
	var tr *Tracker
	if _, ok := tr.Get("x"); ok {
		t.Fatal("Get pada tracker nil harus false")
	}
	if tr.All() != nil {
		t.Fatal("All pada tracker nil harus nil")
	}
	c := tr.Replace("x", "whatsapp")
	if c == nil || strings.TrimSpace(string(c.Identity)) == "" {
		t.Fatal("Replace pada tracker nil harus tetap membuat case")
	}
}
