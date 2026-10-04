package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"ainoc/internal/incident"
	"ainoc/internal/observability"
)

func TestAlertsProjectsPersistedFlapFindingAsReadOnlyWarning(t *testing.T) {
	_, h, store, _ := monitoringRouteServer(t)
	detected := time.Date(2026, time.October, 4, 11, 0, 0, 0, time.UTC)
	if err := store.RecordUplinkFlapState(incident.UplinkFlapState{PriorFindings: []incident.UplinkFlapFinding{{
		UplinkID: "core-a:xe-0/0/1", DetectedAt: detected, Transitions: 3,
	}}}); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(http.MethodGet, "/api/alerts"))
	if w.Code != http.StatusOK {
		t.Fatalf("alerts status = %d, want 200", w.Code)
	}
	var alerts []observability.Alert
	if err := json.NewDecoder(w.Body).Decode(&alerts); err != nil {
		t.Fatalf("decode alerts: %v", err)
	}
	for _, alert := range alerts {
		if alert.Severity == "warning" && alert.Source == "monitoring" && alert.Subject == "core-a:xe-0/0/1" {
			return
		}
	}
	t.Fatalf("missing persisted flap warning in %#v", alerts)
}
