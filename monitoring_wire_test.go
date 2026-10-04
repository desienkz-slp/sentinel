package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/incident"
)

func monitoringRouteServer(t *testing.T) (*Server, http.Handler, *incident.MonitoringStore, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "monitoring.json")
	store, err := incident.OpenMonitoringStore(path, 50)
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{
		cfg:     &config.Config{Addr: "127.0.0.1:8090", OperatorToken: "operator-secret"},
		inc:     incident.New("", 50),
		monitor: store,
	}
	return s, s.routes(), store, path
}

func authorizedRequest(method, target string) *http.Request {
	r := httptest.NewRequest(method, "http://noc.local"+target, nil)
	r.Header.Set("Authorization", "Bearer operator-secret")
	return r
}

func TestMassIncidentsReturnsEmptyReadOnlyListAndPreservesIncidentsRoute(t *testing.T) {
	s, h, _, _ := monitoringRouteServer(t)
	s.inc.Add(incident.Incident{Identity: "628111222333", Intent: "legacy"})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(http.MethodGet, "/api/incidents/mass"))
	if w.Code != http.StatusOK {
		t.Fatalf("mass status = %d, want 200", w.Code)
	}
	var mass struct {
		Count     int             `json:"count"`
		Incidents json.RawMessage `json:"incidents"`
	}
	if err := json.NewDecoder(w.Body).Decode(&mass); err != nil {
		t.Fatalf("decode mass incidents: %v", err)
	}
	if mass.Count != 0 || string(mass.Incidents) != "[]" {
		t.Fatalf("empty mass projection = count=%d incidents=%s, want 0 and []", mass.Count, mass.Incidents)
	}

	w = httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(http.MethodGet, "/api/incidents"))
	if w.Code != http.StatusOK {
		t.Fatalf("legacy incidents status = %d, want 200", w.Code)
	}
	var legacy []incident.Incident
	if err := json.NewDecoder(w.Body).Decode(&legacy); err != nil {
		t.Fatalf("decode legacy incidents: %v", err)
	}
	if len(legacy) != 1 || legacy[0].Identity != "628111222333" {
		t.Fatalf("legacy incidents changed: %#v", legacy)
	}
}

func TestMassIncidentsProjectsStoredParentAndLatestRecoveryWithoutMutation(t *testing.T) {
	_, h, store, path := monitoringRouteServer(t)
	opened := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	if err := store.SaveParentSnapshot(incident.MassIncidentParentSnapshot{
		ParentID: "MASS-42", NodeID: "OLT-01/PON-7", OpenedAt: opened,
		Topology: incident.Topology{OLT: "OLT-01", PON: "PON-7"}, CustomerIDs: []string{"customer-1", "customer-2"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendRecoveryEvaluation(incident.RecoveryEvaluation{
		ParentID: "MASS-42", EvaluatedAt: opened.Add(time.Minute), Provenance: "independent-re-read",
		Result: incident.MassRecoveryResult{ParentClosureEligible: true, RecoveredCustomers: 2, SnapshotCustomers: 2},
	}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(http.MethodGet, "/api/incidents/mass"))
	if w.Code != http.StatusOK {
		t.Fatalf("mass status = %d, want 200", w.Code)
	}
	var body struct {
		Count     int `json:"count"`
		Incidents []struct {
			ParentID       string `json:"parent_id"`
			NodeID         string `json:"node_id"`
			LatestRecovery *struct {
				Provenance string `json:"provenance"`
				Result     struct {
					ParentClosureEligible bool `json:"parent_closure_eligible"`
				} `json:"result"`
			} `json:"latest_recovery"`
		} `json:"incidents"`
	}
	if err := json.NewDecoder(w.Body).Decode(&body); err != nil {
		t.Fatalf("decode mass incidents: %v", err)
	}
	if body.Count != 1 || len(body.Incidents) != 1 {
		t.Fatalf("mass projection count = %d incidents=%#v", body.Count, body.Incidents)
	}
	got := body.Incidents[0]
	if got.ParentID != "MASS-42" || got.NodeID != "OLT-01/PON-7" || got.LatestRecovery == nil || got.LatestRecovery.Provenance != "independent-re-read" || !got.LatestRecovery.Result.ParentClosureEligible {
		t.Fatalf("mass projection incorrect: %#v", got)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("GET /api/incidents/mass must not mutate monitoring persistence")
	}
}

func TestMassIncidentsAllowsOnlyGETAndRemainsOperatorGuarded(t *testing.T) {
	_, h, _, _ := monitoringRouteServer(t)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, authorizedRequest(http.MethodPost, "/api/incidents/mass"))
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST mass status = %d, want 405", w.Code)
	}

	r := httptest.NewRequest(http.MethodGet, "http://noc.local/api/incidents/mass", nil)
	r.RemoteAddr = "203.0.113.9:1234"
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unguarded mass endpoint status = %d, want 401", w.Code)
	}
}
