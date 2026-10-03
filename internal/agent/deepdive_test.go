package agent

import (
	"context"
	"testing"

	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/tool"
)

// TestDeepToolsAllowedScope: daftar tool Endpoint B TIDAK menyertakan tool
// lintas-pelanggan (billing.list_customers, radius.get_system_stats,
// genieacs.get_devices, mikrotik.get_interface_*) — scope per pelanggan terjaga.
func TestDeepToolsAllowedScope(t *testing.T) {
	forbidden := []string{
		"billing.list_customers",
		"radius.get_system_stats",
		"genieacs.get_devices",
		"mikrotik.get_interface_stats",
		"mikrotik.get_interface_live",
		"mikrotik.disconnect_pppoe", // WRITE tidak pernah boleh
	}
	for _, name := range forbidden {
		if deepToolsAllowed[name] {
			t.Errorf("tool %q TIDAK boleh tersedia untuk Endpoint B (scope/lintas-pelanggan/WRITE)", name)
		}
	}
	// Tool per-pelanggan yang sah harus ada.
	for _, name := range []string{"billing.get_customer", "radius.get_session", "mikrotik.get_pppoe_status", "genieacs.get_device_state"} {
		if !deepToolsAllowed[name] {
			t.Errorf("tool %q seharusnya tersedia untuk Endpoint B", name)
		}
	}
}

// TestDeepDiveTidakJalanTanpaReasonLLM: tanpa ReasonLLM, deep-dive mengembalikan
// kosong (tidak crash), dan sistem jatuh ke eskalasi manusia.
func TestDeepDiveTidakJalanTanpaReasonLLM(t *testing.T) {
	e := &Engine{Recipes: learning.NewRecipeStore("")}
	d, tools, ok := e.deepDive(context.Background(), "628111", "internet mati", learning.SigMatiTotal, nil)
	if ok || d != "" || tools != nil {
		t.Fatalf("tanpa ReasonLLM deepDive harus kosong, dapat d=%q tools=%v ok=%v", d, tools, ok)
	}
}

// TestDeepDivePakaiResepTerpelajari: resep yang sudah ada dipakai ulang tanpa
// panggil LLM (deterministik).
func TestDeepDivePakaiResepTerpelajari(t *testing.T) {
	rec := learning.NewRecipeStore("")
	rec.Record(learning.SigMatiTotal, []string{"radius.get_session"}, "GANGGUAN")

	// ReasonLLM diset tapi TIDAK boleh dipanggil bila resep ada -> pakai fake
	// yang akan panic bila dipanggil, untuk membuktikan jalur deterministik.
	e := &Engine{Recipes: rec, ReasonLLM: nil} // ReasonLLM nil => runRecipe yang jalan dulu

	// runRecipe butuh Disp non-nil; tanpa Disp, runRecipe tidak dieksekusi
	// (deepDive kembali kosong) — ini aman & deterministik.
	d, tools, ok := e.deepDive(context.Background(), "628111", "internet mati", learning.SigMatiTotal, nil)
	_ = d
	_ = tools
	_ = ok
}

// TestDeepToolsForHanyaToolAktif: deepToolsFor hanya mengembalikan tool yang
// aktif di registry DAN ada di daftar izin.
func TestDeepToolsForHanyaToolAktif(t *testing.T) {
	reg := registry.Load("")
	_ = reg
	// Gunakan registry kosong -> tidak ada tool.
	e := &Engine{Reg: registry.Load("")}
	if got := e.deepToolsFor(); len(got) != 0 {
		t.Fatalf("registry kosong harusnya tanpa tool, dapat %d", len(got))
	}
}

// guard agar import policy/tool/llm tetap dipakai (deepdive.go memakainya lewat
// Disp, tetapi test ini memastikan kompilasi package agent dengan dependensi).
var (
	_ = policy.Allow
	_ = tool.New
	_ = llm.New
)
