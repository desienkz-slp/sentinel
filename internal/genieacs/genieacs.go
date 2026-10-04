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

// ToolNames memenuhi tool.Adapter — hanya lookup satu perangkat yang dibatasi.
func (a *Adapter) ToolNames() []string {
	return []string{"genieacs.get_device_state"}
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
// (inform < 7 hari, ambang wajar untuk TR-069). Projection hanya meminta
// _lastInform agar lookup agregat tidak menarik raw device IDs atau detail CPE.
func (a *Adapter) Ping(ctx context.Context) (PingResult, error) {
	if !a.Configured() {
		return PingResult{}, fmt.Errorf("genieacs belum dikonfigurasi (isi host NBI)")
	}
	start := time.Now()
	var devices []map[string]any
	if err := a.http.GetJSON(ctx, "/devices/?projection=_lastInform", &devices); err != nil {
		return PingResult{}, err
	}
	online := 0
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	for _, d := range devices {
		if isOnline(d, cutoff) {
			online++
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
	if name != "genieacs.get_device_state" {
		return tool.Output{}, fmt.Errorf("tool genieacs tidak dikenal atau dinonaktifkan: %s", name)
	}
	return a.getDeviceState(ctx, args)
}

const deviceStateProjection = "_lastInform,InternetGatewayDevice.DeviceInfo.Manufacturer,InternetGatewayDevice.DeviceInfo.ModelName,VirtualParameters.RXPower"

// getDeviceState reads exactly one device by its exact GenieACS device ID. The
// NBI response is bounded by limit=1 and a fixed projection; output is reduced
// again so that neither raw IDs nor arbitrary device fields can escape.
func (a *Adapter) getDeviceState(ctx context.Context, args map[string]any) (tool.Output, error) {
	deviceID := firstString(args, "device_id")
	if deviceID == "" {
		return tool.Output{}, fmt.Errorf("genieacs.get_device_state butuh device_id")
	}

	query := `{"_id":` + jsonString(deviceID) + `}`
	values := url.Values{
		"limit":      []string{"1"},
		"projection": []string{deviceStateProjection},
		"query":      []string{query},
	}
	var devices []map[string]any
	if err := a.http.GetJSON(ctx, "/devices/?"+values.Encode(), &devices); err != nil {
		return tool.Output{}, err
	}
	if len(devices) == 0 {
		return tool.Output{Data: map[string]any{"state": "UNKNOWN"}, Text: "Device tidak ditemukan di GenieACS."}, nil
	}
	if len(devices) != 1 {
		return tool.Output{}, fmt.Errorf("respons GenieACS memuat lebih dari satu device untuk lookup tunggal")
	}

	d := devices[0]
	state := safeDeviceState(d)
	return tool.Output{Data: state, Text: deviceStateText(state)}, nil
}

func safeDeviceState(d map[string]any) map[string]any {
	return map[string]any{
		"state":        onoff(isOnline(d, time.Now().Add(-7*24*time.Hour))),
		"vendor":       vendor(d),
		"model":        model(d),
		"rx_power_dbm": rxPower(d),
		"last_inform":  str(d["_lastInform"]),
	}
}

func deviceStateText(state map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "state=%s", str(state["state"]))
	for _, field := range []struct{ label, key string }{
		{"vendor", "vendor"}, {"model", "model"}, {"rx", "rx_power_dbm"}, {"last-inform", "last_inform"},
	} {
		if value := str(state[field.key]); value != "" {
			fmt.Fprintf(&b, ", %s=%s", field.label, value)
			if field.key == "rx_power_dbm" {
				b.WriteString(" dBm")
			}
		}
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

// paramValue mengambil nilai TR-069 dari struktur nested GenieACS.
// Contoh: InternetGatewayDevice.DeviceInfo.Manufacturer = {"_value":"ZTE", ...}
// VirtualParameters.RXPower = {"_value":"-23.66", ...}
func paramValue(d map[string]any, path string) string {
	parts := strings.Split(path, ".")
	cur := any(d)
	for _, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur, ok = m[p]
		if !ok {
			return ""
		}
	}
	if m, ok := cur.(map[string]any); ok {
		if v, ok := m["_value"]; ok {
			return str(v)
		}
	}
	return str(cur)
}

// vendor membaca nama manufacturer ONT.
func vendor(d map[string]any) string {
	for _, p := range []string{
		"InternetGatewayDevice.DeviceInfo.Manufacturer",
		"Device.DeviceInfo.Manufacturer",
		"InternetGatewayDevice.DeviceInfo.ManufacturerOUI",
	} {
		if v := paramValue(d, p); v != "" {
			return v
		}
	}
	return ""
}

// model membaca model ONT.
func model(d map[string]any) string {
	for _, p := range []string{
		"InternetGatewayDevice.DeviceInfo.ModelName",
		"Device.DeviceInfo.ModelName",
	} {
		if v := paramValue(d, p); v != "" {
			return v
		}
	}
	return ""
}

// rxPower membaca daya sinyal optik (dBm).
func rxPower(d map[string]any) string {
	for _, p := range []string{
		"VirtualParameters.RXPower",
		"InternetGatewayDevice.X_CMCC_EponInterfaceConfig.RXPower",
	} {
		if v := paramValue(d, p); v != "" {
			return v
		}
	}
	return ""
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
