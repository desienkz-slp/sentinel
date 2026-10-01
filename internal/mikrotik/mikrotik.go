// Package mikrotik mengimplementasikan adapter read-only ke router MikroTik.
//
// Kontrak (lihat docs/mikrotik-api.md):
//   - Transport : RouterOS v7.1+ REST API di /rest (service www / www-ssl)
//   - Auth      : HTTP Basic Auth (username+password user router), BUKAN API key
//   - TLS       : default MikroTik memakai sertifikat self-signed → InsecureTLS
//     (setara curl -k) untuk jaringan terpercaya.
//
// Adapter ini HANYA read-only. Tindakan WRITE (mis. disconnect PPPoE) tetap
// lewat gerbang policy APPROVAL_REQUIRED dan dibangun terpisah.
package mikrotik

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ainoc/internal/adapter"
	"ainoc/internal/tool"
)

// Adapter adalah adaptor MikroTik read-only (REST API RouterOS v7).
type Adapter struct {
	http *adapter.HTTP
}

// Config adalah parameter koneksi MikroTik (dari config.json / env).
type Config struct {
	Host string // tanpa skema/port, mis. 192.168.88.1
	Port int    // 0 = default (443 bila TLS, 80 bila tidak)
	User string
	Pass string
	TLS  bool
}

// New membuat MikrotikAdapter. Base URL dibangun: http(s)://host[:port]/rest.
func New(c Config) *Adapter {
	if c.Host == "" || c.User == "" {
		return &Adapter{http: adapter.New(adapter.Config{Domain: "mikrotik"})}
	}
	scheme := "http"
	if c.TLS {
		scheme = "https"
	}
	port := c.Port
	if port == 0 {
		if c.TLS {
			port = 443
		} else {
			port = 80
		}
	}
	base := fmt.Sprintf("%s://%s:%d/rest", scheme, strings.TrimSpace(c.Host), port)
	return &Adapter{
		http: adapter.New(adapter.Config{
			Domain:      "mikrotik",
			BaseURL:     base,
			BasicUser:   c.User,
			BasicPass:   c.Pass,
			InsecureTLS: c.TLS, // sertifikat self-signed MikroTik
			MaxRetries:  2,
		}),
	}
}

// Domain memenuhi tool.Adapter.
func (a *Adapter) Domain() string { return "mikrotik" }

// Name memenuhi tool.Adapter.
func (a *Adapter) Name() string { return "MikroTik RouterOS" }

// Configured memenuhi tool.Adapter.
func (a *Adapter) Configured() bool { return a.http != nil && a.http.Configured() }

// ToolNames memenuhi tool.Adapter — hanya tool read-only.
func (a *Adapter) ToolNames() []string { return []string{"mikrotik.get_pppoe_status"} }

// Health memenuhi tool.Adapter: probe GET /system/resource (identitas router).
func (a *Adapter) Health(ctx context.Context) (string, error) {
	d, err := a.Ping(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("mikrotik menjawab (v%s, uptime %s)", d.Version, d.Uptime), nil
}

// PingResult adalah hasil verifikasi koneksi MikroTik untuk dashboard.
type PingResult struct {
	BaseURL   string `json:"base_url"`
	LatencyMS int64  `json:"ping_ms"`
	Version   string `json:"version"`
	BoardName string `json:"board_name"`
	Uptime    string `json:"uptime"`
	CPU       string `json:"cpu_load"`
}

// Ping memverifikasi koneksi + auth terhadap RouterOS (tanpa membocorkan pass).
func (a *Adapter) Ping(ctx context.Context) (PingResult, error) {
	if !a.Configured() {
		return PingResult{}, fmt.Errorf("mikrotik belum dikonfigurasi (isi host + username + password)")
	}
	start := time.Now()
	var res []map[string]string
	if err := a.http.GetJSON(ctx, "/system/resource", &res); err != nil {
		return PingResult{}, err
	}
	r := first(res)
	return PingResult{
		BaseURL:   a.http.BaseURL(),
		LatencyMS: time.Since(start).Milliseconds(),
		Version:   r["version"],
		BoardName: r["board-name"],
		Uptime:    r["uptime"],
		CPU:       r["cpu-load"],
	}, nil
}

// Invoke memenuhi tool.Adapter.
func (a *Adapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	switch name {
	case "mikrotik.get_pppoe_status":
		return a.getPPPoEStatus(ctx, args)
	default:
		return tool.Output{}, fmt.Errorf("tool mikrotik tidak dikenal: %s", name)
	}
}

// getPPPoEStatus membaca sesi PPPoE aktif dari /ppp/active lalu mencocokkan
// username (name) dengan identitas yang dicari.
func (a *Adapter) getPPPoEStatus(ctx context.Context, args map[string]any) (tool.Output, error) {
	identity := firstString(args, "identity", "username", "name")
	if identity == "" {
		return tool.Output{}, fmt.Errorf("mikrotik.get_pppoe_status butuh identity (username PPPoE / nomor pelanggan)")
	}

	var active []map[string]string
	if err := a.http.GetJSON(ctx, "/ppp/active", &active); err != nil {
		return tool.Output{}, err
	}

	// Cocokkan: nama persis, lalu contains (nomor pelanggan bisa jadi bagian username).
	var match []map[string]string
	for _, s := range active {
		name := s["name"]
		if name == identity || strings.Contains(name, identity) {
			match = append(match, s)
		}
	}

	if len(match) == 0 {
		return tool.Output{
			Text: fmt.Sprintf("PPPoE %q TIDAK aktif (tidak ada sesi di /ppp/active).", identity),
		}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Sesi PPPoE aktif untuk %q: %d\n", identity, len(match))
	for _, s := range match {
		fmt.Fprintf(&b, "- %s: service=%s", s["name"], orDash(s["service"]))
		if s["caller-id"] != "" {
			fmt.Fprintf(&b, ", caller-id=%s", s["caller-id"])
		}
		if s["address"] != "" {
			fmt.Fprintf(&b, ", ip=%s", s["address"])
		}
		if s["uptime"] != "" {
			fmt.Fprintf(&b, ", uptime=%s", s["uptime"])
		}
		b.WriteString("\n")
	}

	return tool.Output{
		Data: match,
		Text: strings.TrimSpace(b.String()),
	}, nil
}

func first(m []map[string]string) map[string]string {
	if len(m) == 0 {
		return map[string]string{}
	}
	return m[0]
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
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

var _ = json.Valid // jaga import json tetap terpakai bila struktur berubah
