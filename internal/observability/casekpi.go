package observability

import (
	"time"

	"ainoc/internal/caseengine"
)

// CaseKPI adalah KPI §40 yang dihitung DARI case engine (tracker), bukan
// hardcode. Setiap angka diagregasi dari lifecycle case nyata: total case,
// resolved, escalated, failed, human handling, verified resolved, waktu
// resolusi rata-rata, dan rasio eskalasi/resolusi.
type CaseKPI struct {
	TotalCases        int64 `json:"total_cases"`
	ActiveCases       int64 `json:"active_cases"`
	Resolved          int64 `json:"resolved"`
	Escalated         int64 `json:"escalated"`
	Failed            int64 `json:"failed"`
	HumanHandling     int64 `json:"human_handling"`
	VerifiedResolved  int64 `json:"verified_resolved"`
	AvgResolutionMS   int64 `json:"avg_resolution_ms"`
	EscalationRatePct int64 `json:"escalation_rate_percent"`
	ResolutionRatePct int64 `json:"resolution_rate_percent"`
}

// terminalState melaporkan apakah state menandakan case tidak lagi dikerjakan
// AI (resolved / gagal / menunggu manusia).
func terminalState(s caseengine.State) bool {
	switch s {
	case caseengine.StateResolved, caseengine.StateFailed,
		caseengine.StateEscalation, caseengine.StateHumanHandling:
		return true
	}
	return false
}

// BuildCaseKPI menghitung KPI langsung dari case tracker. Kejadian yang pernah
// dilewati (eskalasi/gagal/resolved) dibaca dari riwayat event, bukan hanya
// state saat ini, supaya case yang sudah lanjut (mis. ESCALATION → HUMAN_HANDLING)
// tetap terhitung pernah dieskalasi.
func BuildCaseKPI(cases []*caseengine.Case) CaseKPI {
	kpi := CaseKPI{TotalCases: int64(len(cases))}
	if len(cases) == 0 {
		return kpi
	}

	var resolutionTotal time.Duration
	var resolvedCount int64
	for _, c := range cases {
		if c == nil {
			continue
		}

		everEscalated := false
		everFailed := false
		everResolved := false
		for _, ev := range c.Events() {
			switch ev.To {
			case caseengine.StateEscalation:
				everEscalated = true
			case caseengine.StateFailed:
				everFailed = true
			case caseengine.StateResolved:
				everResolved = true
			}
		}

		if !terminalState(c.State()) {
			kpi.ActiveCases++
		}
		if c.State() == caseengine.StateHumanHandling {
			kpi.HumanHandling++
		}
		if everEscalated {
			kpi.Escalated++
		}
		if everFailed {
			kpi.Failed++
		}
		if everResolved {
			kpi.Resolved++
			resolvedCount++
			resolutionTotal += c.UpdatedAt.Sub(c.CreatedAt)
			// Resolusi nyata hanya yang punya bukti verifikasi lulus (fase 4).
			for _, v := range c.Verifications() {
				if v.Passed {
					kpi.VerifiedResolved++
					break
				}
			}
		}
	}

	if resolvedCount > 0 {
		kpi.AvgResolutionMS = resolutionTotal.Milliseconds() / resolvedCount
	}
	if kpi.TotalCases > 0 {
		kpi.EscalationRatePct = (kpi.Escalated * 100) / kpi.TotalCases
		kpi.ResolutionRatePct = (kpi.Resolved * 100) / kpi.TotalCases
	}
	return kpi
}
