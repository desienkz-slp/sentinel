// Package incident menyimpan riwayat insiden secara terstruktur.
//
// Ini adalah fondasi "Episodic Memory" dari blueprint. Untuk tahap persiapan,
// penyimpanan memakai JSON file (aman tanpa PostgreSQL berjalan). Skema sudah
// disiapkan untuk migrasi ke PostgreSQL (migrations/001_autonomous_noc.sql)
// tanpa mengubah kontrak data.
package incident

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

// Status akhir sebuah insiden (blueprint §71).
type Status string

const (
	StatusOpen          Status = "OPEN"
	StatusInvestigating Status = "INVESTIGATING"
	StatusResolved      Status = "RESOLVED"
	StatusEscalated     Status = "ESCALATED"
	StatusWaiting       Status = "WAITING"
	StatusClosed        Status = "CLOSED"
	StatusUnknown       Status = "UNKNOWN"
)

// Topology menyimpan metadata infrastruktur yang sudah diverifikasi oleh sumber
// sistem. Field kosong berarti data tidak tersedia dan tidak boleh ditebak.
type Topology struct {
	Area     string `json:"area,omitempty"`
	Router   string `json:"router,omitempty"`
	OLT      string `json:"olt,omitempty"`
	PON      string `json:"pon,omitempty"`
	Upstream string `json:"upstream,omitempty"`
}

// CorrelationKind menjelaskan alasan sebuah diagnosis tidak perlu dijalankan lagi.
type CorrelationKind string

const (
	CorrelationNone      CorrelationKind = "NONE"
	CorrelationDuplicate CorrelationKind = "DUPLICATE"
	CorrelationMass      CorrelationKind = "MASS_INCIDENT"
)

// CorrelationPolicy membatasi jendela dan ambang grouping secara deterministik.
type CorrelationPolicy struct {
	Window        time.Duration
	MassThreshold int
}

// MassIncident adalah ringkasan gangguan yang berdampak pada banyak pelanggan.
type MassIncident struct {
	ID                string    `json:"id"`
	Intent            string    `json:"intent"`
	Topology          Topology  `json:"topology"`
	StartedAt         time.Time `json:"started_at"`
	AffectedCustomers int       `json:"affected_customers"`
	IncidentIDs       []string  `json:"incident_ids"`
}

// CorrelationResult adalah hasil pencatatan yang dipakai orkestrator untuk
// menekan diagnosis redundan ketika insiden sudah terkait.
type CorrelationResult struct {
	Incident   Incident        `json:"incident"`
	Kind       CorrelationKind `json:"kind"`
	Suppressed bool            `json:"suppressed"`
}

// Incident adalah satu rekaman insiden.
type Incident struct {
	ID            string    `json:"id"`
	Status        Status    `json:"status"`
	Severity      string    `json:"severity,omitempty"`
	Topology      Topology  `json:"topology,omitempty"`
	CorrelationID string    `json:"correlation_id,omitempty"`
	Source        string    `json:"source,omitempty"` // whatsapp | dashboard | monitoring
	Identity      string    `json:"identity"`         // nomor pengirim ternormalisasi
	Intent        string    `json:"intent,omitempty"`
	Query         string    `json:"query,omitempty"`
	Target        string    `json:"target,omitempty"`
	Verdict       string    `json:"verdict,omitempty"`
	Confidence    float64   `json:"confidence,omitempty"`
	RootCause     string    `json:"root_cause,omitempty"`
	Evidence      []string  `json:"evidence,omitempty"`
	Missing       []string  `json:"missing,omitempty"`
	Alternatives  []string  `json:"alternatives,omitempty"`
	Actions       []string  `json:"actions,omitempty"`
	EscalatedTo   string    `json:"escalated_to,omitempty"`
	StartedAt     time.Time `json:"started_at"`
	ClosedAt      time.Time `json:"closed_at,omitempty"`
	TraceID       string    `json:"trace_id,omitempty"`
	RequestID     string    `json:"request_id,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

// Store adalah penyimpanan insiden thread-safe.
type Store struct {
	mu   sync.Mutex
	path string
	list []Incident
	max  int
}

// New membuat store. path kosong = hanya di memori (tidak dipersist).
func New(path string, max int) *Store {
	if max <= 0 {
		max = 500
	}
	s := &Store{path: path, max: max}
	s.load()
	return s
}

// Add menyimpan insiden baru dan menutup bila melebihi kapasitas.
func (s *Store) Add(inc Incident) Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	if inc.ID == "" {
		inc.ID = fmt.Sprintf("INC-%d", time.Now().UnixNano())
	}
	if inc.CreatedAt.IsZero() {
		inc.CreatedAt = time.Now()
	}
	if inc.StartedAt.IsZero() {
		inc.StartedAt = inc.CreatedAt
	}
	s.list = append(s.list, inc)
	if len(s.list) > s.max {
		s.list = s.list[len(s.list)-s.max:]
	}
	_ = s.persist()
	return inc
}

// Recent mengembalikan insiden terbaru (terbaru dulu), dibatasi n.
func (s *Store) Recent(n int) []Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n <= 0 {
		n = 50
	}
	out := make([]Incident, 0, len(s.list))
	for i := len(s.list) - 1; i >= 0; i-- {
		out = append(out, s.list[i])
		if len(out) >= n {
			break
		}
	}
	return out
}

// ForIdentity mengembalikan insiden untuk satu nomor pengirim.
func (s *Store) ForIdentity(identity string) []Incident {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.TrimSpace(identity)
	var out []Incident
	for _, inc := range s.list {
		if strings.TrimSpace(inc.Identity) == key {
			out = append(out, inc)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

// Len mengembalikan jumlah insiden tersimpan.
func (s *Store) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.list)
}

// Save menulis ke disk sekarang.
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
	b, err := json.MarshalIndent(s.list, "", "  ")
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
	_ = json.Unmarshal(b, &s.list)
	if len(s.list) > s.max {
		s.list = s.list[len(s.list)-s.max:]
	}
}
