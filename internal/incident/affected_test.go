package incident

import (
	"testing"
	"time"
)

func TestAffectedCustomers(t *testing.T) {
	s := New("", 50)
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	add := func(id, ident, intent string, st Status, ago time.Duration) {
		s.Add(Incident{ID: id, Identity: ident, Intent: intent, Status: st, StartedAt: now.Add(-ago)})
	}
	add("1", "628111000001", "no_internet", StatusInvestigating, time.Minute)
	add("2", "628111000002", "no_internet", StatusEscalated, 5*time.Minute)
	add("3", "628111000001", "no_internet", StatusInvestigating, 2*time.Minute) // duplikat pelanggan
	add("4", "628111000003", "no_internet", StatusClosed, time.Minute)          // tidak aktif
	add("5", "628111000004", "no_internet", StatusInvestigating, time.Hour)     // di luar jendela
	add("6", "628111000005", "slow", StatusInvestigating, time.Minute)          // intent lain
	if got := s.AffectedCustomers("no_internet", 15*time.Minute, now); got != 2 {
		t.Fatalf("got %d, mau 2", got)
	}
	if s.AffectedCustomers("", time.Minute, now) != 0 || s.AffectedCustomers("x", 0, now) != 0 {
		t.Fatal("argumen kosong harus 0")
	}
}
