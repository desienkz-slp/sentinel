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
	"strings"
	"sync"
	"time"
)

// Entry adalah satu baris audit.
type Entry struct {
	ID              int64     `json:"id"`
	EventType       string    `json:"event_type"`
	OccurredAt      time.Time `json:"occurred_at"`
	Actor           string    `json:"actor"`
	CaseID          string    `json:"case_id,omitempty"`
	Agent           string    `json:"agent,omitempty"`
	Model           string    `json:"model,omitempty"`
	PromptVersion   string    `json:"prompt_version,omitempty"`
	Tool            string    `json:"tool,omitempty"`
	Arguments       any       `json:"arguments,omitempty"`
	Result          any       `json:"result,omitempty"`
	RiskLevel       string    `json:"risk_level,omitempty"`
	PolicyDecision  string    `json:"policy_decision,omitempty"`
	ExecutionStatus string    `json:"execution_status,omitempty"`
	Verification    string    `json:"verification,omitempty"`
	HumanApproval   string    `json:"human_approval,omitempty"`
	Error           string    `json:"error,omitempty"`
	EntityType      string    `json:"entity_type,omitempty"`
	EntityID        string    `json:"entity_id,omitempty"`
	Before          any       `json:"before,omitempty"`
	After           any       `json:"after,omitempty"`
	RequestID       string    `json:"request_id,omitempty"`
	TraceID         string    `json:"trace_id,omitempty"`
	Note            string    `json:"note,omitempty"`
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
	e.Arguments = sanitize(e.Arguments)
	e.Result = sanitize(e.Result)
	e.Before = sanitize(e.Before)
	e.After = sanitize(e.After)
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
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// sanitize menyalin data audit melalui JSON dan meredaksi key sensitif secara
// rekursif. Caller tidak boleh mengandalkan audit sebagai tempat menyimpan rahasia.
func sanitize(value any) any {
	if value == nil {
		return nil
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "[TIDAK_DAPAT_DIAUDIT]"
	}
	var copied any
	if err := json.Unmarshal(b, &copied); err != nil {
		return "[TIDAK_DAPAT_DIAUDIT]"
	}
	return redact(copied)
}

func redact(value any) any {
	switch data := value.(type) {
	case map[string]any:
		for key, item := range data {
			if sensitiveKey(key) {
				data[key] = "[REDACTED]"
				continue
			}
			data[key] = redact(item)
		}
	case []any:
		for i, item := range data {
			data[i] = redact(item)
		}
	}
	return value
}

func sensitiveKey(key string) bool {
	key = strings.ToLower(strings.ReplaceAll(key, "-", "_"))
	return strings.Contains(key, "password") || strings.Contains(key, "passwd") ||
		strings.Contains(key, "secret") || strings.Contains(key, "token") ||
		strings.Contains(key, "api_key") || strings.Contains(key, "authorization")
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
