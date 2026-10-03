package observability

import (
	"sort"
	"sync"
	"time"
)

// TeamDecision = satu keputusan pemilahan pesan. Sengaja TIDAK memuat nomor,
// nama, username, atau isi pesan — hanya label kategori dan angka.
type TeamDecision struct {
	Team      string        // cs | noc | none
	Handler   string        // status_integrasi | daftar_pelanggan | cek_billing | ... | llm
	HandledBy string        // code | llm
	Tools     []string      // nama tool yang dipanggil (tanpa argumen)
	Latency   time.Duration // lama menangani
	OK        bool          // selesai tanpa error
	Mode      string        // off | shadow | on (mode router saat itu)
}

// TeamMetric adalah agregat per (tim, handler, handledBy).
type TeamMetric struct {
	Team             string `json:"team"`
	Handler          string `json:"handler"`
	HandledBy        string `json:"handled_by"`
	Count            int64  `json:"count"`
	Errors           int64  `json:"errors"`
	AverageLatencyMS int64  `json:"average_latency_ms"`
}

// TeamSnapshot aman dipublikasikan ke dashboard.
type TeamSnapshot struct {
	Total          int64            `json:"total"`
	HandledBy      map[string]int64 `json:"handled_by"` // code | llm
	ByTeam         map[string]int64 `json:"by_team"`
	ToolCalls      map[string]int64 `json:"tool_calls"`
	CodeShare      float64          `json:"code_share"` // 0..1, bagian yang ditangani kode
	Rows           []TeamMetric     `json:"rows"`
	Mismatches     int64            `json:"shadow_mismatches"`
	ScopeDenied    int64            `json:"scope_denied"`
	ScopeRewritten int64            `json:"scope_rewritten"`
	Redactions     int64            `json:"reply_redactions"`
}

type teamKey struct{ team, handler, by string }
type teamCounter struct{ n, errs, lat int64 }

// TeamCollector aman untuk goroutine; nil aman dipakai (no-op).
type TeamCollector struct {
	mu                          sync.Mutex
	rows                        map[teamKey]*teamCounter
	tools                       map[string]int64
	mismatches                  int64
	scopeDenied, scopeRewritten int64
	redactions                  int64
}

// NewTeamCollector membuat collector kosong.
func NewTeamCollector() *TeamCollector {
	return &TeamCollector{rows: map[teamKey]*teamCounter{}, tools: map[string]int64{}}
}

// Record mencatat satu keputusan.
func (c *TeamCollector) Record(d TeamDecision) {
	if c == nil {
		return
	}
	k := teamKey{d.Team, d.Handler, d.HandledBy}
	c.mu.Lock()
	defer c.mu.Unlock()
	row := c.rows[k]
	if row == nil {
		row = &teamCounter{}
		c.rows[k] = row
	}
	row.n++
	if !d.OK {
		row.errs++
	}
	row.lat += d.Latency.Milliseconds()
	for _, t := range d.Tools {
		c.tools[t]++
	}
}

// RecordMismatch mencatat selisih mode shadow (keputusan router != jalur nyata).
func (c *TeamCollector) RecordMismatch() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.mismatches++
	c.mu.Unlock()
}

// RecordScope mencatat satu kejadian pembatas tim CS.
func (c *TeamCollector) RecordScope(allowed, rewritten bool) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if !allowed {
		c.scopeDenied++
	}
	if rewritten {
		c.scopeRewritten++
	}
}

// RecordRedaction mencatat satu balasan yang disunting penyaring presentasi.
func (c *TeamCollector) RecordRedaction(kategori int) {
	if c == nil || kategori <= 0 {
		return
	}
	c.mu.Lock()
	c.redactions++
	c.mu.Unlock()
}

// Snapshot mengembalikan salinan stabil.
func (c *TeamCollector) Snapshot() TeamSnapshot {
	out := TeamSnapshot{
		HandledBy: map[string]int64{}, ByTeam: map[string]int64{},
		ToolCalls: map[string]int64{}, Rows: []TeamMetric{},
	}
	if c == nil {
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, v := range c.rows {
		m := TeamMetric{Team: k.team, Handler: k.handler, HandledBy: k.by, Count: v.n, Errors: v.errs}
		if v.n > 0 {
			m.AverageLatencyMS = v.lat / v.n
		}
		out.Total += v.n
		out.HandledBy[k.by] += v.n
		out.ByTeam[k.team] += v.n
		out.Rows = append(out.Rows, m)
	}
	for t, n := range c.tools {
		out.ToolCalls[t] = n
	}
	out.Mismatches = c.mismatches
	out.ScopeDenied, out.ScopeRewritten = c.scopeDenied, c.scopeRewritten
	out.Redactions = c.redactions
	if out.Total > 0 {
		out.CodeShare = float64(out.HandledBy["code"]) / float64(out.Total)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		a, b := out.Rows[i], out.Rows[j]
		if a.Team != b.Team {
			return a.Team < b.Team
		}
		if a.Handler != b.Handler {
			return a.Handler < b.Handler
		}
		return a.HandledBy < b.HandledBy
	})
	return out
}
