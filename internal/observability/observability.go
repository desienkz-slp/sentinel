// Package observability menyediakan metrik proses, KPI konservatif, dan alert
// deterministik untuk NOC Sentinel. Paket ini tidak memanggil infrastruktur
// eksternal agar endpoint observasi selalu tetap tersedia saat dependensi gagal.
package observability

import (
	"sort"
	"sync"
	"time"
)

// RouteMetric adalah agregat HTTP per metode dan rute, tanpa data request/PII.
type RouteMetric struct {
	Method           string `json:"method"`
	Path             string `json:"path"`
	Requests         int64  `json:"requests"`
	Errors           int64  `json:"errors"`
	AverageLatencyMS int64  `json:"average_latency_ms"`
}

// Snapshot adalah metrik HTTP yang aman dipublikasikan ke dashboard.
type Snapshot struct {
	TotalRequests int64         `json:"total_requests"`
	ErrorRequests int64         `json:"error_requests"`
	Routes        []RouteMetric `json:"routes"`
}

type routeCounter struct {
	requests int64
	errors   int64
	latency  int64
}

// Collector menyimpan counter HTTP dalam memori dan aman untuk goroutine.
type Collector struct {
	mu     sync.Mutex
	routes map[string]*routeCounter
}

// NewCollector membuat collector kosong.
func NewCollector() *Collector {
	return &Collector{routes: make(map[string]*routeCounter)}
}

// RecordHTTP mencatat hasil request tanpa query string atau isi request.
func (c *Collector) RecordHTTP(method, path string, status int, duration time.Duration) {
	if c == nil {
		return
	}
	key := method + " " + path
	c.mu.Lock()
	defer c.mu.Unlock()
	route := c.routes[key]
	if route == nil {
		route = &routeCounter{}
		c.routes[key] = route
	}
	route.requests++
	if status >= 400 {
		route.errors++
	}
	route.latency += duration.Milliseconds()
}

// Snapshot mengembalikan salinan stabil yang mudah diuji dan diserialisasi.
func (c *Collector) Snapshot() Snapshot {
	if c == nil {
		return Snapshot{Routes: []RouteMetric{}}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	result := Snapshot{Routes: make([]RouteMetric, 0, len(c.routes))}
	for key, counter := range c.routes {
		metric := RouteMetric{Requests: counter.requests, Errors: counter.errors}
		for i, ch := range key {
			if ch == ' ' {
				metric.Method = key[:i]
				metric.Path = key[i+1:]
				break
			}
		}
		if counter.requests > 0 {
			metric.AverageLatencyMS = counter.latency / counter.requests
		}
		result.TotalRequests += metric.Requests
		result.ErrorRequests += metric.Errors
		result.Routes = append(result.Routes, metric)
	}
	sort.Slice(result.Routes, func(i, j int) bool {
		if result.Routes[i].Path == result.Routes[j].Path {
			return result.Routes[i].Method < result.Routes[j].Method
		}
		return result.Routes[i].Path < result.Routes[j].Path
	})
	return result
}

// CaseOutcome adalah proyeksi minimal laporan agen untuk KPI, tanpa query pelanggan.
type CaseOutcome struct {
	ElapsedMS int64
	Escalated bool
	Error     string
	Verdict   string
}

// KPI menyajikan indikator yang dapat dihitung dari laporan agen yang tersedia.
type KPI struct {
	CasesProcessed      int64 `json:"cases_processed"`
	Escalated           int64 `json:"escalated"`
	Failed              int64 `json:"failed"`
	VerifiedResolved    int64 `json:"verified_resolved"`
	AverageDiagnosisMS  int64 `json:"average_diagnosis_ms"`
	HumanEscalationRate int64 `json:"human_escalation_rate_percent"`
}

// BuildKPI membangun KPI konservatif. Laporan diagnosis belum memuat bukti
// verifikasi resolusi, sehingga verified_resolved sengaja tetap nol sampai
// case engine menyediakan catatan verification yang eksplisit.
func BuildKPI(cases []CaseOutcome) KPI {
	kpi := KPI{CasesProcessed: int64(len(cases))}
	var totalLatency int64
	for _, item := range cases {
		totalLatency += item.ElapsedMS
		if item.Escalated {
			kpi.Escalated++
		}
		if item.Error != "" {
			kpi.Failed++
		}
	}
	if kpi.CasesProcessed > 0 {
		kpi.AverageDiagnosisMS = totalLatency / kpi.CasesProcessed
		kpi.HumanEscalationRate = (kpi.Escalated * 100) / kpi.CasesProcessed
	}
	return kpi
}

// Dependency adalah proyeksi status dependensi untuk evaluasi alert.
type Dependency struct {
	Name   string
	Status string
	Detail string
}

// Alert adalah sinyal yang harus ditampilkan/diproses operator. Alert belum
// mengirim notifikasi eksternal karena kanal dan kebijakan operator belum ada.
type Alert struct {
	Severity string `json:"severity"`
	Source   string `json:"source"`
	Subject  string `json:"subject"`
	Message  string `json:"message"`
}

// BuildAlerts membuat alert dari dependensi tidak ONLINE/UNKNOWN dan kegagalan
// laporan. UNKNOWN tidak diangkat karena berarti probe awal belum selesai.
func BuildAlerts(deps []Dependency, cases []CaseOutcome) []Alert {
	alerts := make([]Alert, 0)
	for _, dep := range deps {
		if dep.Status == "ONLINE" || dep.Status == "UNKNOWN" {
			continue
		}
		severity := "warning"
		if dep.Status == "OFFLINE" {
			severity = "critical"
		}
		alerts = append(alerts, Alert{Severity: severity, Source: "dependency", Subject: dep.Name, Message: dep.Detail})
	}
	for _, item := range cases {
		if item.Error != "" {
			alerts = append(alerts, Alert{Severity: "warning", Source: "case", Subject: "diagnosis", Message: item.Error})
		}
	}
	return alerts
}
