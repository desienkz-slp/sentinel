// observability_wire.go — FASE 5: endpoint observabilitas lifecycle case + KPI
// yang dihitung dari case engine (bukan hardcode).
//
//   - /api/cases  : lifecycle tiap case aktif (state, events, escalation target,
//     verification result) — data case engine + audit append-only.
//   - /api/kpi    : KPI §40 diagregasi dari tracker case (BuildCaseKPI).
//
// Keduanya additive dan read-only: tidak mengubah alur diagnosis.
package main

import (
	"net/http"
	"sort"
	"strings"

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/observability"
)

// caseEventView adalah proyeksi satu transisi lifecycle untuk JSON.
type caseEventView struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Actor  string `json:"actor"`
	Reason string `json:"reason"`
	At     string `json:"at"`
}

// verificationView adalah proyeksi bukti verifikasi untuk JSON.
type verificationView struct {
	Passed  bool   `json:"passed"`
	Source  string `json:"source"`
	Summary string `json:"summary"`
	At      string `json:"at"`
}

// escalationTargetView adalah target handoff manusia untuk satu case, diambil
// dari audit append-only (event escalation_handoff) — bukan direka.
type escalationTargetView struct {
	Role   string `json:"role"`
	Phone  string `json:"phone"`
	Domain string `json:"domain"`
	Sent   bool   `json:"sent"`
}

// caseLifecycleView adalah proyeksi penuh lifecycle satu case untuk dashboard.
type caseLifecycleView struct {
	CaseID        string                `json:"case_id"`
	Identity      string                `json:"identity"`
	Channel       string                `json:"channel"`
	State         string                `json:"state"`
	Version       int64                 `json:"version"`
	CreatedAt     string                `json:"created_at"`
	UpdatedAt     string                `json:"updated_at"`
	Events        []caseEventView       `json:"events"`
	Verifications []verificationView    `json:"verifications"`
	Escalation    *escalationTargetView `json:"escalation,omitempty"`
}

// buildCaseLifecycle memproyeksikan case + audit menjadi lifecycle view.
// Escalation target dibaca dari audit (event escalation_handoff untuk case_id).
func buildCaseLifecycle(c *caseengine.Case, aud *audit.Store) caseLifecycleView {
	v := caseLifecycleView{}
	if c == nil {
		return v
	}
	v.CaseID = string(c.ID)
	v.Identity = c.Identity
	v.Channel = c.Channel
	v.State = string(c.State())
	v.Version = c.Version()
	v.CreatedAt = c.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")
	v.UpdatedAt = c.UpdatedAt.UTC().Format("2006-01-02T15:04:05Z")

	v.Events = make([]caseEventView, 0, len(c.Events()))
	for _, ev := range c.Events() {
		v.Events = append(v.Events, caseEventView{
			From:   string(ev.From),
			To:     string(ev.To),
			Actor:  ev.Actor,
			Reason: ev.Reason,
			At:     ev.At.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	v.Verifications = make([]verificationView, 0, len(c.Verifications()))
	for _, vf := range c.Verifications() {
		v.Verifications = append(v.Verifications, verificationView{
			Passed:  vf.Passed,
			Source:  vf.Source,
			Summary: vf.Summary,
			At:      vf.At.UTC().Format("2006-01-02T15:04:05Z"),
		})
	}

	if aud != nil {
		v.Escalation = escalationTargetFromAudit(aud, v.CaseID)
	}
	return v
}

// escalationTargetFromAudit mencari handoff terakhir untuk case_id di audit dan
// mengembalikan target role+nomor+domain+sent. Mengembalikan nil bila belum ada
// handoff (case belum dieskalasi).
func escalationTargetFromAudit(aud *audit.Store, caseID string) *escalationTargetView {
	entries := aud.ByCase(caseID)
	for _, e := range entries {
		if e.EventType != "escalation_handoff" {
			continue
		}
		view := &escalationTargetView{Sent: e.ExecutionStatus == "SENT"}
		if after, ok := e.After.(map[string]any); ok {
			if s, ok := after["target_role"].(string); ok {
				view.Role = s
			}
			if s, ok := after["target_phone"].(string); ok {
				view.Phone = s
			}
			if s, ok := after["domain"].(string); ok {
				view.Domain = s
			}
			if s, ok := after["sent"].(bool); ok {
				view.Sent = s
			}
		}
		return view
	}
	return nil
}

// caseKPIReport menggabungkan KPI case engine dengan ringkasan legacy report
// (untuk kompatibilitas dashboard lama yang membaca cases_processed/verified_resolved).
type caseKPIReport struct {
	observability.CaseKPI
	// Alias kompatibilitas mundur dengan skema /api/kpi lama.
	CasesProcessed int64 `json:"cases_processed"`
}

// handleCases menampilkan lifecycle semua case aktif (terbaru dulu) + ringkasan.
func (s *Server) handleCases(w http.ResponseWriter, r *http.Request) {
	cases := s.engineCases()
	views := make([]caseLifecycleView, 0, len(cases))
	for _, c := range cases {
		views = append(views, buildCaseLifecycle(c, s.aud))
	}
	// Filter opsional ?state=RESOLVED untuk dashboard.
	if q := strings.ToUpper(strings.TrimSpace(r.URL.Query().Get("state"))); q != "" {
		filtered := make([]caseLifecycleView, 0, len(views))
		for _, v := range views {
			if v.State == q {
				filtered = append(filtered, v)
			}
		}
		views = filtered
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count": len(views),
		"cases": views,
	})
}

// handleCaseKPI menampilkan KPI §40 dari case engine (BuildCaseKPI).
func (s *Server) handleCaseKPI(w http.ResponseWriter, r *http.Request) {
	kpi := observability.BuildCaseKPI(s.engineCases())
	writeJSON(w, http.StatusOK, caseKPIReport{
		CaseKPI:        kpi,
		CasesProcessed: kpi.TotalCases,
	})
}

// engineCases mengembalikan semua case dari CaseWire (bila tersedia), terurut
// terbaru dulu. Bila wiring mati (nil), kembalikan slice kosong.
func (s *Server) engineCases() []*caseengine.Case {
	if s.engine == nil || s.engine.Case == nil {
		return nil
	}
	cases := s.engine.Case.AllCases()
	if cases == nil {
		return nil
	}
	// AllCases sudah terurut terbaru dulu; jaga determinisme tambahan.
	sort.SliceStable(cases, func(i, j int) bool {
		return cases[i].CreatedAt.After(cases[j].CreatedAt)
	})
	return cases
}
