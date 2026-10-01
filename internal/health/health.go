// Package health melacak status dependensi sistem secara terpusat.
//
// Sesuai blueprint §46-47: setiap layanan wajib punya health/ready, dan sistem
// harus tahu apakah dependensi ONLINE / DEGRADED / OFFLINE / UNKNOWN — lalu
// menurunkan kemampuannya secara anggun (graceful degradation) bila ada yang
// mati, bukan menebak status yang sebenarnya tidak diketahui.
//
// Registry ini thread-safe dan bisa dipanggil dari mana saja: LLM client,
// WhatsApp gateway, database, dan nanti adaptor Billing/RADIUS/MikroTik/GenieACS.
package health

import (
	"sort"
	"sync"
	"time"
)

// Status adalah keadaan sebuah dependensi.
type Status string

const (
	StatusOnline   Status = "ONLINE"
	StatusDegraded Status = "DEGRADED"
	StatusOffline  Status = "OFFLINE"
	StatusUnknown  Status = "UNKNOWN"
)

// Dependency adalah satu layanan yang dipantau.
type Dependency struct {
	Name        string    `json:"name"`
	Status      Status    `json:"status"`
	Detail      string    `json:"detail,omitempty"`
	LastChecked time.Time `json:"last_checked"`
	LatencyMS   int64     `json:"latency_ms,omitempty"`
}

// Registry menyimpan status seluruh dependensi.
type Registry struct {
	mu    sync.RWMutex
	deps  map[string]Dependency
	order []string
}

// New membuat registry kosong.
func New() *Registry {
	return &Registry{deps: map[string]Dependency{}}
}

// Set mencatat status sebuah dependensi (upsert).
func (r *Registry) Set(name string, status Status, detail string, latencyMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.deps[name]; !exists {
		r.order = append(r.order, name)
	}
	r.deps[name] = Dependency{
		Name:        name,
		Status:      status,
		Detail:      detail,
		LastChecked: time.Now(),
		LatencyMS:   latencyMS,
	}
}

// Get mengembalikan status satu dependensi.
func (r *Registry) Get(name string) (Dependency, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	d, ok := r.deps[name]
	return d, ok
}

// All mengembalikan seluruh dependensi dalam urutan pendaftaran.
func (r *Registry) All() []Dependency {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Dependency, 0, len(r.order))
	for _, n := range r.order {
		out = append(out, r.deps[n])
	}
	return out
}

// Overall mengembalikan status agregat seluruh sistem:
//   - bila ada yang OFFLINE dan itu dependensi kritis -> DEGRADED/OFFLINE
//   - bila semua ONLINE -> ONLINE
//   - bila ada UNKNOWN dan belum pernah dicek -> UNKNOWN
func (r *Registry) Overall() Status {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.deps) == 0 {
		return StatusUnknown
	}
	offline := false
	degraded := false
	unknown := false
	for _, d := range r.deps {
		switch d.Status {
		case StatusOffline:
			offline = true
		case StatusDegraded:
			degraded = true
		case StatusUnknown:
			unknown = true
		}
	}
	switch {
	case offline:
		return StatusDegraded // sistem masih jalan, tapi ada dependensi mati
	case degraded:
		return StatusDegraded
	case unknown:
		return StatusUnknown
	default:
		return StatusOnline
	}
}

// Names mengembalikan daftar nama dependensi (terurut untuk determinisme).
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := append([]string(nil), r.order...)
	sort.Strings(out)
	return out
}

// Summary adalah ringkasan untuk endpoint /api/health.
func (r *Registry) Summary() map[string]any {
	all := r.All()
	deps := make([]map[string]any, 0, len(all))
	for _, d := range all {
		deps = append(deps, map[string]any{
			"name":         d.Name,
			"status":       d.Status,
			"detail":       d.Detail,
			"last_checked": d.LastChecked,
			"latency_ms":   d.LatencyMS,
		})
	}
	return map[string]any{
		"status":  r.Overall(),
		"healthy": r.Overall() == StatusOnline,
		"deps":    deps,
	}
}
