// Package genieacs mengimplementasikan adapter read-only ke GenieACS NBI.
//
// Kontrak (lihat docs/genieacs-api.md):
//   - Transport : HTTP REST via NBI (genieacs-nbi), port default 7557.
//   - Query     : bahasa MongoDB (?query={...}), WAJIB URL-encoded.
//   - Auth      : NBI bawaan TIDAK punya token header standar. Bila deployment
//     memakai Basic/Bearer, isi token; default kosong (jaringan internal/VPN).
//   - Online    : dihitung dari _lastInform, bukan field boolean.
//
// Adapter ini HANYA read-only. Semua task WRITE (setParameterValues, reboot,
// factoryReset, dll.) tetap lewat gerbang policy dan dibangun terpisah.
package genieacs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"ainoc/internal/adapter"
	"ainoc/internal/tool"
)

// Adapter adalah adaptor GenieACS read-only (NBI REST).
type Adapter struct {
	http *adapter.HTTP
}

// New membuat GenieACSAdapter. baseURL = NOC_GENIEACS_URL (mis. http://10.0.0.10:7557),
// token = opsional (NBI bawaan tanpa auth; hanya diisi bila deployment pakai auth).
func New(baseURL, token string) *Adapter {
	if baseURL == "" {
		return &Adapter{http: adapter.New(adapter.Config{Domain: "genieacs"})}
	}
	return &Adapter{
		http: adapter.New(adapter.Config{
			Domain:     "genieacs",
			BaseURL:    strings.TrimRight(baseURL, "/"),
			Token:      token,
			MaxRetries: 2,
		}),
	}
}

// Domain memenuhi tool.Adapter.
func (a *Adapter) Domain() string { return "genieacs" }

// Name memenuhi tool.Adapter.
func (a *Adapter) Name() string { return "GenieACS (TR-069)" }

// Configured memenuhi tool.Adapter.
func (a *Adapter) Configured() bool { return a.http != nil && a.http.Configured() }

// ToolNames memenuhi tool.Adapter — hanya tool read-only.
func (a *Adapter) ToolNames() []string {
	return []string{
		"genieacs.get_device_state",
		"genieacs.get_devices",
	}
}

// Health memenuhi tool.Adapter: probe GET /devices (pastikan NBI menjawab).
func (a *Adapter) Health(ctx context.Context) (string, error) {
	d, err := a.Ping(ctx)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("genieacs menjawab (%d device, %d online)", d.TotalDevices, d.OnlineDevices), nil
}

// PingResult adalah hasil verifikasi koneksi GenieACS untuk dashboard.
type PingResult struct {
	BaseURL       string `json:"base_url"`
	LatencyMS     int64  `json:"ping_ms"`
	TotalDevices  int    `json:"total_devices"`
	OnlineDevices int    `json:"online_devices"`
}

// Ping memverifikasi koneksi terhadap NBI. Menghitung total device + yang online
// (inform < 7 hari, ambang wajar untuk TR-069).
func (a *Adapter) Ping(ctx context.Context) (PingResult, error) {
	if !a.Configured() {
		return PingResult{}, fmt.Errorf("genieacs belum dikonfigurasi (isi host NBI)")
	}
	start := time.Now()
	var devices []map[string]any
	if err := a.http.GetJSON(ctx, "/devices/", &devices); err != nil {
		return PingResult{}, err
	}
	online := 0
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, d := range devices {
		if li, ok := d["_lastInform"].(string); ok {
			if t, err := time.Parse("2006-01-02 15:04:05 -0700", li); err == nil && t.After(cutoff) {
				online++
			} else if err == nil {
				// format tanpa tz? coba parse ISO
			} else if t2, err2 := time.Parse(time.RFC3339, li); err2 == nil && t2.After(cutoff) {
				online++
			}
		}
	}
	return PingResult{
		BaseURL:       a.http.BaseURL(),
		LatencyMS:     time.Since(start).Milliseconds(),
		TotalDevices:  len(devices),
		OnlineDevices: online,
	}, nil
}

// Invoke memenuhi tool.Adapter.
func (a *Adapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	switch name {
	case "genieacs.get_device_state":
		return a.getDeviceState(ctx, args)
	case "genieacs.get_devices":
		return a.getDevices(ctx, args)
	default:
		return tool.Output{}, fmt.Errorf("tool genieacs tidak dikenal: %s", name)
	}
}

// getDeviceState membaca status satu device (online/offline + info dasar).
func (a *Adapter) getDeviceState(ctx context.Context, args map[string]any) (tool.Output, error) {
	deviceID := firstString(args, "device_id", "_id", "serial", "identity")
	if deviceID == "" {
		return tool.Output{}, fmt.Errorf("genieacs.get_device_state butuh device_id (serial number ONT/CPE)")
	}
	q := `{"_id":` + jsonString(deviceID) + `}`
	var devices []map[string]any
	if err := a.http.GetJSON(ctx, "/devices/?query="+url.QueryEscape(q), &devices); err != nil {
		return tool.Output{}, err
	}
	if len(devices) == 0 {
		return tool.Output{Text: fmt.Sprintf("Device %q tidak ditemukan di GenieACS.", deviceID)}, nil
	}
	d := devices[0]
	state := deviceState(d)
	return tool.Output{
		Data: d,
		Text: state,
	}, nil
}

// getDevices membaca daftar device (opsional filter online/offline).
func (a *Adapter) getDevices(ctx context.Context, args map[string]any) (tool.Output, error) {
	var devices []map[string]any
	if err := a.http.GetJSON(ctx, "/devices/", &devices); err != nil {
		return tool.Output{}, err
	}

	filter := firstString(args, "status", "filter") // "online" | "offline" | ""
	cutoff := time.Now().Add(-7 * 24 * time.Hour)

	var b strings.Builder
	count := 0
	for _, d := range devices {
		online := isOnline(d, cutoff)
		if filter == "online" && !online {
			continue
		}
		if filter == "offline" && online {
			continue
		}
		count++
		id := str(d["_id"])
		fmt.Fprintf(&b, "- %s: %s", id, onoff(online))
		if man := str(d["Device.DeviceInfo.Manufacturer"]); man != "" {
			fmt.Fprintf(&b, ", %s", man)
		}
		if model := str(d["Device.DeviceInfo.ModelName"]); model != "" {
			fmt.Fprintf(&b, " %s", model)
		}
		if li := str(d["_lastInform"]); li != "" {
			fmt.Fprintf(&b, ", inform=%s", li)
		}
		b.WriteString("\n")
	}
	if count == 0 {
		return tool.Output{Text: "Tidak ada device (atau filter tidak cocok)."}, nil
	}
	return tool.Output{Data: devices, Text: strings.TrimSpace(b.String())}, nil
}

// deviceState merangkum satu device ke teks.
func deviceState(d map[string]any) string {
	id := str(d["_id"])
	online := isOnline(d, time.Now().Add(-7*24*time.Hour))
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %s", id, onoff(online))
	if man := str(d["Device.DeviceInfo.Manufacturer"]); man != "" {
		fmt.Fprintf(&b, ", vendor=%s", man)
	}
	if model := str(d["Device.DeviceInfo.ModelName"]); model != "" {
		fmt.Fprintf(&b, ", model=%s", model)
	}
	if li := str(d["_lastInform"]); li != "" {
		fmt.Fprintf(&b, ", last-inform=%s", li)
	}
	if tags := str(d["_tags"]); tags != "" {
		fmt.Fprintf(&b, ", tags=%s", tags)
	}
	return b.String()
}

// isOnline menentukan online berdasar _lastInform (ambang cutoff).
func isOnline(d map[string]any, cutoff time.Time) bool {
	li := str(d["_lastInform"])
	if li == "" {
		return false
	}
	for _, layout := range []string{"2006-01-02 15:04:05 -0700", time.RFC3339, "2006-01-02 15:04:05"} {
		if t, err := time.Parse(layout, li); err == nil {
			return t.After(cutoff)
		}
	}
	return false
}

func str(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	b, _ := json.Marshal(v)
	return strings.Trim(string(b), `"`)
}

func onoff(online bool) string {
	if online {
		return "ONLINE"
	}
	return "OFFLINE"
}

// jsonString mengubah string jadi representasi JSON (dengan kutipan + escape).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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

var _ = json.Valid // jaga import json tetap terpakai
