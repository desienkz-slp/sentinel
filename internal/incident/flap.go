package incident

import (
	"sort"
	"time"
)

const (
	uplinkFlapWindow   = 15 * time.Minute
	uplinkFlapCooldown = 15 * time.Minute
	uplinkFlapMinimum  = 3
)

// UplinkState is a canonical, input-provided uplink state.
type UplinkState string

const (
	UplinkUp   UplinkState = "UP"
	UplinkDown UplinkState = "DOWN"
)

// UplinkObservation is immutable monitoring evidence supplied to the evaluator.
type UplinkObservation struct {
	EventID    string
	UplinkID   string
	State      UplinkState
	ObservedAt time.Time
}

// UplinkFlapFinding is read-only evidence that a canonical uplink flapped.
type UplinkFlapFinding struct {
	UplinkID    string
	DetectedAt  time.Time
	Transitions int
}

// UplinkFlapInput contains only caller-supplied evidence and prior findings.
type UplinkFlapInput struct {
	Observations  []UplinkObservation
	PriorFindings []UplinkFlapFinding
}

// CanonicalizeUplinkObservations returns a copy ordered by uplink, observation
// time, then event identity. This prevents arrival order from affecting findings.
func CanonicalizeUplinkObservations(observations []UplinkObservation) []UplinkObservation {
	ordered := append([]UplinkObservation(nil), observations...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if ordered[i].UplinkID != ordered[j].UplinkID {
			return ordered[i].UplinkID < ordered[j].UplinkID
		}
		if !ordered[i].ObservedAt.Equal(ordered[j].ObservedAt) {
			return ordered[i].ObservedAt.Before(ordered[j].ObservedAt)
		}
		return ordered[i].EventID < ordered[j].EventID
	})
	return ordered
}

// EvaluateUplinkFlaps evaluates immutable uplink evidence without I/O or state mutation.
func EvaluateUplinkFlaps(input UplinkFlapInput) []UplinkFlapFinding {
	type sequence struct {
		observed        bool
		state           UplinkState
		transitionTimes []time.Time
	}

	sequences := make(map[string]sequence)
	seenEvents := make(map[string]struct{})
	lastFinding := make(map[string]time.Time)
	for _, finding := range input.PriorFindings {
		if prior, exists := lastFinding[finding.UplinkID]; !exists || finding.DetectedAt.After(prior) {
			lastFinding[finding.UplinkID] = finding.DetectedAt
		}
	}
	var findings []UplinkFlapFinding
	for _, observation := range CanonicalizeUplinkObservations(input.Observations) {
		if observation.EventID != "" {
			if _, seen := seenEvents[observation.EventID]; seen {
				continue
			}
			seenEvents[observation.EventID] = struct{}{}
		}
		sequence := sequences[observation.UplinkID]
		if sequence.observed {
			if sequence.state == observation.State {
				continue
			}
			sequence.transitionTimes = append(sequence.transitionTimes, observation.ObservedAt)
			windowStart := observation.ObservedAt.Add(-uplinkFlapWindow)
			for len(sequence.transitionTimes) > 0 && sequence.transitionTimes[0].Before(windowStart) {
				sequence.transitionTimes = sequence.transitionTimes[1:]
			}
		} else {
			sequence.observed = true
		}
		sequence.state = observation.State
		sequences[observation.UplinkID] = sequence
		if len(sequence.transitionTimes) == uplinkFlapMinimum {
			if prior, found := lastFinding[observation.UplinkID]; found && !observation.ObservedAt.Before(prior) && observation.ObservedAt.Before(prior.Add(uplinkFlapCooldown)) {
				continue
			}
			finding := UplinkFlapFinding{
				UplinkID:    observation.UplinkID,
				DetectedAt:  observation.ObservedAt,
				Transitions: len(sequence.transitionTimes),
			}
			findings = append(findings, finding)
			lastFinding[observation.UplinkID] = finding.DetectedAt
		}
	}
	return findings
}
