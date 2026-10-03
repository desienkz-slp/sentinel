package customtool

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Store menyimpan spec tool custom (draft + active + rejected), dipersist atomik.
type Store struct {
	mu    sync.Mutex
	path  string
	items map[string]Spec // key = name
	dirty bool
}

// NewStore membuat store. path kosong = in-memory only.
func NewStore(path string) *Store {
	s := &Store{path: path, items: map[string]Spec{}}
	s.Load()
	return s
}

func (s *Store) Load() {
	if s.path == "" {
		return
	}
	b, err := os.ReadFile(s.path)
	if err != nil {
		return
	}
	var doc struct {
		Tools map[string]Spec `json:"tools"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return
	}
	s.mu.Lock()
	if doc.Tools != nil {
		s.items = doc.Tools
	}
	s.mu.Unlock()
}

func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	s.mu.Lock()
	doc := struct {
		Tools   map[string]Spec `json:"tools"`
		SavedAt time.Time       `json:"saved_at"`
	}{Tools: s.items, SavedAt: time.Now()}
	s.dirty = false
	s.mu.Unlock()

	b, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Propose menyimpan spec sebagai draft. Validasi dulu; nama bentrok = ditolak.
func (s *Store) Propose(spec Spec, proposedBy string) error {
	if err := Validate(spec); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	name := strings.TrimSpace(spec.Name)
	if _, exists := s.items[name]; exists {
		return fmtToolError("tool %q sudah ada", name)
	}
	spec.Status = StatusDraft
	spec.ProposedBy = strings.TrimSpace(proposedBy)
	spec.ProposedAt = time.Now().UTC()
	s.items[name] = spec
	s.dirty = true
	return nil
}

// Approve mengaktifkan draft (hanya superadmin). Idempotent terhadap active.
func (s *Store) Approve(name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.items[name]
	if !ok {
		return fmtToolError("tool %q tidak ditemukan", name)
	}
	if sp.Status == StatusActive {
		return nil
	}
	now := time.Now().UTC()
	sp.Status = StatusActive
	sp.ApprovedAt = &now
	s.items[name] = sp
	s.dirty = true
	return nil
}

// Reject menandai draft ditolak (superadmin), tetap tersimpan untuk audit.
func (s *Store) Reject(name, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.items[name]
	if !ok {
		return fmtToolError("tool %q tidak ditemukan", name)
	}
	sp.Status = StatusRejected
	sp.Reason = strings.TrimSpace(reason)
	s.items[name] = sp
	s.dirty = true
	return nil
}

// Get mengembalikan spec berdasarkan nama.
func (s *Store) Get(name string) (Spec, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp, ok := s.items[name]
	return sp, ok
}

// Active mengembalikan spec ber-status active (urutan nama).
func (s *Store) Active() []Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Spec, 0)
	for _, sp := range s.items {
		if sp.Status == StatusActive {
			out = append(out, sp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Drafts mengembalikan spec ber-status draft (menunggu approval superadmin).
func (s *Store) Drafts() []Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Spec, 0)
	for _, sp := range s.items {
		if sp.Status == StatusDraft {
			out = append(out, sp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// All mengembalikan semua spec (untuk dashboard).
func (s *Store) All() []Spec {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Spec, 0, len(s.items))
	for _, sp := range s.items {
		out = append(out, sp)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SaveIfDirty menulis ke disk bila ada perubahan.
func (s *Store) SaveIfDirty() error {
	s.mu.Lock()
	d := s.dirty
	s.mu.Unlock()
	if !d {
		return nil
	}
	return s.Save()
}

func (s *Store) Stats() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	draft, active, rejected := 0, 0, 0
	for _, sp := range s.items {
		switch sp.Status {
		case StatusDraft:
			draft++
		case StatusActive:
			active++
		case StatusRejected:
			rejected++
		}
	}
	return map[string]any{"draft": draft, "active": active, "rejected": rejected, "tersimpan": s.path != ""}
}

func fmtToolError(format string, args ...any) error {
	return &ToolError{msg: fmt.Sprintf(format, args...)}
}

type ToolError struct{ msg string }

func (e *ToolError) Error() string { return e.msg }
