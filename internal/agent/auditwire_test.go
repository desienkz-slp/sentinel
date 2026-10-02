// auditwire_test.go — FASE 5: audit mencatat transisi state case + KPI dihitung
// benar dari case tracker.
package agent

import (
	"testing"

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/standard"
)

func TestAuditMencatatTransisiStateCase(t *testing.T) {
	store := audit.New("", 200)
	w := NewCaseWireWithAudit(store)

	snap := w.begin("628111222333", "whatsapp")
	if snap.CaseID == "" {
		t.Fatal("case ID tidak boleh kosong")
	}

	// begin membuat case baru: NEW -> IDENTIFYING -> CONVERSATION = 2 transisi.
	entries := store.ByCase(snap.CaseID)
	transitions := 0
	for _, e := range entries {
		if e.EventType == "case_transition" {
			transitions++
			if e.Before == nil || e.After == nil {
				t.Fatalf("transisi audit harus membawa before/after: %#v", e)
			}
		}
	}
	if transitions != 2 {
		t.Fatalf("jumlah transisi audit = %d, ingin 2 (NEW->IDENTIFYING->CONVERSATION)", transitions)
	}
}

func TestAuditMencatatSeluruhLifecycleKeluhan(t *testing.T) {
	store := audit.New("", 200)
	w := NewCaseWireWithAudit(store)
	w.begin("628111222333", "whatsapp")
	w.onDiagnosis("628111222333", standard.IntentComplaint)
	w.onResult("628111222333", standard.IntentComplaint, "TIDAK DIKETAHUI", 30, true)

	c, _ := w.tracker.Get("628111222333")
	if c.State() != caseengine.StateEscalation {
		t.Fatalf("state akhir = %s, ingin ESCALATION", c.State())
	}

	// Semua transisi harus tercatat ke audit dan sesuai dengan events case.
	auditTransitions := 0
	for _, e := range store.ByCase(string(c.ID)) {
		if e.EventType == "case_transition" {
			auditTransitions++
		}
	}
	if auditTransitions != len(c.Events()) {
		t.Fatalf("transisi audit = %d, tapi events case = %d", auditTransitions, len(c.Events()))
	}
}

func TestKPIDihitungDariCaseTrackerBukanHardcode(t *testing.T) {
	store := audit.New("", 200)
	w := NewCaseWireWithAudit(store)

	// Case 1: keluhan dengan verdict tidak pasti -> ESCALATION.
	w.begin("628111222333", "whatsapp")
	w.onDiagnosis("628111222333", standard.IntentComplaint)
	w.onResult("628111222333", standard.IntentComplaint, "TIDAK DIKETAHUI", 30, true)

	// Case 2: keluhan terdiagnosis normal -> INVESTIGATION (aktif).
	w.begin("628555666777", "whatsapp")
	w.onDiagnosis("628555666777", standard.IntentComplaint)
	w.onResult("628555666777", standard.IntentComplaint, "GANGGUAN", 85, true)

	cases := w.AllCases()
	if len(cases) != 2 {
		t.Fatalf("jumlah case = %d, ingin 2", len(cases))
	}

	// KPI dihitung dari tracker (paket observability) — lihat casekpi_test.go
	// untuk asersi angka. Di sini cukup memastikan tracker mengembalikan case
	// yang benar untuk diagregasi.
	states := map[string]int{}
	for _, c := range cases {
		states[string(c.State())]++
	}
	if states["ESCALATION"] != 1 {
		t.Fatalf("case ber-state ESCALATION = %d, ingin 1", states["ESCALATION"])
	}
	if states["INVESTIGATION"] != 1 {
		t.Fatalf("case ber-state INVESTIGATION = %d, ingin 1", states["INVESTIGATION"])
	}
}
