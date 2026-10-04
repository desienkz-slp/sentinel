package incident

// ObservationState is the verified service state returned by a re-read.
type ObservationState string

const (
	ObservationActive  ObservationState = "ACTIVE"
	ObservationUnknown ObservationState = "UNKNOWN"
)

// ActiveMembershipSnapshot is the fixed active-customer membership captured
// when a mass-incident parent opens. It is the recovery denominator.
type ActiveMembershipSnapshot struct {
	ParentID    string
	NodeID      string
	CustomerIDs []string
}

// RecoveryObservation is a single, independently re-read customer state.
type RecoveryObservation struct {
	CustomerID  string
	NodeID      string
	State       ObservationState
	Fresh       bool
	Independent bool
}

// MassRecoveryInput contains only immutable recovery evidence. It has no I/O
// and does not change any incident or customer-case lifecycle state.
type MassRecoveryInput struct {
	Snapshot     ActiveMembershipSnapshot
	Observations []RecoveryObservation
}

// MassRecoveryResult says only whether the parent may be closed; individual
// customer cases are intentionally outside this evaluator's authority.
type MassRecoveryResult struct {
	SnapshotCustomers     int
	RecoveredCustomers    int
	ParentClosureEligible bool
	Indeterminate         bool
}

// EvaluateMassRecovery evaluates the fixed membership snapshot against fresh,
// independent ACTIVE observations from the same node. At least 90 percent is
// required before an incident parent may be closed.
func EvaluateMassRecovery(input MassRecoveryInput) MassRecoveryResult {
	members := make(map[string]struct{}, len(input.Snapshot.CustomerIDs))
	for _, customerID := range input.Snapshot.CustomerIDs {
		if customerID != "" {
			members[customerID] = struct{}{}
		}
	}

	result := MassRecoveryResult{SnapshotCustomers: len(members)}
	if result.SnapshotCustomers == 0 {
		result.Indeterminate = true
		return result
	}

	recovered := make(map[string]struct{}, len(input.Observations))
	for _, observation := range input.Observations {
		if observation.NodeID != input.Snapshot.NodeID || observation.State != ObservationActive || !observation.Fresh || !observation.Independent {
			continue
		}
		if _, member := members[observation.CustomerID]; member {
			recovered[observation.CustomerID] = struct{}{}
		}
	}

	result.RecoveredCustomers = len(recovered)
	result.ParentClosureEligible = result.RecoveredCustomers*100 >= result.SnapshotCustomers*90
	return result
}
