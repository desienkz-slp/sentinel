// Package uplinkpoll produces explicitly labelled, read-only poll-derived
// uplink evidence. It does not invoke tools, policies, notifications, or writes
// to RouterOS.
package uplinkpoll

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/incident"
)

const PollDerivedProvenance = "poll-derived-mikrotik-interface-running"

// InterfaceReader is the narrow, read-only MikroTik seam used by the poller.
type InterfaceReader interface {
	InterfaceRunning(context.Context, []string) (map[string]bool, error)
}

// Producer translates allowlisted interface reads into durable monitoring
// evidence and pure flap findings.
type Producer struct {
	store    *incident.MonitoringStore
	readers  map[string]InterfaceReader
	monitors []config.UplinkMonitor
}

func New(store *incident.MonitoringStore, readers map[string]InterfaceReader, monitors []config.UplinkMonitor) (*Producer, error) {
	if store == nil {
		return nil, fmt.Errorf("monitoring store is required")
	}
	if len(monitors) == 0 {
		return nil, fmt.Errorf("at least one allowlisted uplink monitor is required")
	}
	copyMonitors := append([]config.UplinkMonitor(nil), monitors...)
	sort.Slice(copyMonitors, func(i, j int) bool { return copyMonitors[i].UplinkID < copyMonitors[j].UplinkID })
	for _, monitor := range copyMonitors {
		if strings.TrimSpace(monitor.UplinkID) == "" || strings.TrimSpace(monitor.Router) == "" || strings.TrimSpace(monitor.Interface) == "" {
			return nil, fmt.Errorf("uplink monitor requires uplink_id, router, and interface")
		}
		if readers[monitor.Router] == nil {
			return nil, fmt.Errorf("uplink monitor %q has no router reader %q", monitor.UplinkID, monitor.Router)
		}
	}
	return &Producer{store: store, readers: readers, monitors: copyMonitors}, nil
}

// LocalEventID produces a deterministic local identity for a poll-detected
// transition. It is intentionally not a RouterOS event identity.
func LocalEventID(uplinkID string, state incident.UplinkState, observedAt time.Time) string {
	material := strings.Join([]string{PollDerivedProvenance, uplinkID, string(state), observedAt.UTC().Format(time.RFC3339Nano)}, "\x00")
	digest := sha256.Sum256([]byte(material))
	return "poll-derived:" + hex.EncodeToString(digest[:])
}

// Poll performs one read-only pass. Initial concrete reads and failed reads are
// persisted as non-events; only concrete state changes receive local EventIDs.
func (p *Producer) Poll(ctx context.Context, observedAt time.Time) error {
	byRouter := make(map[string][]config.UplinkMonitor)
	for _, monitor := range p.monitors {
		byRouter[monitor.Router] = append(byRouter[monitor.Router], monitor)
	}
	prior := p.store.UplinkFlapState()
	last := lastConcreteStates(prior.Observations)
	var newObservations []incident.UplinkObservation
	for router, monitors := range byRouter {
		names := make([]string, 0, len(monitors))
		for _, monitor := range monitors {
			names = append(names, monitor.Interface)
		}
		states, err := p.readers[router].InterfaceRunning(ctx, names)
		for _, monitor := range monitors {
			observation := incident.UplinkObservation{UplinkID: monitor.UplinkID, ObservedAt: observedAt.UTC(), Provenance: PollDerivedProvenance}
			if err != nil {
				observation.State = incident.UplinkUnknown
				newObservations = append(newObservations, observation)
				continue
			}
			running, found := states[monitor.Interface]
			if !found { // The queried RouterOS row did not map to an allowlisted target.
				continue
			}
			if running {
				observation.State = incident.UplinkUp
			} else {
				observation.State = incident.UplinkDown
			}
			if old, known := last[monitor.UplinkID]; known && old == observation.State {
				continue
			}
			if _, known := last[monitor.UplinkID]; known {
				observation.EventID = LocalEventID(observation.UplinkID, observation.State, observation.ObservedAt)
			}
			newObservations = append(newObservations, observation)
			last[monitor.UplinkID] = observation.State
		}
	}
	if len(newObservations) == 0 {
		return nil
	}
	all := append(append([]incident.UplinkObservation(nil), prior.Observations...), newObservations...)
	findings := incident.EvaluateUplinkFlaps(incident.UplinkFlapInput{Observations: all, PriorFindings: prior.PriorFindings})
	return p.store.RecordUplinkFlapState(incident.UplinkFlapState{Observations: newObservations, PriorFindings: findings})
}

func lastConcreteStates(observations []incident.UplinkObservation) map[string]incident.UplinkState {
	last := make(map[string]incident.UplinkState)
	for _, observation := range incident.CanonicalizeUplinkObservations(observations) {
		if observation.State == incident.UplinkUp || observation.State == incident.UplinkDown {
			last[observation.UplinkID] = observation.State
		}
	}
	return last
}

// Scheduler is a service seam. Production may call Run; tests can exercise
// Poll directly without starting a ticker.
type Scheduler struct {
	producer *Producer
	interval time.Duration
}

func NewScheduler(producer *Producer, interval time.Duration) *Scheduler {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &Scheduler{producer: producer, interval: interval}
}
func (s *Scheduler) Interval() time.Duration { return s.interval }
func (s *Scheduler) Run(ctx context.Context) {
	if s.producer == nil {
		return
	}
	s.producer.Poll(ctx, time.Now().UTC())
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case at := <-ticker.C:
			_ = s.producer.Poll(ctx, at.UTC())
		}
	}
}
