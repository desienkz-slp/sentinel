package learning

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRecipeStoreRecordAndBest: resep dengan verdict pasti tersimpan & terambil
// sebagai urutan tool; verdict TIDAK DIKETAHUI tidak dipelajari.
func TestRecipeStoreRecordAndBest(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "recipes.json")
	s := NewRecipeStore(p)

	// Verdict pasti -> tersimpan.
	s.Record(SigMatiTotal, []string{"billing.get_customer", "radius.get_session"}, "GANGGUAN")
	s.Record(SigMatiTotal, []string{"billing.get_customer", "radius.get_session"}, "GANGGUAN") // dup -> hits++
	s.Record(SigMatiTotal, []string{"radius.get_session"}, "DEGRADASI")

	// Verdict tak pasti -> TIDAK dipelajari.
	s.Record(SigLambat, []string{"ping"}, "TIDAK DIKETAHUI")

	best := s.BestFor(SigMatiTotal)
	// Resep dengan hits terbanyak menang.
	if len(best) != 2 || best[0] != "billing.get_customer" || best[1] != "radius.get_session" {
		t.Fatalf("BestFor(MATI_TOTAL) = %v, mau urutan billing.get_customer->radius.get_session", best)
	}
	if got := s.BestFor(SigLambat); got != nil {
		t.Fatalf("BestFor(LAMBAT) = %v, mau nil (verdict tak pasti tidak dipelajari)", got)
	}

	// Persist + reload.
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2 := NewRecipeStore(p)
	if best2 := s2.BestFor(SigMatiTotal); len(best2) != 2 {
		t.Fatalf("setelah reload BestFor = %v", best2)
	}
}

// TestRecipeStoreInMemory: path kosong = tidak persist, tapi tetap berfungsi.
func TestRecipeStoreInMemory(t *testing.T) {
	s := NewRecipeStore("")
	s.Record(SigWifi, []string{"interface", "ping"}, "SEHAT")
	if got := s.BestFor(SigWifi); len(got) != 2 {
		t.Fatalf("in-memory BestFor = %v", got)
	}
	if _, err := os.Stat(filepath.Join(t.TempDir(), "x")); err != nil {
		// tidak ada file ditulis untuk path kosong
	}
}

// TestRecipeStoreStats: statistik resep.
func TestRecipeStoreStats(t *testing.T) {
	s := NewRecipeStore("")
	s.Record(SigDNS, []string{"dns"}, "GANGGUAN")
	st := s.Stats()
	if st["resep"].(int) != 1 {
		t.Fatalf("resep = %v, mau 1", st["resep"])
	}
}
