package incident

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAddAndRecent(t *testing.T) {
	s := New("", 10)
	s.Add(Incident{Identity: "628111", Intent: "CUSTOMER_INTERNET_DOWN", StartedAt: time.Now()})
	s.Add(Incident{Identity: "628222", Intent: "CUSTOMER_SLOW_INTERNET", StartedAt: time.Now().Add(time.Minute)})
	if s.Len() != 2 {
		t.Fatalf("Len = %d, mau 2", s.Len())
	}
	recent := s.Recent(10)
	if len(recent) != 2 {
		t.Fatalf("Recent = %d, mau 2", len(recent))
	}
	// Terbaru dulu.
	if recent[0].Identity != "628222" {
		t.Errorf("Recent[0].Identity = %s, mau 628222", recent[0].Identity)
	}
}

func TestPersistAndReload(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "incidents.json")
	s := New(p, 10)
	s.Add(Incident{Identity: "628333", Intent: "CUSTOMER_DNS_PROBLEM", Status: StatusInvestigating})

	// Reload dari disk.
	s2 := New(p, 10)
	if s2.Len() != 1 {
		t.Fatalf("setelah reload Len = %d, mau 1", s2.Len())
	}
	got := s2.Recent(1)[0]
	if got.Identity != "628333" || got.Status != StatusInvestigating {
		t.Errorf("data tidak utuh: %+v", got)
	}
}

func TestCapEnforced(t *testing.T) {
	s := New("", 3)
	for i := 0; i < 10; i++ {
		s.Add(Incident{Identity: "628999", StartedAt: time.Now()})
	}
	if s.Len() != 3 {
		t.Errorf("cap = %d, mau 3 (yang lama dibuang)", s.Len())
	}
}

func TestForIdentity(t *testing.T) {
	s := New("", 10)
	s.Add(Incident{Identity: "628111", StartedAt: time.Now().Add(-time.Hour)})
	s.Add(Incident{Identity: "628222", StartedAt: time.Now()})
	s.Add(Incident{Identity: "628111", StartedAt: time.Now().Add(-time.Minute)})

	got := s.ForIdentity("628111")
	if len(got) != 2 {
		t.Fatalf("ForIdentity(628111) = %d, mau 2", len(got))
	}
	// Terbaru dulu.
	if !got[0].StartedAt.After(got[1].StartedAt) {
		t.Error("ForIdentity harus terurut terbaru dulu")
	}
}

func TestAutoIDAndTimestamps(t *testing.T) {
	s := New("", 10)
	inc := s.Add(Incident{Identity: "628111"})
	if inc.ID == "" {
		t.Error("ID tidak boleh kosong")
	}
	if inc.CreatedAt.IsZero() || inc.StartedAt.IsZero() {
		t.Error("CreatedAt/StartedAt harus terisi otomatis")
	}
}

func TestNoPersistWhenPathEmpty(t *testing.T) {
	s := New("", 10)
	s.Add(Incident{Identity: "628111"})
	if err := s.Save(); err != nil {
		t.Errorf("Save tanpa path = %v, mau nil (memori saja)", err)
	}
}

func TestLoadMissingFileEmpty(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "x.json"), 10)
	if s.Len() != 0 {
		t.Errorf("file hilang -> Len = %d, mau 0", s.Len())
	}
	_ = os.RemoveAll
}
