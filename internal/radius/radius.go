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
	"encoding/json"
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

// ToolNames memenuhi tool.Adapter — hanya tool read-only.
func (a *Adapter) ToolNames() []string {
	return []string{
		"radius.get_session",
		"radius.get_user",
		"radius.get_system_stats",
	}
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
	case "radius.get_session":
		return a.getSession(ctx, args)
	case "radius.get_user":
		return a.getUser(ctx, args)
	case "radius.get_system_stats":
		return a.getSystemStats(ctx, args)
	default:
		return tool.Output{}, fmt.Errorf("tool radius tidak dikenal: %s", name)
	}
}

// Session adalah satu sesi PPP aktif dari /api/sessions.
type Session struct {
	RadacctID        int    `json:"radacctid"`
	AcctSessionID    string `json:"acctsessionid"`
	Username         string `json:"username"`
	NASIPAddress     string `json:"nasipaddress"`
	FramedIPAddress  string `json:"framedipaddress"`
	AcctStartTime    string `json:"acctstarttime"`
	AcctSessionTime  int    `json:"acctsessiontime"`
	AcctInputOctets  int64  `json:"acctinputoctets"`
	AcctOutputOctets int64  `json:"acctoutputoctets"`
	CallingStationID string `json:"callingstationid"`
	NASIdentifier    string `json:"nas_identifier"`
	Profile          string `json:"profile"`
}

// getSession membaca sesi aktif (semua atau satu user).
func (a *Adapter) getSession(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username")
	var sessions []Session
	if err := a.http.GetJSON(ctx, "/api/sessions", &sessions); err != nil {
		return tool.Output{}, err
	}

	var match []Session
	if identity == "" {
		match = sessions
	} else {
		for _, s := range sessions {
			if s.Username == identity || strings.Contains(s.Username, identity) {
				match = append(match, s)
			}
		}
	}

	if len(match) == 0 {
		if identity != "" {
			return tool.Output{Text: fmt.Sprintf("Tidak ada sesi aktif untuk %q.", identity)}, nil
		}
		return tool.Output{Text: "Tidak ada sesi aktif."}, nil
	}

	var b strings.Builder
	if identity != "" {
		fmt.Fprintf(&b, "Sesi aktif %q: %d\n", identity, len(match))
	} else {
		fmt.Fprintf(&b, "Total sesi aktif: %d\n", len(match))
	}
	for _, s := range match {
		fmt.Fprintf(&b, "- %s: ip=%s, nas=%s", s.Username, orDash(s.FramedIPAddress), orDash(s.NASIPAddress))
		if s.AcctSessionTime > 0 {
			fmt.Fprintf(&b, ", durasi=%s", humanDuration(s.AcctSessionTime))
		}
		fmt.Fprintf(&b, ", up=%s down=%s", humanBytes(uint64(s.AcctInputOctets)), humanBytes(uint64(s.AcctOutputOctets)))
		if s.Profile != "" {
			fmt.Fprintf(&b, ", profil=%s", s.Profile)
		}
		b.WriteString("\n")
	}
	return tool.Output{Data: match, Text: strings.TrimSpace(b.String())}, nil
}

// User adalah satu PPP user dari /api/users.
type User struct {
	Username       string  `json:"username"`
	Password       string  `json:"password"`
	Profile        string  `json:"profile"`
	NASIP          *string `json:"nas_ip"`
	LastDisconnect *string `json:"last_disconnect"`
}

// userOutput is the customer-safe projection returned by the tool. Radius may
// send a cleartext password; it must never enter tool.Output.Data.
type userOutput struct {
	Username       string  `json:"username"`
	Profile        string  `json:"profile"`
	NASIP          *string `json:"nas_ip"`
	LastDisconnect *string `json:"last_disconnect"`
	PasswordSet    bool    `json:"password_set"`
}

func projectUser(u User) userOutput {
	return userOutput{
		Username:       u.Username,
		Profile:        u.Profile,
		NASIP:          u.NASIP,
		LastDisconnect: u.LastDisconnect,
		PasswordSet:    u.Password != "",
	}
}

// getUser membaca data user PPP (semua atau satu user). Password tidak
// dicetak ke teks (rahasia) — hanya ditandai ada/tidak.
func (a *Adapter) getUser(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username")
	var users []User
	if err := a.http.GetJSON(ctx, "/api/users", &users); err != nil {
		return tool.Output{}, err
	}

	var match []User
	if identity == "" {
		match = users
	} else {
		for _, u := range users {
			if u.Username == identity || strings.Contains(u.Username, identity) {
				match = append(match, u)
			}
		}
	}

	if len(match) == 0 {
		if identity != "" {
			return tool.Output{Text: fmt.Sprintf("Tidak ada user PPP %q.", identity)}, nil
		}
		return tool.Output{Text: "Tidak ada user PPP."}, nil
	}

	var b strings.Builder
	if identity != "" {
		fmt.Fprintf(&b, "User PPP %q:\n", identity)
	} else {
		fmt.Fprintf(&b, "Total user PPP: %d\n", len(match))
	}
	for _, u := range match {
		line := fmt.Sprintf("- %s: profil=%s", u.Username, orDash(u.Profile))
		if u.NASIP != nil && *u.NASIP != "" {
			line += fmt.Sprintf(", nas=%s", *u.NASIP)
		}
		if u.Password != "" {
			line += ", password=Tersimpan"
		}
		if u.LastDisconnect != nil && *u.LastDisconnect != "" {
			line += fmt.Sprintf(", last-disconnect=%s", *u.LastDisconnect)
		}
		b.WriteString(line + "\n")
	}
	projected := make([]userOutput, 0, len(match))
	for _, u := range match {
		projected = append(projected, projectUser(u))
	}
	return tool.Output{Data: projected, Text: strings.TrimSpace(b.String())}, nil
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

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// humanBytes mengubah byte ke bentuk terbaca (KB/MB/GB/TB).
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// humanDuration mengubah detik ke bentuk terbaca (1h2m3s).
func humanDuration(sec int) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	m := sec / 60
	if m < 60 {
		return fmt.Sprintf("%dm%ds", m, sec%60)
	}
	h := m / 60
	return fmt.Sprintf("%dh%dm", h, m%60)
}

var _ = json.Valid // jaga import json tetap terpakai bila struktur berubah
