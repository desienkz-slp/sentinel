package audit

import (
	"path/filepath"
	"testing"
)

func TestRecordAndRecent(t *testing.T) {
	s := New("", 10)
	s.Record(Entry{EventType: "tool_call", Actor: "agent", EntityType: "incident", EntityID: "INC-1"})
	s.Record(Entry{EventType: "policy_decision", Actor: "policy", EntityType: "incident", EntityID: "INC-1"})
	if s.Len() != 2 {
		t.Fatalf("Len = %d, mau 2", s.Len())
	}
	r := s.Recent(10)
	if r[0].EventType != "policy_decision" {
		t.Errorf("Recent[0] = %s, mau policy_decision (terbaru dulu)", r[0].EventType)
	}
}

func TestIDsMonotonic(t *testing.T) {
	s := New("", 10)
	for i := 0; i < 5; i++ {
		s.Record(Entry{EventType: "e", Actor: "a"})
	}
	if err := s.Validate(); err != nil {
		t.Errorf("ID harus monoton: %v", err)
	}
}

func TestPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "audit.json")
	s := New(p, 10)
	s.Record(Entry{EventType: "action_started", Actor: "operator", RequestID: "req-1", TraceID: "trace-1"})

	s2 := New(p, 10)
	if s2.Len() != 1 {
		t.Fatalf("reload Len = %d, mau 1", s2.Len())
	}
	got := s2.Recent(1)[0]
	if got.RequestID != "req-1" || got.TraceID != "trace-1" || got.Actor != "operator" {
		t.Errorf("data audit tidak utuh: %+v", got)
	}
	// ID berlanjut setelah reload (tidak mulai dari 1 lagi).
	next := s2.Record(Entry{EventType: "x", Actor: "y"})
	if next.ID <= got.ID {
		t.Errorf("ID setelah reload (%d) harus > ID sebelum (%d)", next.ID, got.ID)
	}
}

func TestCapEnforced(t *testing.T) {
	s := New("", 3)
	for i := 0; i < 10; i++ {
		s.Record(Entry{EventType: "e", Actor: "a"})
	}
	if s.Len() != 3 {
		t.Errorf("cap = %d, mau 3", s.Len())
	}
}

func TestRecordAutoTimestamp(t *testing.T) {
	s := New("", 10)
	e := s.Record(Entry{EventType: "e", Actor: "a"})
	if e.OccurredAt.IsZero() {
		t.Error("OccurredAt harus terisi otomatis")
	}
	if e.ID == 0 {
		t.Error("ID harus terisi otomatis")
	}
}
