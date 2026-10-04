// Package radius mengimplementasikan adapter read-only ke NETORA Radius UI.
//
// Kontrak (lihat radius_api.md):
//   - Transport : HTTP REST (server.js Radius UI), BUKAN protokol RADIUS UDP.
//   - Auth      : header Authorization: Bearer <API_TOKEN> (machine token).
//   - Endpoint  : /api/* (users, sessions, logs, traffic, profiles, fup, nas,
//     system/stats, tunnels, vpn).
//
// Adapter ini HANYA read-only. Semua tindakan WRITE (disable/kick/dll.) tetap
// lewat gerbang policy APPROVAL_REQUIRED dan dibangun terpisah.
package radius

import (
	"context"
	"fmt"
	"strings"
	"time"

	"ainoc/internal/adapter"
	"ainoc/internal/tool"
)

// Adapter adalah adaptor Radius UI read-only (HTTP REST).
type Adapter struct {
	http *adapter.HTTP
}

// New membuat RadiusAdapter. baseURL = NOC_RADIUS_URL (mis. http://192.168.1.100:3001),
// token = NOC_RADIUS_TOKEN (API_TOKEN machine). Tidak ada base path tambahan.
func New(baseURL, token string) *Adapter {
	if baseURL == "" {
		return &Adapter{http: adapter.New(adapter.Config{Domain: "radius"})}
	}
	return &Adapter{
		http: adapter.New(adapter.Config{
			Domain:     "radius",
			BaseURL:    strings.TrimRight(baseURL, "/"),
			Token:      token,
			MaxRetries: 2,
		}),
	}
}

// Domain memenuhi tool.Adapter.
func (a *Adapter) Domain() string { return "radius" }

// Name memenuhi tool.Adapter.
func (a *Adapter) Name() string { return "NETORA Radius UI" }

// Configured memenuhi tool.Adapter.
func (a *Adapter) Configured() bool { return a.http != nil && a.http.Configured() }

// ToolNames memenuhi tool.Adapter. NETORA only documents bulk /api/sessions
// and /api/users endpoints; no exact server-side filtering/projection exists.
// Do not expose those PII-bearing bulk paths through this adapter.
func (a *Adapter) ToolNames() []string {
	return []string{"radius.get_system_stats"}
}

// Health memenuhi tool.Adapter: probe GET /api/system/stats (kesehatan server).
func (a *Adapter) Health(ctx context.Context) (string, error) {
	d, err := a.Ping(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("radius menjawab (cpu %s%%, %d user, db %s)", d.CPULoad, d.UserCount, d.DBSizeMB), nil
}

// PingResult adalah hasil verifikasi koneksi Radius UI untuk dashboard.
type PingResult struct {
	BaseURL   string `json:"base_url"`
	LatencyMS int64  `json:"ping_ms"`
	CPULoad   string `json:"cpu_load"`
	MemUsed   string `json:"memory_used_percent"`
	UserCount int    `json:"user_count"`
	DBSizeMB  string `json:"db_size_mb"`
}

// Ping memverifikasi koneksi + auth terhadap Radius UI (tanpa membocorkan token).
func (a *Adapter) Ping(ctx context.Context) (PingResult, error) {
	if !a.Configured() {
		return PingResult{}, fmt.Errorf("radius belum dikonfigurasi (isi host + API token)")
	}
	start := time.Now()
	var s struct {
		CPU struct {
			Load string `json:"load"`
		} `json:"cpu"`
		Memory struct {
			UsedPercent string `json:"usedPercent"`
		} `json:"memory"`
		Database struct {
			SizeMB string `json:"sizeMB"`
		} `json:"database"`
		RadiusData struct {
			RadcheckCount int `json:"radcheckCount"`
		} `json:"radiusData"`
	}
	if err := a.http.GetJSON(ctx, "/api/system/stats", &s); err != nil {
		return PingResult{}, err
	}
	return PingResult{
		BaseURL:   a.http.BaseURL(),
		LatencyMS: time.Since(start).Milliseconds(),
		CPULoad:   s.CPU.Load,
		MemUsed:   s.Memory.UsedPercent,
		UserCount: s.RadiusData.RadcheckCount,
		DBSizeMB:  s.Database.SizeMB,
	}, nil
}

// Invoke memenuhi tool.Adapter.
func (a *Adapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	switch name {
	case "radius.get_system_stats":
		return a.getSystemStats(ctx, args)
	default:
		return tool.Output{}, fmt.Errorf("tool radius tidak dikenal: %s", name)
	}
}

// getSystemStats membaca statistik server Radius UI.
func (a *Adapter) getSystemStats(ctx context.Context, args map[string]any) (tool.Output, error) {
	d, err := a.Ping(ctx)
	if err != nil {
		return tool.Output{}, err
	}
	text := fmt.Sprintf("Radius UI: cpu=%s%%, ram=%s%%, user=%d, db=%s",
		orDash(d.CPULoad), orDash(d.MemUsed), d.UserCount, orDash(d.DBSizeMB))
	return tool.Output{Data: d, Text: text}, nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
