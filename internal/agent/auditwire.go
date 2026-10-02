// auditwire.go — FASE 5: jejak audit lengkap per §25 untuk transisi state case.
//
// CaseWire menggerakkan state machine di banyak titik (begin/onDiagnosis/
// onResult/onActionStarted/onActionCompleted/onPolicyBlock). Sebagian jalur
// sudah mencatat keputusan ke audit (policy_decision, verification,
// escalation_handoff, diagnosis), tetapi transisi state case itu sendiri belum
// punya satu baris audit yang konsisten.
//
// auditTransition mencatat SETIAP transisi yang benar-benar terjadi (berhasil)
// ke audit append-only dengan actor, from/to, dan reason — sesuai §25 "audit
// trail" dan §39 "case lifecycle". Ini additive: bila store audit nil, tidak
// terjadi apa-apa.
package agent

import (
	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
)

// auditTransition mencatat satu transisi state case yang berhasil ke audit
// append-only. Before/After membawa state lama/baru (dan reason + event count)
// supaya timeline case bisa direkonstruksi dari /api/audit.
func auditTransition(aud *audit.Store, c *caseengine.Case, from, to caseengine.State, actor, reason string) {
	if aud == nil || c == nil {
		return
	}
	aud.Record(audit.Entry{
		EventType:       "case_transition",
		Actor:           actor,
		CaseID:          string(c.ID),
		EntityType:      "case",
		EntityID:        string(c.ID),
		ExecutionStatus: "TRANSITIONED",
		Before:          map[string]any{"state": string(from), "version": c.Version() - 1},
		After:           map[string]any{"state": string(to), "version": c.Version(), "events": len(c.Events())},
		Note:            reason,
	})
}

// recordTransition memanggil c.Transition lalu mencatatnya ke audit bila sukses.
// Ia adalah satu-satunya jalur transisi case yang dianjurkan di wiring — kode
// lama yang memanggil c.Transition langsung tetap jalan, tapi transisinya tidak
// masuk audit sampai dialihkan ke sini.
func recordTransition(aud *audit.Store, c *caseengine.Case, next caseengine.State, actor, reason string) error {
	from := caseengine.State("")
	if c != nil {
		from = c.State()
	}
	if err := c.Transition(next, actor, reason); err != nil {
		return err
	}
	auditTransition(aud, c, from, next, actor, reason)
	return nil
}
