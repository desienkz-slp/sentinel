package incident

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"
)

var ErrImmutableParentSnapshot = errors.New("monitoring parent snapshot is immutable")

// MassIncidentParentSnapshot is the fixed evidence captured when a monitored
// mass-incident parent opens. Active state is tracked separately from this
// immutable evidence.
type MassIncidentParentSnapshot struct {
	ParentID    string    `json:"parent_id"`
	NodeID      string    `json:"node_id"`
	Topology    Topology  `json:"topology"`
	OpenedAt    time.Time `json:"opened_at"`
	CustomerIDs []string  `json:"customer_ids"`
}

type monitoringParent struct {
	Snapshot MassIncidentParentSnapshot `json:"snapshot"`
	Active   bool                       `json:"active"`
}

type monitoringData struct {
	Parents             map[string]monitoringParent     `json:"parents"`
	RecoveryEvaluations map[string][]RecoveryEvaluation `json:"recovery_evaluations"`
	FlapState           UplinkFlapState                 `json:"flap_state"`
}

// UplinkFlapState is the durable evidence needed to preserve event dedupe and
// evaluator cooldown across process restarts.
type UplinkFlapState struct {
	Observations  []UplinkObservation `json:"observations"`
	PriorFindings []UplinkFlapFinding `json:"prior_findings"`
}

// RecoveryEvaluation records the read-only evaluator result together with the
// source provenance of the independent re-read.
type RecoveryEvaluation struct {
	ParentID    string             `json:"parent_id"`
	EvaluatedAt time.Time          `json:"evaluated_at"`
	Provenance  string             `json:"provenance"`
	Result      MassRecoveryResult `json:"result"`
}

// MonitoringStore is the dedicated durable, read-only monitoring evidence
// store. It intentionally does not share the capped generic incident store.
type MonitoringStore struct {
	mu   sync.RWMutex
	path string
	max  int
	data monitoringData
}

// OpenMonitoringStore loads a dedicated JSON store. A missing file starts
// empty; malformed or unreadable existing files return an explicit error.
func OpenMonitoringStore(path string, max int) (*MonitoringStore, error) {
	if max <= 0 {
		max = 500
	}
	s := &MonitoringStore{path: path, max: max, data: monitoringData{
		Parents:             make(map[string]monitoringParent),
		RecoveryEvaluations: make(map[string][]RecoveryEvaluation),
	}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// SaveParentSnapshot records a parent only once. Later writes must be exactly
// identical and cannot change its fixed membership or topology evidence.
func (s *MonitoringStore) SaveParentSnapshot(snapshot MassIncidentParentSnapshot) error {
	if snapshot.ParentID == "" {
		return errors.New("monitoring parent ID is required")
	}
	snapshot = cloneParentSnapshot(snapshot)
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, found := s.data.Parents[snapshot.ParentID]; found {
		if !reflect.DeepEqual(existing.Snapshot, snapshot) {
			return ErrImmutableParentSnapshot
		}
		return nil
	}
	s.data.Parents[snapshot.ParentID] = monitoringParent{Snapshot: snapshot, Active: true}
	s.trimInactiveParents()
	return s.persistLocked()
}

// SetParentActive updates retention eligibility only; it never changes immutable
// parent evidence and performs no workflow or case transition.
func (s *MonitoringStore) SetParentActive(parentID string, active bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	parent, found := s.data.Parents[parentID]
	if !found {
		return errors.New("monitoring parent snapshot not found")
	}
	parent.Active = active
	s.data.Parents[parentID] = parent
	s.trimInactiveParents()
	return s.persistLocked()
}

// ParentSnapshot returns a copy so callers cannot mutate stored membership.
func (s *MonitoringStore) ParentSnapshot(parentID string) (MassIncidentParentSnapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	parent, found := s.data.Parents[parentID]
	return cloneParentSnapshot(parent.Snapshot), found
}

// ParentSnapshots returns immutable parent evidence in deterministic newest-first
// order. It is read-only and does not alter retention or lifecycle state.
func (s *MonitoringStore) ParentSnapshots() []MassIncidentParentSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MassIncidentParentSnapshot, 0, len(s.data.Parents))
	for _, parent := range s.data.Parents {
		out = append(out, cloneParentSnapshot(parent.Snapshot))
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OpenedAt.Equal(out[j].OpenedAt) {
			return out[i].ParentID < out[j].ParentID
		}
		return out[i].OpenedAt.After(out[j].OpenedAt)
	})
	return out
}

// LatestRecoveryEvaluation returns the latest stored evaluator result, if any.
// The returned value is copied from durable evidence and has no side effects.
func (s *MonitoringStore) LatestRecoveryEvaluation(parentID string) (RecoveryEvaluation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	evaluations := s.data.RecoveryEvaluations[parentID]
	if len(evaluations) == 0 {
		return RecoveryEvaluation{}, false
	}
	return evaluations[len(evaluations)-1], true
}

func cloneParentSnapshot(snapshot MassIncidentParentSnapshot) MassIncidentParentSnapshot {
	snapshot.CustomerIDs = append([]string(nil), snapshot.CustomerIDs...)
	return snapshot
}

// AppendRecoveryEvaluation retains the evaluator result and provenance without
// changing any parent or customer lifecycle state.
func (s *MonitoringStore) AppendRecoveryEvaluation(evaluation RecoveryEvaluation) error {
	if evaluation.ParentID == "" {
		return errors.New("monitoring recovery parent ID is required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, found := s.data.Parents[evaluation.ParentID]; !found {
		return errors.New("monitoring recovery parent snapshot not found")
	}
	evaluations := append(s.data.RecoveryEvaluations[evaluation.ParentID], evaluation)
	if len(evaluations) > s.max {
		evaluations = evaluations[len(evaluations)-s.max:]
	}
	s.data.RecoveryEvaluations[evaluation.ParentID] = evaluations
	return s.persistLocked()
}

// RecoveryEvaluations returns a copy in append order.
func (s *MonitoringStore) RecoveryEvaluations(parentID string) []RecoveryEvaluation {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append([]RecoveryEvaluation(nil), s.data.RecoveryEvaluations[parentID]...)
}

// RecordUplinkFlapState merges durable event evidence and findings. Event IDs
// are deduplicated, observations are canonicalized, and both histories are
// bounded by the store retention limit.
func (s *MonitoringStore) RecordUplinkFlapState(state UplinkFlapState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	merged := append(append([]UplinkObservation(nil), s.data.FlapState.Observations...), state.Observations...)
	merged = dedupeUplinkObservations(CanonicalizeUplinkObservations(merged))
	if len(merged) > s.max {
		merged = merged[len(merged)-s.max:]
	}
	findings := append(append([]UplinkFlapFinding(nil), s.data.FlapState.PriorFindings...), state.PriorFindings...)
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].UplinkID != findings[j].UplinkID {
			return findings[i].UplinkID < findings[j].UplinkID
		}
		return findings[i].DetectedAt.Before(findings[j].DetectedAt)
	})
	if len(findings) > s.max {
		findings = findings[len(findings)-s.max:]
	}
	s.data.FlapState = UplinkFlapState{Observations: merged, PriorFindings: findings}
	return s.persistLocked()
}

// UplinkFlapState returns copies of the persisted read-only flap evidence.
func (s *MonitoringStore) UplinkFlapState() UplinkFlapState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return UplinkFlapState{
		Observations:  append([]UplinkObservation(nil), s.data.FlapState.Observations...),
		PriorFindings: append([]UplinkFlapFinding(nil), s.data.FlapState.PriorFindings...),
	}
}

func dedupeUplinkObservations(observations []UplinkObservation) []UplinkObservation {
	seen := make(map[string]struct{}, len(observations))
	out := make([]UplinkObservation, 0, len(observations))
	for _, observation := range observations {
		if observation.EventID != "" {
			if _, found := seen[observation.EventID]; found {
				continue
			}
			seen[observation.EventID] = struct{}{}
		}
		out = append(out, observation)
	}
	return out
}

func (s *MonitoringStore) trimInactiveParents() {
	for len(s.data.Parents) > s.max {
		var oldestID string
		var oldest time.Time
		for id, parent := range s.data.Parents {
			if parent.Active {
				continue
			}
			if oldestID == "" || parent.Snapshot.OpenedAt.Before(oldest) || (parent.Snapshot.OpenedAt.Equal(oldest) && id < oldestID) {
				oldestID, oldest = id, parent.Snapshot.OpenedAt
			}
		}
		if oldestID == "" {
			return
		}
		delete(s.data.Parents, oldestID)
	}
}

func (s *MonitoringStore) persistLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create monitoring store directory: %w", err)
	}
	body, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal monitoring store: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write monitoring store: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("replace monitoring store: %w", err)
	}
	return nil
}

func (s *MonitoringStore) load() error {
	if s.path == "" {
		return nil
	}
	body, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read monitoring store: %w", err)
	}
	if err := json.Unmarshal(body, &s.data); err != nil {
		return fmt.Errorf("decode monitoring store: %w", err)
	}
	if s.data.Parents == nil {
		s.data.Parents = make(map[string]monitoringParent)
	}
	if s.data.RecoveryEvaluations == nil {
		s.data.RecoveryEvaluations = make(map[string][]RecoveryEvaluation)
	}
	s.trimInactiveParents()
	return nil
}
