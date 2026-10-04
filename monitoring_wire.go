package main

import (
	"fmt"
	"net/http"
	"time"

	"ainoc/internal/incident"
	"ainoc/internal/observability"
)

type massIncidentView struct {
	incident.MassIncidentParentSnapshot
	LatestRecovery *recoveryEvaluationView `json:"latest_recovery,omitempty"`
}

type recoveryEvaluationView struct {
	EvaluatedAt time.Time              `json:"evaluated_at"`
	Provenance  string                 `json:"provenance"`
	Result      massRecoveryResultView `json:"result"`
}

type massRecoveryResultView struct {
	SnapshotCustomers     int  `json:"snapshot_customers"`
	RecoveredCustomers    int  `json:"recovered_customers"`
	ParentClosureEligible bool `json:"parent_closure_eligible"`
	Indeterminate         bool `json:"indeterminate"`
}

func projectRecoveryEvaluation(evaluation incident.RecoveryEvaluation) recoveryEvaluationView {
	return recoveryEvaluationView{
		EvaluatedAt: evaluation.EvaluatedAt,
		Provenance:  evaluation.Provenance,
		Result: massRecoveryResultView{
			SnapshotCustomers:     evaluation.Result.SnapshotCustomers,
			RecoveredCustomers:    evaluation.Result.RecoveredCustomers,
			ParentClosureEligible: evaluation.Result.ParentClosureEligible,
			Indeterminate:         evaluation.Result.Indeterminate,
		},
	}
}

// handleMassIncidents projects only durable monitoring evidence. It never
// evaluates recovery, opens/closes a parent, or writes to the store.
func (s *Server) handleMassIncidents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "gunakan GET"})
		return
	}
	views := make([]massIncidentView, 0)
	if s.monitor != nil {
		for _, snapshot := range s.monitor.ParentSnapshots() {
			view := massIncidentView{MassIncidentParentSnapshot: snapshot}
			if latest, ok := s.monitor.LatestRecoveryEvaluation(snapshot.ParentID); ok {
				projection := projectRecoveryEvaluation(latest)
				view.LatestRecovery = &projection
			}
			views = append(views, view)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"count": len(views), "incidents": views})
}

// monitoringAlerts projects durable flap findings only. It does not call an
// adapter, re-evaluate evidence, acknowledge alerts, or persist state.
func (s *Server) monitoringAlerts() []observability.Alert {
	alerts := make([]observability.Alert, 0)
	if s.monitor == nil {
		return alerts
	}
	for _, finding := range s.monitor.UplinkFlapState().PriorFindings {
		alerts = append(alerts, observability.Alert{
			Severity: "warning",
			Source:   "monitoring",
			Subject:  finding.UplinkID,
			Message:  fmt.Sprintf("uplink flap: %d transitions detected at %s", finding.Transitions, finding.DetectedAt.UTC().Format(time.RFC3339)),
		})
	}
	return alerts
}
