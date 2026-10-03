package incident

import "time"

// AffectedCustomers menghitung pelanggan BERBEDA yang punya insiden aktif dengan
// intent yang sama dalam jendela terakhir. Hanya membaca; tanpa efek samping.
func (s *Store) AffectedCustomers(intent string, window time.Duration, now time.Time) int {
	if intent == "" || window <= 0 {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	seen := map[string]struct{}{}
	for _, in := range s.list {
		if in.Intent != intent || !isActive(in.Status) || in.Identity == "" {
			continue
		}
		if now.Sub(in.StartedAt) > window || in.StartedAt.Sub(now) > window {
			continue
		}
		seen[in.Identity] = struct{}{}
	}
	return len(seen)
}
