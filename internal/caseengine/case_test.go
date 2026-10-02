package caseengine

import (
	"regexp"
	"testing"
	"time"
)

func TestNewCreatesRequiredCaseIDAndInitialState(t *testing.T) {
	now := time.Date(2026, time.October, 1, 10, 30, 0, 0, time.UTC)
	c := New("whatsapp", "628111222333", now)

	if !regexp.MustCompile(`^CASE-20261001-[A-Z0-9]{6}$`).MatchString(string(c.ID)) {
		t.Fatalf("case ID %q tidak mengikuti format master spec", c.ID)
	}
	if c.State() != StateNew {
		t.Fatalf("state awal = %s, ingin NEW", c.State())
	}
	if c.Version() != 1 {
		t.Fatalf("version awal = %d, ingin 1", c.Version())
	}
}

func TestTransitionRejectsIllegalStateChange(t *testing.T) {
	c := New("whatsapp", "628111222333", time.Now())
	if err := c.Transition(StateResolved, "system", "tanpa diagnosis"); err == nil {
		t.Fatal("NEW -> RESOLVED harus ditolak")
	}
	if c.State() != StateNew || c.Version() != 1 {
		t.Fatalf("case berubah setelah transisi ilegal: state=%s version=%d", c.State(), c.Version())
	}
}

func TestResolvedRequiresRecordedVerification(t *testing.T) {
	c := New("whatsapp", "628111222333", time.Now())
	for _, next := range []State{StateIdentifying, StateConversation, StateReadyForDiagnosis, StateReasoning, StateInvestigation, StateVerifying} {
		if err := c.Transition(next, "system", "uji lifecycle"); err != nil {
			t.Fatalf("transisi ke %s gagal: %v", next, err)
		}
	}
	if err := c.Transition(StateResolved, "system", "belum diverifikasi"); err == nil {
		t.Fatal("VERIFYING -> RESOLVED tanpa verification harus ditolak")
	}
	if err := c.RecordVerification(Verification{Passed: true, Source: "radius", Summary: "PPPoE aktif"}); err != nil {
		t.Fatalf("verification pada state VERIFYING ditolak: %v", err)
	}
	if err := c.Transition(StateResolved, "system", "layanan terverifikasi"); err != nil {
		t.Fatalf("VERIFYING -> RESOLVED setelah verification gagal: %v", err)
	}
}

func TestEscalationPathNeedsHumanHandlingBeforeResolution(t *testing.T) {
	c := New("whatsapp", "628111222333", time.Now())
	for _, next := range []State{StateIdentifying, StateConversation, StateReadyForDiagnosis, StateReasoning, StateInvestigation, StateEscalation, StateHumanHandling, StateVerifying} {
		if err := c.Transition(next, "system", "uji escalation"); err != nil {
			t.Fatalf("transisi ke %s gagal: %v", next, err)
		}
	}
	if err := c.RecordVerification(Verification{Passed: true, Source: "human", Summary: "NOC Senior mengonfirmasi pulih"}); err != nil {
		t.Fatalf("verification hasil handoff ditolak: %v", err)
	}
	if err := c.Transition(StateResolved, "human:noc-1", "hasil handoff terverifikasi"); err != nil {
		t.Fatalf("jalur escalation -> resolved gagal: %v", err)
	}
}

func TestRecordVerificationRejectsOutsideVerifying(t *testing.T) {
	c := New("whatsapp", "628111222333", time.Now())
	if err := c.RecordVerification(Verification{Passed: true, Source: "radius"}); err == nil {
		t.Fatal("verification sebelum state VERIFYING harus ditolak")
	}
}
