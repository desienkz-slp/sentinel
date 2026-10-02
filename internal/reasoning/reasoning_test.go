package reasoning

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"ainoc/internal/correlation"
)

type collectorStub struct {
	byDomain map[string]Evidence
}

func (s collectorStub) Collect(_ context.Context, input CaseInput, domain string) Evidence {
	if got := input.CaseID; got == "" {
		return Evidence{Domain: domain, State: correlation.StateUnknown, Error: "case_id kosong"}
	}
	if evidence, ok := s.byDomain[domain]; ok {
		return evidence
	}
	return Evidence{Domain: domain, State: correlation.StateUnknown, Error: "bukti belum tersedia"}
}

func TestEvaluateBuildsStructuredOutputFromCollectedEvidence(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	engine := New(collectorStub{byDomain: map[string]Evidence{
		"billing":  {Domain: "billing", State: correlation.StateActive, Source: "billing.get_customer", CollectedAt: now},
		"radius":   {Domain: "radius", State: correlation.StateAuthed, Source: "radius.get_session", CollectedAt: now},
		"mikrotik": {Domain: "mikrotik", State: correlation.StatePPPoEDown, Source: "mikrotik.get_pppoe_status", CollectedAt: now},
		"genieacs": {Domain: "genieacs", State: correlation.StateDeviceOn, Source: "genieacs.get_device", CollectedAt: now},
	}})

	output := engine.Evaluate(context.Background(), CaseInput{
		CaseID:   "CASE-20261002-ABCDEF",
		Intent:   "internet_down",
		Identity: "628111222333",
		Facts:    map[string]string{"scope": "all_devices", "condition": "unavailable"},
	})

	if output.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version = %q, mau %q", output.SchemaVersion, SchemaVersion)
	}
	if output.Conclusion.PrimaryArea != "mikrotik" {
		t.Fatalf("area utama = %q, mau mikrotik", output.Conclusion.PrimaryArea)
	}
	if len(output.Hypotheses) == 0 || output.Hypotheses[0].Status != HypothesisSupported {
		t.Fatalf("hipotesis utama harus SUPPORTED: %+v", output.Hypotheses)
	}
	if len(output.Diagnostics) != 4 {
		t.Fatalf("diagnostik = %d, mau 4", len(output.Diagnostics))
	}
	encoded, err := json.Marshal(output)
	if err != nil {
		t.Fatalf("structured output harus dapat di-JSON-kan: %v", err)
	}
	var schema map[string]any
	if err := json.Unmarshal(encoded, &schema); err != nil || schema["conclusion"] == nil || schema["evidence"] == nil {
		t.Fatalf("schema output tidak lengkap: %s, err=%v", encoded, err)
	}
}

func TestEvaluateKeepsUnavailableEvidenceUnknown(t *testing.T) {
	engine := New(collectorStub{byDomain: map[string]Evidence{
		"billing": {Domain: "billing", State: correlation.StateActive, Source: "billing.get_customer", CollectedAt: time.Now().UTC()},
	}})

	output := engine.Evaluate(context.Background(), CaseInput{CaseID: "CASE-20261002-ABCDEF", Intent: "internet_down"})

	for _, domain := range []string{"radius", "mikrotik", "genieacs"} {
		evidence := output.EvidenceFor(domain)
		if evidence.State != correlation.StateUnknown {
			t.Errorf("bukti %s = %s, mau UNKNOWN", domain, evidence.State)
		}
	}
	if output.Conclusion.Confidence >= 0.5 {
		t.Errorf("keyakinan dengan bukti tidak tersedia = %.2f, harus rendah", output.Conclusion.Confidence)
	}
	if output.Conclusion.Diagnosis != "bukti tidak cukup untuk diagnosis pasti" {
		t.Errorf("diagnosis = %q; jangan membuat diagnosis dari bukti UNKNOWN", output.Conclusion.Diagnosis)
	}
}

func TestEvaluateRejectsInvalidInputWithoutFabricatingEvidence(t *testing.T) {
	output := New(nil).Evaluate(context.Background(), CaseInput{Intent: "internet_down"})
	if output.InputValid {
		t.Fatal("input tanpa case_id harus tidak valid")
	}
	if len(output.ValidationErrors) == 0 {
		t.Fatal("kesalahan validasi harus terstruktur")
	}
	for _, evidence := range output.Evidence {
		if evidence.State != correlation.StateUnknown {
			t.Errorf("bukti invalid input harus UNKNOWN: %+v", evidence)
		}
	}
}

func TestEvaluateKeepsCrossDomainAndUnknownStateAsUnknown(t *testing.T) {
	engine := New(collectorStub{byDomain: map[string]Evidence{
		"billing":  {State: correlation.StateActive},
		"radius":   {State: correlation.StateActive}, // state billing tidak sah untuk radius.
		"mikrotik": {State: correlation.State("BUKAN_STATE")},
		"genieacs": {State: correlation.StateDeviceOn},
	}})

	output := engine.Evaluate(context.Background(), CaseInput{CaseID: "CASE-20261002-ABCDEF", Intent: "internet_down"})
	for _, domain := range []string{"radius", "mikrotik"} {
		if got := output.EvidenceFor(domain).State; got != correlation.StateUnknown {
			t.Errorf("bukti state tidak sah %s = %s, mau UNKNOWN", domain, got)
		}
		for _, diagnostic := range output.Diagnostics {
			if diagnostic.Domain == domain && diagnostic.Status != DiagnosticUnknown {
				t.Errorf("diagnostik %s = %s, mau UNKNOWN", domain, diagnostic.Status)
			}
		}
	}
}

func TestEvaluateDoesNotCollectForUnsupportedIntent(t *testing.T) {
	called := 0
	collector := collectorFunc(func(_ context.Context, _ CaseInput, domain string) Evidence {
		called++
		return Evidence{Domain: domain, State: correlation.StateActive}
	})

	output := New(collector).Evaluate(context.Background(), CaseInput{CaseID: "CASE-20261002-ABCDEF", Intent: "CHAT"})
	if output.InputValid {
		t.Fatal("intent non-diagnostik harus tidak valid")
	}
	if called != 0 {
		t.Fatalf("collector dipanggil %d kali untuk intent non-diagnostik", called)
	}
}

type collectorFunc func(context.Context, CaseInput, string) Evidence

func (f collectorFunc) Collect(ctx context.Context, input CaseInput, domain string) Evidence {
	return f(ctx, input, domain)
}
