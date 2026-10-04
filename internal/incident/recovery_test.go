package incident

import (
	"strconv"
	"testing"
)

func TestEvaluateMassRecoveryAllowsOnlyParentClosureAtExactlyNinetyPercent(t *testing.T) {
	result := EvaluateMassRecovery(MassRecoveryInput{
		Snapshot: ActiveMembershipSnapshot{
			ParentID:    "MASS-1",
			NodeID:      "OLT-01/PON-7",
			CustomerIDs: customerIDs(10),
		},
		Observations: activeObservations("OLT-01/PON-7", customerIDs(9)),
	})

	if !result.ParentClosureEligible || result.Indeterminate {
		t.Fatalf("exactly 90%% must permit parent closure: %+v", result)
	}
	if result.RecoveredCustomers != 9 || result.SnapshotCustomers != 10 {
		t.Fatalf("counts = recovered %d snapshot %d, want 9 and 10", result.RecoveredCustomers, result.SnapshotCustomers)
	}
}

func TestEvaluateMassRecoveryRejectsEightyNinePercent(t *testing.T) {
	result := EvaluateMassRecovery(MassRecoveryInput{
		Snapshot:     ActiveMembershipSnapshot{ParentID: "MASS-1", NodeID: "OLT-01/PON-7", CustomerIDs: customerIDs(100)},
		Observations: activeObservations("OLT-01/PON-7", customerIDs(89)),
	})

	if result.ParentClosureEligible || result.Indeterminate {
		t.Fatalf("89%% must not permit parent closure: %+v", result)
	}
}

func TestEvaluateMassRecoveryIsIndeterminateForEmptySnapshot(t *testing.T) {
	result := EvaluateMassRecovery(MassRecoveryInput{
		Snapshot: ActiveMembershipSnapshot{ParentID: "MASS-1", NodeID: "OLT-01/PON-7"},
	})

	if !result.Indeterminate || result.ParentClosureEligible {
		t.Fatalf("empty snapshot must be indeterminate, never eligible: %+v", result)
	}
}

func TestEvaluateMassRecoveryDoesNotInflateDuplicateCustomerObservations(t *testing.T) {
	result := EvaluateMassRecovery(MassRecoveryInput{
		Snapshot: ActiveMembershipSnapshot{ParentID: "MASS-1", NodeID: "OLT-01/PON-7", CustomerIDs: []string{"customer-1", "customer-2"}},
		Observations: []RecoveryObservation{
			{CustomerID: "customer-1", NodeID: "OLT-01/PON-7", State: ObservationActive, Fresh: true, Independent: true},
			{CustomerID: "customer-1", NodeID: "OLT-01/PON-7", State: ObservationActive, Fresh: true, Independent: true},
		},
	})

	if result.RecoveredCustomers != 1 || result.ParentClosureEligible {
		t.Fatalf("duplicate identity must count once: %+v", result)
	}
}

func TestEvaluateMassRecoveryExcludesUnknownAndStaleObservations(t *testing.T) {
	result := EvaluateMassRecovery(MassRecoveryInput{
		Snapshot: ActiveMembershipSnapshot{ParentID: "MASS-1", NodeID: "OLT-01/PON-7", CustomerIDs: []string{"customer-1", "customer-2", "customer-3"}},
		Observations: []RecoveryObservation{
			{CustomerID: "customer-1", NodeID: "OLT-01/PON-7", State: ObservationUnknown, Fresh: true, Independent: true},
			{CustomerID: "customer-2", NodeID: "OLT-01/PON-7", State: ObservationActive, Fresh: false, Independent: true},
			{CustomerID: "customer-3", NodeID: "OLT-01/PON-7", State: ObservationActive, Fresh: true, Independent: false},
		},
	})

	if result.RecoveredCustomers != 0 || result.ParentClosureEligible {
		t.Fatalf("unknown, stale, and non-independent observations must not recover: %+v", result)
	}
}

func TestEvaluateMassRecoveryExcludesObservationFromWrongNode(t *testing.T) {
	result := EvaluateMassRecovery(MassRecoveryInput{
		Snapshot: ActiveMembershipSnapshot{ParentID: "MASS-1", NodeID: "OLT-01/PON-7", CustomerIDs: []string{"customer-1"}},
		Observations: []RecoveryObservation{
			{CustomerID: "customer-1", NodeID: "OLT-02/PON-7", State: ObservationActive, Fresh: true, Independent: true},
		},
	})

	if result.RecoveredCustomers != 0 || result.ParentClosureEligible {
		t.Fatalf("wrong-node observation must not recover: %+v", result)
	}
}

func customerIDs(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "customer-" + strconv.Itoa(i+1)
	}
	return out
}

func activeObservations(nodeID string, customerIDs []string) []RecoveryObservation {
	out := make([]RecoveryObservation, len(customerIDs))
	for i, customerID := range customerIDs {
		out[i] = RecoveryObservation{CustomerID: customerID, NodeID: nodeID, State: ObservationActive, Fresh: true, Independent: true}
	}
	return out
}
