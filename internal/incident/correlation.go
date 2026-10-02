package incident

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	defaultCorrelationWindow = 15 * time.Minute
	defaultMassThreshold     = 3
)

// Record mencatat insiden dan secara atomik menentukan apakah diagnosis baru
// harus ditekan. Hanya metadata topologi yang telah diverifikasi yang dipakai;
// tanpa metadata tersebut sistem tidak menganggap dua pelanggan berkorelasi.
func (s *Store) Record(inc Incident, policy CorrelationPolicy) CorrelationResult {
	policy = normalizePolicy(policy)
	s.mu.Lock()
	defer s.mu.Unlock()

	inc = normalizeIncident(inc)
	key := inc.topologyKey()
	// Pengulangan dari pelanggan yang sama tidak membutuhkan data topologi:
	// message/case yang sama tetap harus menekan diagnosis kedua dalam jendela.
	for _, existing := range s.list {
		if existing.Identity == inc.Identity && existing.Intent == inc.Intent && isActive(existing.Status) &&
			durationAbs(existing.StartedAt.Sub(inc.StartedAt)) <= policy.Window {
			inc.CorrelationID = existing.ID
			return CorrelationResult{Incident: inc, Kind: CorrelationDuplicate, Suppressed: true}
		}
	}
	if key == "" {
		s.addLocked(inc)
		return CorrelationResult{Incident: inc, Kind: CorrelationNone}
	}

	matched := s.matchingLocked(inc, key, policy.Window)
	customers := distinctCustomers(matched, inc)
	if len(customers) >= policy.MassThreshold {
		groupID := massID(inc, key)
		for _, existing := range matched {
			if strings.HasPrefix(existing.CorrelationID, "MASS-") {
				groupID = existing.CorrelationID
				break
			}
		}
		memberIDs := make([]string, 0, len(matched)+1)
		for _, existing := range matched {
			memberIDs = append(memberIDs, existing.ID)
		}
		memberIDs = append(memberIDs, inc.ID)
		for i := range s.list {
			if containsID(memberIDs, s.list[i].ID) {
				s.list[i].CorrelationID = groupID
			}
		}
		inc.CorrelationID = groupID
		inc.Status = StatusEscalated
		s.addLocked(inc)
		return CorrelationResult{Incident: inc, Kind: CorrelationMass, Suppressed: true}
	}

	s.addLocked(inc)
	return CorrelationResult{Incident: inc, Kind: CorrelationNone}
}

// MassIncidents mengembalikan semua grup mass incident yang masih ada di store.
func (s *Store) MassIncidents() []MassIncident {
	s.mu.Lock()
	defer s.mu.Unlock()
	groups := map[string][]Incident{}
	for _, inc := range s.list {
		if strings.HasPrefix(inc.CorrelationID, "MASS-") {
			groups[inc.CorrelationID] = append(groups[inc.CorrelationID], inc)
		}
	}
	out := make([]MassIncident, 0, len(groups))
	for id, members := range groups {
		sort.Slice(members, func(i, j int) bool { return members[i].StartedAt.Before(members[j].StartedAt) })
		identities := map[string]struct{}{}
		ids := make([]string, 0, len(members))
		for _, member := range members {
			identities[member.Identity] = struct{}{}
			ids = append(ids, member.ID)
		}
		out = append(out, MassIncident{
			ID: id, Intent: members[0].Intent, Topology: members[0].Topology,
			StartedAt: members[0].StartedAt, AffectedCustomers: len(identities), IncidentIDs: ids,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func normalizePolicy(policy CorrelationPolicy) CorrelationPolicy {
	if policy.Window <= 0 {
		policy.Window = defaultCorrelationWindow
	}
	if policy.MassThreshold < 2 {
		policy.MassThreshold = defaultMassThreshold
	}
	return policy
}

func normalizeIncident(inc Incident) Incident {
	inc.Identity = strings.TrimSpace(inc.Identity)
	inc.Intent = strings.TrimSpace(inc.Intent)
	inc.Topology = Topology{
		Area: strings.TrimSpace(inc.Topology.Area), Router: strings.TrimSpace(inc.Topology.Router),
		OLT: strings.TrimSpace(inc.Topology.OLT), PON: strings.TrimSpace(inc.Topology.PON),
		Upstream: strings.TrimSpace(inc.Topology.Upstream),
	}
	if inc.ID == "" {
		inc.ID = fmt.Sprintf("INC-%d", time.Now().UnixNano())
	}
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now().UTC()
	}
	if inc.StartedAt.IsZero() {
		inc.StartedAt = inc.CreatedAt
	}
	return inc
}

func (inc Incident) topologyKey() string {
	// Pilih domain bersama paling spesifik agar PON berbeda pada OLT sama tidak
	// keliru dikelompokkan. Area baru dipakai bila tidak ada data lebih spesifik.
	switch {
	case inc.Topology.Upstream != "":
		return "upstream:" + strings.ToLower(inc.Topology.Upstream)
	case inc.Topology.PON != "":
		return "pon:" + strings.ToLower(inc.Topology.OLT) + "/" + strings.ToLower(inc.Topology.PON)
	case inc.Topology.OLT != "":
		return "olt:" + strings.ToLower(inc.Topology.OLT)
	case inc.Topology.Router != "":
		return "router:" + strings.ToLower(inc.Topology.Router)
	case inc.Topology.Area != "":
		return "area:" + strings.ToLower(inc.Topology.Area)
	default:
		return ""
	}
}

func (s *Store) matchingLocked(candidate Incident, key string, window time.Duration) []Incident {
	out := make([]Incident, 0)
	for _, existing := range s.list {
		if existing.Intent != candidate.Intent || existing.topologyKey() != key || !isActive(existing.Status) {
			continue
		}
		if durationAbs(existing.StartedAt.Sub(candidate.StartedAt)) <= window {
			out = append(out, existing)
		}
	}
	return out
}

func (s *Store) addLocked(inc Incident) {
	s.list = append(s.list, inc)
	if len(s.list) > s.max {
		s.list = s.list[len(s.list)-s.max:]
	}
	_ = s.persist()
}

func isActive(status Status) bool {
	return status != StatusResolved && status != StatusClosed
}

func distinctCustomers(matches []Incident, candidate Incident) map[string]struct{} {
	out := map[string]struct{}{candidate.Identity: {}}
	for _, match := range matches {
		out[match.Identity] = struct{}{}
	}
	return out
}

func massID(inc Incident, topologyKey string) string {
	return fmt.Sprintf("MASS-%d-%s", inc.StartedAt.UTC().Unix()/60, strings.NewReplacer(":", "-", "/", "-").Replace(topologyKey))
}

func durationAbs(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

func containsID(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
