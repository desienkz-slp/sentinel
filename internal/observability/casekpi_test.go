package observability

import (
	"testing"
	"time"

	"ainoc/internal/caseengine"
)

// buildCase memudahkan membuat case dengan jalur lifecycle tertentu.
func buildCase(now time.Time, states ...caseengine.State) *caseengine.Case {
	c := caseengine.New("whatsapp", "628111222333", now)
	for _, s := range states {
		if err := c.Transition(s, "test", "uji"); err != nil {
			panic(err)
		}
	}
	return c
}

func TestBuildCaseKPIDariTracker(t *testing.T) {
	now := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)

	// Case resolved via verification.
	resolved := buildCase(now,
		caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateReasoning,
		caseengine.StateInvestigation, caseengine.StateActionProposed,
		caseengine.StatePolicyCheck, caseengine.StateExecuting,
		caseengine.StateVerifying)
	if err := resolved.RecordVerification(caseengine.Verification{Passed: true, Source: "radius", Summary: "PPPoE up"}); err != nil {
		t.Fatalf("verification: %v", err)
	}
	if err := resolved.Transition(caseengine.StateResolved, "verification", "lulus"); err != nil {
		t.Fatalf("resolved: %v", err)
	}

	// Case escalated (verdict tidak pasti).
	escalated := buildCase(now,
		caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateReasoning,
		caseengine.StateInvestigation, caseengine.StateEscalation)

	// Case aktif (masih investigation).
	active := buildCase(now,
		caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateReasoning,
		caseengine.StateInvestigation)

	kpi := BuildCaseKPI([]*caseengine.Case{resolved, escalated, active})

	if kpi.TotalCases != 3 {
		t.Fatalf("total = %d, ingin 3", kpi.TotalCases)
	}
	if kpi.Resolved != 1 {
		t.Fatalf("resolved = %d, ingin 1", kpi.Resolved)
	}
	if kpi.VerifiedResolved != 1 {
		t.Fatalf("verified_resolved = %d, ingin 1", kpi.VerifiedResolved)
	}
	if kpi.Escalated != 1 {
		t.Fatalf("escalated = %d, ingin 1", kpi.Escalated)
	}
	if kpi.ActiveCases != 1 {
		t.Fatalf("active = %d, ingin 1", kpi.ActiveCases)
	}
	if kpi.Failed != 0 {
		t.Fatalf("failed = %d, ingin 0", kpi.Failed)
	}
	if kpi.EscalationRatePct != 33 {
		t.Fatalf("escalation_rate = %d, ingin 33", kpi.EscalationRatePct)
	}
	if kpi.ResolutionRatePct != 33 {
		t.Fatalf("resolution_rate = %d, ingin 33", kpi.ResolutionRatePct)
	}
}

func TestBuildCaseKPIEscalationKeHumanHandlingTetapTerhitung(t *testing.T) {
	now := time.Date(2026, time.October, 3, 10, 0, 0, 0, time.UTC)

	// ESCALATION -> HUMAN_HANDLING: case yang sudah lanjut harus tetap terhitung
	// pernah dieskalasi (bukan hanya state saat ini).
	c := buildCase(now,
		caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateReasoning,
		caseengine.StateInvestigation, caseengine.StateEscalation,
		caseengine.StateHumanHandling)

	kpi := BuildCaseKPI([]*caseengine.Case{c})
	if kpi.Escalated != 1 {
		t.Fatalf("escalated = %d, ingin 1 (pernah lewat ESCALATION)", kpi.Escalated)
	}
	if kpi.HumanHandling != 1 {
		t.Fatalf("human_handling = %d, ingin 1", kpi.HumanHandling)
	}
	if kpi.ActiveCases != 0 {
		t.Fatalf("active = %d, ingin 0", kpi.ActiveCases)
	}
}

func TestBuildCaseKPIKosong(t *testing.T) {
	kpi := BuildCaseKPI(nil)
	if kpi.TotalCases != 0 || kpi.ActiveCases != 0 {
		t.Fatalf("KPI kosong harus nol semua: %#v", kpi)
	}
}
