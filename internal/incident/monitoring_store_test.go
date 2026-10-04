package incident

import (
	"errors"
	"os"
	"testing"
	"time"
)

func TestMonitoringStoreRejectsParentSnapshotOverwriteOrEnlargement(t *testing.T) {
	store, err := OpenMonitoringStore(t.TempDir()+"/monitoring.json", 10)
	if err != nil {
		t.Fatal(err)
	}
	original := MassIncidentParentSnapshot{
		ParentID:    "mass-1",
		NodeID:      "olt-1/pon-1",
		Topology:    Topology{OLT: "olt-1", PON: "pon-1"},
		OpenedAt:    time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC),
		CustomerIDs: []string{"customer-1", "customer-2"},
	}
	if err := store.SaveParentSnapshot(original); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.CustomerIDs = append(changed.CustomerIDs, "customer-3")
	if err := store.SaveParentSnapshot(changed); !errors.Is(err, ErrImmutableParentSnapshot) {
		t.Fatalf("enlargement error = %v, want ErrImmutableParentSnapshot", err)
	}
	changed = original
	changed.NodeID = "olt-2/pon-1"
	if err := store.SaveParentSnapshot(changed); !errors.Is(err, ErrImmutableParentSnapshot) {
		t.Fatalf("overwrite error = %v, want ErrImmutableParentSnapshot", err)
	}

	got, ok := store.ParentSnapshot("mass-1")
	if !ok || len(got.CustomerIDs) != 2 || got.NodeID != original.NodeID {
		t.Fatalf("snapshot after rejected writes = %+v, found %t", got, ok)
	}
}

func TestMonitoringStoreReloadsRecoveryEvaluationWithProvenance(t *testing.T) {
	path := t.TempDir() + "/monitoring.json"
	store, err := OpenMonitoringStore(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveParentSnapshot(MassIncidentParentSnapshot{ParentID: "mass-1", NodeID: "olt-1", CustomerIDs: []string{"customer-1"}}); err != nil {
		t.Fatal(err)
	}
	evaluation := RecoveryEvaluation{
		ParentID:    "mass-1",
		EvaluatedAt: time.Date(2026, time.October, 4, 11, 0, 0, 0, time.UTC),
		Provenance:  "synthetic-independent-reread",
		Result:      MassRecoveryResult{SnapshotCustomers: 1, RecoveredCustomers: 1, ParentClosureEligible: true},
	}
	if err := store.AppendRecoveryEvaluation(evaluation); err != nil {
		t.Fatal(err)
	}

	loaded, err := OpenMonitoringStore(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := loaded.RecoveryEvaluations("mass-1")
	if len(got) != 1 || got[0].Provenance != evaluation.Provenance || !got[0].Result.ParentClosureEligible || !got[0].EvaluatedAt.Equal(evaluation.EvaluatedAt) {
		t.Fatalf("reloaded evaluations = %+v", got)
	}
}

func TestMonitoringStoreReloadsFlapDedupeAndCooldownState(t *testing.T) {
	path := t.TempDir() + "/monitoring.json"
	store, err := OpenMonitoringStore(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	if err := store.RecordUplinkFlapState(UplinkFlapState{
		Observations: []UplinkObservation{
			{EventID: "event-2", UplinkID: "uplink-1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-1", UplinkID: "uplink-1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "uplink-1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
		},
		PriorFindings: []UplinkFlapFinding{{UplinkID: "uplink-1", DetectedAt: at}},
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := OpenMonitoringStore(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	state := loaded.UplinkFlapState()
	if len(state.Observations) != 2 || state.Observations[0].EventID != "event-1" {
		t.Fatalf("reloaded observations = %+v", state.Observations)
	}
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: append(state.Observations,
			UplinkObservation{EventID: "event-3", UplinkID: "uplink-1", State: UplinkUp, ObservedAt: at.Add(2 * time.Minute)},
			UplinkObservation{EventID: "event-4", UplinkID: "uplink-1", State: UplinkDown, ObservedAt: at.Add(3 * time.Minute)},
		),
		PriorFindings: state.PriorFindings,
	})
	if len(findings) != 0 {
		t.Fatalf("cooldown findings after reload = %+v", findings)
	}
}

func TestMonitoringStoreRetentionKeepsActiveParent(t *testing.T) {
	store, err := OpenMonitoringStore(t.TempDir()+"/monitoring.json", 2)
	if err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"closed-old", "active", "new"} {
		if err := store.SaveParentSnapshot(MassIncidentParentSnapshot{ParentID: id, OpenedAt: time.Date(2026, time.October, 4, 10+i, 0, 0, 0, time.UTC)}); err != nil {
			t.Fatal(err)
		}
		if id == "closed-old" {
			if err := store.SetParentActive(id, false); err != nil {
				t.Fatal(err)
			}
		}
	}
	if _, found := store.ParentSnapshot("active"); !found {
		t.Fatal("active parent was evicted by retention")
	}
	if _, found := store.ParentSnapshot("closed-old"); found {
		t.Fatal("inactive parent was not evicted by retention")
	}
}

func TestMonitoringStoreReturnsPersistenceError(t *testing.T) {
	dir := t.TempDir() + "/store-dir"
	store, err := OpenMonitoringStore(dir+"/monitoring.json", 10)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveParentSnapshot(MassIncidentParentSnapshot{ParentID: "mass-1"}); err == nil {
		t.Fatal("SaveParentSnapshot returned nil for an unwritable store path")
	}
}
