package health

import "testing"

func TestSetAndGet(t *testing.T) {
	r := New()
	r.Set("llm", StatusOnline, "", 120)
	r.Set("whatsapp", StatusOnline, "connected", 45)

	if d, ok := r.Get("llm"); !ok || d.Status != StatusOnline {
		t.Errorf("Get(llm) = %+v ok=%v", d, ok)
	}
	if d, ok := r.Get("whatsapp"); !ok || d.Detail != "connected" {
		t.Errorf("Get(whatsapp) = %+v ok=%v", d, ok)
	}
	if _, ok := r.Get("tidak-ada"); ok {
		t.Error("Get dependensi tak dikenal harus ok=false")
	}
}

func TestOverallAllOnline(t *testing.T) {
	r := New()
	r.Set("a", StatusOnline, "", 0)
	r.Set("b", StatusOnline, "", 0)
	if r.Overall() != StatusOnline {
		t.Errorf("Overall = %s, mau ONLINE", r.Overall())
	}
}

func TestOverallOfflineDegrades(t *testing.T) {
	r := New()
	r.Set("a", StatusOnline, "", 0)
	r.Set("b", StatusOffline, "connection refused", 0)
	if r.Overall() != StatusDegraded {
		t.Errorf("Overall = %s, mau DEGRADED (sistem tetap jalan walau ada yang mati)", r.Overall())
	}
}

func TestOverallDegraded(t *testing.T) {
	r := New()
	r.Set("a", StatusDegraded, "slow", 0)
	if r.Overall() != StatusDegraded {
		t.Errorf("Overall = %s, mau DEGRADED", r.Overall())
	}
}

func TestOverallEmptyUnknown(t *testing.T) {
	r := New()
	if r.Overall() != StatusUnknown {
		t.Errorf("Overall kosong = %s, mau UNKNOWN", r.Overall())
	}
}

func TestOverallUnknown(t *testing.T) {
	r := New()
	r.Set("a", StatusUnknown, "belum dicek", 0)
	if r.Overall() != StatusUnknown {
		t.Errorf("Overall = %s, mau UNKNOWN", r.Overall())
	}
}

func TestUpsertKeepsOrder(t *testing.T) {
	r := New()
	r.Set("b", StatusOnline, "", 0)
	r.Set("a", StatusOnline, "", 0)
	r.Set("b", StatusOffline, "berubah", 0) // update, bukan duplikat

	all := r.All()
	if len(all) != 2 {
		t.Fatalf("All = %d, mau 2 (update bukan duplikat)", len(all))
	}
	if all[0].Name != "b" || all[1].Name != "a" {
		t.Errorf("urutan pendaftaran harus dipertahankan: %s, %s", all[0].Name, all[1].Name)
	}
	// Update status tetap di slot yang sama.
	if d, _ := r.Get("b"); d.Status != StatusOffline || d.Detail != "berubah" {
		t.Errorf("update tidak diterapkan: %+v", d)
	}
}

func TestSummaryShape(t *testing.T) {
	r := New()
	r.Set("llm", StatusOnline, "", 100)
	s := r.Summary()
	if s["healthy"] != true {
		t.Errorf("healthy = %v, mau true", s["healthy"])
	}
	deps, ok := s["deps"].([]map[string]any)
	if !ok || len(deps) != 1 {
		t.Fatalf("deps = %T len=%d, mau []map len=1", s["deps"], len(deps))
	}
}
