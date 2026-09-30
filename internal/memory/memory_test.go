package memory

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractFactsLokasi(t *testing.T) {
	facts := ExtractFacts("pak, saya di Jl. Melati no 5, internet lambat")
	var ada bool
	for _, f := range facts {
		if f.Kind == "lokasi" {
			ada = true
			if f.Text == "" {
				t.Error("fakta lokasi kosong")
			}
		}
	}
	if !ada {
		t.Errorf("lokasi tidak terdeteksi dari: %v", facts)
	}
}

func TestExtractFactsPerangkat(t *testing.T) {
	facts := ExtractFacts("ONT saya lampu LOS merah, sudah restart router")
	kinds := map[string]bool{}
	for _, f := range facts {
		kinds[f.Kind] = true
	}
	if !kinds["perangkat"] {
		t.Errorf("perangkat tidak terdeteksi: %v", facts)
	}
}

// Pesan tanpa fakta tidak boleh menghasilkan fakta palsu.
func TestExtractFactsKosong(t *testing.T) {
	if f := ExtractFacts("halo"); len(f) != 0 {
		t.Errorf("pesan sapaan menghasilkan fakta: %v", f)
	}
	if f := ExtractFacts(""); len(f) != 0 {
		t.Errorf("pesan kosong menghasilkan fakta: %v", f)
	}
}

// Fakta yang disebut berulang hanya disimpan sekali, tapi hitungannya naik.
func TestAddFactMenggabungkanDuplikat(t *testing.T) {
	s := New(Config{})
	s.AddFact("628111", "lokasi", "Jl. Melati")
	s.AddFact("628111", "lokasi", "Jl. Melati")
	s.AddFact("628111", "lokasi", "jl. melati") // beda kapitalisasi

	facts := s.Facts("628111")
	if len(facts) != 1 {
		t.Fatalf("fakta = %d, mau 1 (duplikat harus digabung): %v", len(facts), facts)
	}
	if facts[0].Hits != 3 {
		t.Errorf("hits = %d, mau 3", facts[0].Hits)
	}
}

// Fakta terpisah per nomor — inti isolasi.
func TestFactsTerpisahPerNomor(t *testing.T) {
	s := New(Config{})
	s.AddFact("628111", "lokasi", "Jl. Melati")
	s.AddFact("628222", "lokasi", "Jl. Kenanga")

	a := s.Facts("628111")
	b := s.Facts("628222")
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("fakta A=%d B=%d, mau 1 masing-masing", len(a), len(b))
	}
	if a[0].Text == b[0].Text {
		t.Error("fakta nomor A dan B bercampur")
	}
}

func TestIncidentDanRecurrence(t *testing.T) {
	s := New(Config{})
	base := time.Now()
	for i := 0; i < 3; i++ {
		s.AddIncident("628111", Incident{
			ID: "INC-" + string(rune('a'+i)), At: base.Add(-time.Duration(i) * time.Hour),
			Signature: "LAMBAT", Verdict: "DEGRADASI", Target: "8.8.8.8",
		})
	}
	// Insiden kategori lain tidak boleh ikut terhitung.
	s.AddIncident("628111", Incident{ID: "INC-x", At: base, Signature: "DNS", Verdict: "SEHAT"})

	n, verdict, _ := s.Recurrence("628111", "LAMBAT", 7*24*time.Hour)
	if n != 3 {
		t.Errorf("pengulangan LAMBAT = %d, mau 3", n)
	}
	if verdict != "DEGRADASI" {
		t.Errorf("verdict terakhir = %q, mau DEGRADASI", verdict)
	}
	if n2, _, _ := s.Recurrence("628111", "DNS", 7*24*time.Hour); n2 != 1 {
		t.Errorf("pengulangan DNS = %d, mau 1", n2)
	}
	// Nomor lain tidak punya riwayat.
	if n3, _, _ := s.Recurrence("628999", "LAMBAT", 7*24*time.Hour); n3 != 0 {
		t.Errorf("nomor tak dikenal punya riwayat: %d", n3)
	}
}

func TestRecentIncidentsDibatasiWaktu(t *testing.T) {
	s := New(Config{})
	s.AddIncident("628111", Incident{ID: "lama", At: time.Now().Add(-30 * 24 * time.Hour), Signature: "LAMBAT"})
	s.AddIncident("628111", Incident{ID: "baru", At: time.Now(), Signature: "LAMBAT"})

	rec := s.RecentIncidents("628111", 7*24*time.Hour)
	if len(rec) != 1 {
		t.Fatalf("insiden 7 hari = %d, mau 1", len(rec))
	}
	if rec[0].ID != "baru" {
		t.Errorf("insiden terbaru = %q, mau 'baru'", rec[0].ID)
	}
}

// Memory harus bertahan setelah restart (persistensi ke disk).
func TestPersistensiKeDisk(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")

	s1 := New(Config{Path: path})
	s1.AddFact("628111", "lokasi", "Jl. Melati")
	s1.AddIncident("628111", Incident{ID: "INC-1", At: time.Now(), Signature: "LAMBAT", Verdict: "DEGRADASI"})
	if err := s1.Save(); err != nil {
		t.Fatalf("Save gagal: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file memory tidak dibuat: %v", err)
	}

	// Simulasi restart: instance baru membaca dari disk.
	s2 := New(Config{Path: path})
	facts := s2.Facts("628111")
	if len(facts) != 1 || facts[0].Text != "Jl. Melati" {
		t.Errorf("fakta tidak bertahan setelah restart: %v", facts)
	}
	inc := s2.RecentIncidents("628111", 24*time.Hour)
	if len(inc) != 1 || inc[0].Verdict != "DEGRADASI" {
		t.Errorf("insiden tidak bertahan setelah restart: %v", inc)
	}
}

// SaveIfDirty hanya menulis bila ada perubahan.
func TestSaveIfDirty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.json")
	s := New(Config{Path: path})

	if err := s.SaveIfDirty(); err != nil {
		t.Fatalf("SaveIfDirty tanpa perubahan error: %v", err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Error("file dibuat padahal belum ada perubahan")
	}

	s.AddFact("628111", "lokasi", "Jl. Melati")
	if err := s.SaveIfDirty(); err != nil {
		t.Fatalf("SaveIfDirty gagal: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("file tidak dibuat padahal ada perubahan")
	}
}

// Memory tanpa path (memori saja) tidak boleh error.
func TestTanpaPath(t *testing.T) {
	s := New(Config{})
	s.AddFact("628111", "lokasi", "Jl. Melati")
	if err := s.Save(); err != nil {
		t.Errorf("Save tanpa path harus no-op, dapat: %v", err)
	}
	if len(s.Facts("628111")) != 1 {
		t.Error("fakta hilang")
	}
}

func TestStats(t *testing.T) {
	s := New(Config{})
	s.AddFact("628111", "lokasi", "Jl. Melati")
	s.AddIncident("628111", Incident{ID: "x", At: time.Now(), Signature: "LAMBAT"})
	st := s.Stats()
	if st["pengirim_dikenal"].(int) != 1 {
		t.Errorf("pengirim_dikenal = %v", st["pengirim_dikenal"])
	}
	if st["total_insiden"].(int) != 1 {
		t.Errorf("total_insiden = %v", st["total_insiden"])
	}
}
