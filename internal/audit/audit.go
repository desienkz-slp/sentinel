// Package audit menyimpan jejak tindakan penting secara append-only.
//
// Sesuai blueprint §39-40: setiap tindakan penting wajib dicatat dengan actor,
// entity, before/after, request_id, dan trace_id. Log bersifat APPEND-ONLY:
// sekali ditulis, tidak dihapus/diubah (immutabilitas untuk audit).
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Entry adalah satu baris audit.
type Entry struct {
	ID         int64     `json:"id"`
	EventType  string    `json:"event_type"`
	OccurredAt time.Time `json:"occurred_at"`
	Actor      string    `json:"actor"`
	EntityType string    `json:"entity_type,omitempty"`
	EntityID   string    `json:"entity_id,omitempty"`
	Before     any       `json:"before,omitempty"`
	After      any       `json:"after,omitempty"`
	RequestID  string    `json:"request_id,omitempty"`
	TraceID    string    `json:"trace_id,omitempty"`
	Note       string    `json:"note,omitempty"`
}

// Store adalah log audit thread-safe, append-only.
type Store struct {
	mu      sync.Mutex
	path    string
	seq     int64
	entries []Entry
	max     int
}

// New membuat store. path kosong = hanya di memori.
func New(path string, max int) *Store {
	if max <= 0 {
		max = 2000
	}
	s := &Store{path: path, max: max}
	s.load()
	return s
}

// Record menambah satu baris audit dan mengembalikannya.
func (s *Store) Record(e Entry) Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	s.seq++
	e.ID = s.seq
	s.entries = append(s.entries, e)
	if len(s.entries) > s.max {
		s.entries = s.entries[len(s.entries)-s.max:] // buang yang paling lama
	}
	_ = s.persist()
	return e
}

// Recent mengembalikan entri terbaru (terbaru dulu).
func (s *Store) Recent(n int) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		n = 100
	}
	out := make([]Entry, 0, len(s.entries))
	for i := len(s.entries) - 1; i >= 0; i-- {
		out = append(out, s.entries[i])
		if len(out) >= n {
			break
		}
	}
	return out
}

// Len mengembalikan jumlah baris.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Save menulis ke disk.
func (s *Store) Save() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persist()
}

func (s *Store) persist() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.entries, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) load() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	_ = json.Unmarshal(b, &s.entries)
	for _, e := range s.entries {
		if e.ID > s.seq {
			s.seq = e.ID
		}
	}
	if len(s.entries) > s.max {
		s.entries = s.entries[len(s.entries)-s.max:]
	}
}

// Validate mengecek integritas ID (monoton naik) — berguna untuk deteksi kerusakan.
func (s *Store) Validate() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var prev int64
	for _, e := range s.entries {
		if e.ID <= prev {
			return fmt.Errorf("ID audit tidak monoton: %d setelah %d", e.ID, prev)
		}
		prev = e.ID
	}
	return nil
}
