package incident

import (
	"testing"
	"time"
)

func TestEvaluateUplinkFlapsDoesNotFindThresholdMinusOneTransitions(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(2 * time.Minute)},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want none for two transitions", findings)
	}
}

func TestEvaluateUplinkFlapsFindsThreeAlternatingTransitions(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(2 * time.Minute)},
			{EventID: "event-4", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(3 * time.Minute)},
		},
	})

	if len(findings) != 1 {
		t.Fatalf("findings = %+v, want one", findings)
	}
	if findings[0].UplinkID != "core-a:xe-0/0/1" || findings[0].Transitions != 3 || !findings[0].DetectedAt.Equal(at.Add(3*time.Minute)) {
		t.Fatalf("finding = %+v, want three transitions on the canonical uplink", findings[0])
	}
}

func TestEvaluateUplinkFlapsDoesNotCountRepeatedSameState(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(2 * time.Minute)},
			{EventID: "event-4", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(3 * time.Minute)},
			{EventID: "event-5", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(4 * time.Minute)},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want no finding for two state changes", findings)
	}
}

func TestEvaluateUplinkFlapsIgnoresDuplicateEvent(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(2 * time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(3 * time.Minute)},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want duplicate event excluded", findings)
	}
}

func TestEvaluateUplinkFlapsExcludesTransitionsOutsideWindow(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(2 * time.Minute)},
			{EventID: "event-4", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(17 * time.Minute)},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want transitions outside 15-minute window excluded", findings)
	}
}

func TestEvaluateUplinkFlapsDoesNotCoalesceDifferentInterfaces(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/2", State: UplinkDown, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(2 * time.Minute)},
			{EventID: "event-4", UplinkID: "core-a:xe-0/0/2", State: UplinkUp, ObservedAt: at.Add(3 * time.Minute)},
			{EventID: "event-5", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(4 * time.Minute)},
			{EventID: "event-6", UplinkID: "core-a:xe-0/0/2", State: UplinkDown, ObservedAt: at.Add(5 * time.Minute)},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want separate interfaces not coalesced", findings)
	}
}

func TestEvaluateUplinkFlapsSuppressesFindingDuringCooldown(t *testing.T) {
	at := time.Date(2026, time.October, 4, 10, 0, 0, 0, time.UTC)
	findings := EvaluateUplinkFlaps(UplinkFlapInput{
		PriorFindings: []UplinkFlapFinding{{
			UplinkID:   "core-a:xe-0/0/1",
			DetectedAt: at,
		}},
		Observations: []UplinkObservation{
			{EventID: "event-1", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(time.Minute)},
			{EventID: "event-2", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(2 * time.Minute)},
			{EventID: "event-3", UplinkID: "core-a:xe-0/0/1", State: UplinkUp, ObservedAt: at.Add(3 * time.Minute)},
			{EventID: "event-4", UplinkID: "core-a:xe-0/0/1", State: UplinkDown, ObservedAt: at.Add(4 * time.Minute)},
		},
	})

	if len(findings) != 0 {
		t.Fatalf("findings = %+v, want cooldown to suppress repeat finding", findings)
	}
}
