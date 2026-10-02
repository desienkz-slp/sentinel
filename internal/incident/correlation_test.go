package incident

import (
	"testing"
	"time"
)

func TestCorrelateSuppressesDuplicateIncident(t *testing.T) {
	store := New("", 20)
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	policy := CorrelationPolicy{Window: 15 * time.Minute, MassThreshold: 3}

	first := store.Record(Incident{
		ID: "INC-1", Status: StatusInvestigating, Identity: "628111", Intent: "CUSTOMER_INTERNET_DOWN",
		Topology: Topology{Router: "RTR-CIMAHI", OLT: "OLT-01", PON: "PON-1"}, StartedAt: now,
	}, policy)
	if first.Suppressed || first.Kind != CorrelationNone {
		t.Fatalf("insiden pertama tidak boleh tersupresi: %+v", first)
	}

	duplicate := store.Record(Incident{
		ID: "INC-2", Status: StatusInvestigating, Identity: "628111", Intent: "CUSTOMER_INTERNET_DOWN",
		Topology: Topology{Router: "RTR-CIMAHI", OLT: "OLT-01", PON: "PON-1"}, StartedAt: now.Add(time.Minute),
	}, policy)
	if !duplicate.Suppressed || duplicate.Kind != CorrelationDuplicate {
		t.Fatalf("duplikat harus menekan diagnosis redundan: %+v", duplicate)
	}
	if duplicate.Incident.CorrelationID != "INC-1" {
		t.Errorf("correlation_id = %q, mau INC-1", duplicate.Incident.CorrelationID)
	}
	if store.Len() != 1 {
		t.Errorf("duplikat tidak boleh disimpan sebagai insiden baru, Len = %d", store.Len())
	}
}

func TestCorrelateSuppressesDuplicateWithoutTopology(t *testing.T) {
	store := New("", 20)
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	policy := CorrelationPolicy{Window: 15 * time.Minute, MassThreshold: 3}

	store.Record(Incident{ID: "INC-1", Status: StatusInvestigating, Identity: "628111", Intent: "CUSTOMER_INTERNET_DOWN", StartedAt: now}, policy)
	duplicate := store.Record(Incident{ID: "INC-2", Status: StatusInvestigating, Identity: "628111", Intent: "CUSTOMER_INTERNET_DOWN", StartedAt: now.Add(time.Minute)}, policy)

	if !duplicate.Suppressed || duplicate.Kind != CorrelationDuplicate {
		t.Fatalf("duplikat tanpa topologi harus tetap ditekan: %+v", duplicate)
	}
}

func TestCorrelateGroupsMassIncidentByTopologyAndTime(t *testing.T) {
	store := New("", 20)
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	policy := CorrelationPolicy{Window: 10 * time.Minute, MassThreshold: 3}
	topology := Topology{Area: "Cimahi", Router: "RTR-CIMAHI", OLT: "OLT-01", PON: "PON-7"}

	for i, identity := range []string{"628111", "628222", "628333"} {
		result := store.Record(Incident{
			ID: "INC-MASS-" + identity, Status: StatusInvestigating, Identity: identity,
			Intent: "CUSTOMER_INTERNET_DOWN", Topology: topology, StartedAt: now.Add(time.Duration(i) * time.Minute),
		}, policy)
		if i < 2 && result.Kind != CorrelationNone {
			t.Fatalf("belum memenuhi ambang mass incident: %+v", result)
		}
		if i == 2 {
			if !result.Suppressed || result.Kind != CorrelationMass {
				t.Fatalf("customer ketiga harus tergabung mass incident: %+v", result)
			}
			if result.Incident.CorrelationID == "" {
				t.Fatal("mass incident harus memiliki correlation_id stabil")
			}
		}
	}

	groups := store.MassIncidents()
	if len(groups) != 1 {
		t.Fatalf("mass incidents = %d, mau 1", len(groups))
	}
	if groups[0].AffectedCustomers != 3 || groups[0].Topology.PON != "PON-7" {
		t.Errorf("group = %+v, mau tiga pelanggan pada PON-7", groups[0])
	}
	groupID := groups[0].ID
	fourth := store.Record(Incident{
		ID: "INC-MASS-628444", Status: StatusInvestigating, Identity: "628444",
		Intent: "CUSTOMER_INTERNET_DOWN", Topology: topology, StartedAt: now.Add(3 * time.Minute),
	}, policy)
	if fourth.Incident.CorrelationID != groupID {
		t.Errorf("anggota lanjutan harus memakai group stabil: %q, mau %q", fourth.Incident.CorrelationID, groupID)
	}
	if got := store.MassIncidents()[0].AffectedCustomers; got != 4 {
		t.Errorf("affected_customers = %d, mau 4", got)
	}
}

func TestCorrelateDoesNotGroupDifferentTopologyOrExpiredWindow(t *testing.T) {
	store := New("", 20)
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	policy := CorrelationPolicy{Window: 5 * time.Minute, MassThreshold: 2}

	store.Record(Incident{ID: "INC-A", Status: StatusInvestigating, Identity: "628111", Intent: "CUSTOMER_INTERNET_DOWN", Topology: Topology{OLT: "OLT-01", PON: "PON-1"}, StartedAt: now}, policy)
	different := store.Record(Incident{ID: "INC-B", Status: StatusInvestigating, Identity: "628222", Intent: "CUSTOMER_INTERNET_DOWN", Topology: Topology{OLT: "OLT-01", PON: "PON-2"}, StartedAt: now.Add(time.Minute)}, policy)
	expired := store.Record(Incident{ID: "INC-C", Status: StatusInvestigating, Identity: "628333", Intent: "CUSTOMER_INTERNET_DOWN", Topology: Topology{OLT: "OLT-01", PON: "PON-1"}, StartedAt: now.Add(6 * time.Minute)}, policy)

	if different.Kind != CorrelationNone || expired.Kind != CorrelationNone {
		t.Fatalf("topologi/waktu berbeda tidak boleh dikorelasikan: different=%+v expired=%+v", different, expired)
	}
	if len(store.MassIncidents()) != 0 {
		t.Fatal("tidak boleh ada mass incident")
	}
}
