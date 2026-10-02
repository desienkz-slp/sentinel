package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/config"
)

// makeCase membangun case dengan jalur lifecycle tertentu dan memasukkannya ke
// tracker. Kembali (tracker case, case ID).
func makeCase(t *testing.T, w *agent.CaseWire, identity string, states ...caseengine.State) *caseengine.Case {
	t.Helper()
	c := caseengine.New("whatsapp", identity, time.Now().UTC())
	for _, s := range states {
		if err := c.Transition(s, "test", "uji lifecycle"); err != nil {
			t.Fatalf("transisi ke %s gagal: %v", s, err)
		}
	}
	w.Tracker().Put(c)
	return c
}

func TestCaseLifecycleDanKPIEndpoints(t *testing.T) {
	store := audit.New("", 200)
	casew := agent.NewCaseWireWithAudit(store)

	// Satu case ber-eskalasi + satu case aktif.
	esc := makeCase(t, casew, "628111222333",
		caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateReasoning,
		caseengine.StateInvestigation, caseengine.StateEscalation)
	_ = esc
	_ = makeCase(t, casew, "628555666777",
		caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateReasoning,
		caseengine.StateInvestigation)

	eng := &agent.Engine{}
	eng.Case = casew

	s := &Server{
		cfg:    &config.Config{Addr: "127.0.0.1:8090", OperatorToken: "operator-secret"},
		engine: eng,
		aud:    store,
	}
	h := s.routes()

	// /api/cases harus mengembalikan lifecycle case.
	r := httptest.NewRequest(http.MethodGet, "http://noc.local/api/cases", nil)
	r.Header.Set("Authorization", "Bearer operator-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/cases status=%d, ingin 200", w.Code)
	}
	var body struct {
		Count int `json:"count"`
		Cases []struct {
			CaseID     string `json:"case_id"`
			State      string `json:"state"`
			Events     []any  `json:"events"`
			Escalation *struct {
				Role string `json:"role"`
			} `json:"escalation"`
		} `json:"cases"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode /api/cases: %v", err)
	}
	if body.Count != 2 {
		t.Fatalf("count = %d, ingin 2", body.Count)
	}
	for _, c := range body.Cases {
		if c.CaseID == "" || c.State == "" {
			t.Fatalf("case lifecycle tidak lengkap: %#v", c)
		}
		if len(c.Events) == 0 {
			t.Fatalf("case %s harus punya events lifecycle", c.CaseID)
		}
	}

	// /api/kpi harus dihitung dari case engine (total_cases = 2).
	r = httptest.NewRequest(http.MethodGet, "http://noc.local/api/kpi", nil)
	r.Header.Set("Authorization", "Bearer operator-secret")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("/api/kpi status=%d, ingin 200", w.Code)
	}
	var kpi struct {
		TotalCases  int64 `json:"total_cases"`
		Escalated   int64 `json:"escalated"`
		ActiveCases int64 `json:"active_cases"`
	}
	if err := json.NewDecoder(w.Body).Decode(&kpi); err != nil {
		t.Fatalf("decode /api/kpi: %v", err)
	}
	if kpi.TotalCases != 2 {
		t.Fatalf("total_cases = %d, ingin 2 (dari case engine)", kpi.TotalCases)
	}
	if kpi.Escalated != 1 {
		t.Fatalf("escalated = %d, ingin 1", kpi.Escalated)
	}
	if kpi.ActiveCases != 1 {
		t.Fatalf("active_cases = %d, ingin 1", kpi.ActiveCases)
	}
}

func TestEscalationTargetFromAudit(t *testing.T) {
	store := audit.New("", 200)
	store.Record(audit.Entry{
		EventType:       "escalation_handoff",
		Actor:           "agent",
		CaseID:          "CASE-20261003-TEST01",
		ExecutionStatus: "SENT",
		After: map[string]any{
			"target_role":  "noc_senior",
			"target_phone": "628111222333",
			"domain":       "network",
			"sent":         true,
		},
	})

	target := escalationTargetFromAudit(store, "CASE-20261003-TEST01")
	if target == nil {
		t.Fatal("escalation target tidak boleh nil")
	}
	if target.Role != "noc_senior" || target.Phone != "628111222333" || !target.Sent {
		t.Fatalf("target salah: %#v", target)
	}
}
