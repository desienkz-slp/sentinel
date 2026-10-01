package genieacs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// deviceJSON membangun satu device dengan struktur nested GenieACS asli.
func deviceJSON(id string, lastInform time.Time, rxPower string) string {
	return `{"_id":"` + id + `",` +
		`"_lastInform":"` + lastInform.Format(time.RFC3339) + `",` +
		`"InternetGatewayDevice":{"DeviceInfo":{"Manufacturer":{"_value":"ZTE"},"ModelName":{"_value":"HG8245H5"}}},` +
		`"VirtualParameters":{"RXPower":{"_value":"` + rxPower + `"}}}`
}

// TestPing: parse /devices + hitung online/offline dari _lastInform (RFC3339).
func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/devices/" {
			t.Errorf("path = %q, mau /devices/", r.URL.Path)
		}
		recent := deviceJSON("ONT-AAA-001", time.Now().Add(-1*time.Hour), "-23.66")
		old := deviceJSON("ONT-BBB-002", time.Now().Add(-30*24*time.Hour), "-28.00")
		w.Write([]byte(`[` + recent + `,` + old + `]`))
	}))
	defer srv.Close()

	a := New(srv.URL, "")
	d, err := a.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping error: %v", err)
	}
	if d.TotalDevices != 2 {
		t.Errorf("TotalDevices = %d, mau 2", d.TotalDevices)
	}
	if d.OnlineDevices != 1 {
		t.Errorf("OnlineDevices = %d, mau 1 (hanya ONT-AAA yang inform < 7 hari)", d.OnlineDevices)
	}
}

// TestGetDeviceState: cari device by _id, tampilkan ONLINE + vendor + model + RXPower.
func TestGetDeviceState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "" {
			t.Errorf("query param kosong")
		}
		recent := deviceJSON("ONT-AAA-001", time.Now().Add(-1*time.Hour), "-23.66")
		w.Write([]byte(`[` + recent + `]`))
	}))
	defer srv.Close()

	a := New(srv.URL, "")
	out, err := a.Invoke(context.Background(), "genieacs.get_device_state", map[string]any{"device_id": "ONT-AAA-001"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	for _, want := range []string{"ONT-AAA-001", "ONLINE", "ZTE", "HG8245H5", "-23.66 dBm"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("Text = %q, mau memuat %q", out.Text, want)
		}
	}
}

// TestGetDevicesFilter: filter online/offline (projection _id,_lastInform).
func TestGetDevicesFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recent := deviceJSON("ONT-AAA-001", time.Now().Add(-1*time.Hour), "-23.66")
		old := deviceJSON("ONT-BBB-002", time.Now().Add(-30*24*time.Hour), "-28.00")
		w.Write([]byte(`[` + recent + `,` + old + `]`))
	}))
	defer srv.Close()

	a := New(srv.URL, "")
	out, err := a.Invoke(context.Background(), "genieacs.get_devices", map[string]any{"status": "online"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "ONT-AAA-001") {
		t.Errorf("Text = %q, mau memuat ONT-AAA-001 (online)", out.Text)
	}
	if strings.Contains(out.Text, "ONT-BBB-002") {
		t.Errorf("Text = %q, tidak boleh memuat ONT-BBB-002 (offline)", out.Text)
	}
}

// TestConfigured & ToolNames.
func TestConfiguredAndToolNames(t *testing.T) {
	if New("", "").Configured() {
		t.Error("Configured = true tanpa URL")
	}
	a := New("http://x", "")
	if a.Domain() != "genieacs" {
		t.Errorf("Domain = %q, mau genieacs", a.Domain())
	}
	names := a.ToolNames()
	want := []string{"genieacs.get_device_state", "genieacs.get_devices"}
	if len(names) != len(want) {
		t.Fatalf("ToolNames = %v, mau %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("ToolNames[%d] = %q, mau %q", i, names[i], want[i])
		}
	}
}

// TestIsOnline: RFC3339 dan format lama.
func TestIsOnline(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	if !isOnline(map[string]any{"_lastInform": recent.Format(time.RFC3339)}, cutoff) {
		t.Error("recent (RFC3339) harusnya online")
	}
	if isOnline(map[string]any{"_lastInform": ""}, cutoff) {
		t.Error("kosong harusnya offline")
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	if isOnline(map[string]any{"_lastInform": old.Format(time.RFC3339)}, cutoff) {
		t.Error("30 hari lalu harusnya offline")
	}
}

// TestParamValue: baca nested _value.
func TestParamValue(t *testing.T) {
	d := map[string]any{
		"InternetGatewayDevice": map[string]any{
			"DeviceInfo": map[string]any{
				"Manufacturer": map[string]any{"_value": "ZTE"},
			},
		},
		"VirtualParameters": map[string]any{
			"RXPower": map[string]any{"_value": "-23.66"},
		},
	}
	if got := paramValue(d, "InternetGatewayDevice.DeviceInfo.Manufacturer"); got != "ZTE" {
		t.Errorf("paramValue = %q, mau ZTE", got)
	}
	if got := paramValue(d, "VirtualParameters.RXPower"); got != "-23.66" {
		t.Errorf("paramValue = %q, mau -23.66", got)
	}
	if got := paramValue(d, "tidak.ada"); got != "" {
		t.Errorf("paramValue path tak ada = %q, mau kosong", got)
	}
	if got := rxPower(d); got != "-23.66" {
		t.Errorf("rxPower = %q, mau -23.66", got)
	}
	if got := vendor(d); got != "ZTE" {
		t.Errorf("vendor = %q, mau ZTE", got)
	}
	if got := model(d); got != "" {
		t.Errorf("model = %q, mau kosong (tidak ada ModelName)", got)
	}
}
