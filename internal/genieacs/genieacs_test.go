package genieacs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestPing: parse /devices + hitung online/offline dari _lastInform.
func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/devices/" {
			t.Errorf("path = %q, mau /devices/", r.URL.Path)
		}
		recent := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05 -0700")
		old := time.Now().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05 -0700")
		w.Write([]byte(`[
			{"_id":"ONT-AAA-001","_lastInform":"` + recent + `","Device.DeviceInfo.Manufacturer":"ZTE","Device.DeviceInfo.ModelName":"F660"},
			{"_id":"ONT-BBB-002","_lastInform":"` + old + `","Device.DeviceInfo.Manufacturer":"Huawei","Device.DeviceInfo.ModelName":"HG8245"}
		]`))
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

// TestGetDeviceState: cari device by _id, tampilkan ONLINE/OFFLINE.
func TestGetDeviceState(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("query") == "" {
			t.Errorf("query param kosong")
		}
		recent := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05 -0700")
		w.Write([]byte(`[{"_id":"ONT-AAA-001","_lastInform":"` + recent + `","Device.DeviceInfo.Manufacturer":"ZTE","Device.DeviceInfo.ModelName":"F660"}]`))
	}))
	defer srv.Close()

	a := New(srv.URL, "")
	out, err := a.Invoke(context.Background(), "genieacs.get_device_state", map[string]any{"device_id": "ONT-AAA-001"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "ONT-AAA-001") || !strings.Contains(out.Text, "ONLINE") {
		t.Errorf("Text = %q, mau memuat ONT-AAA-001 ONLINE", out.Text)
	}
}

// TestGetDevicesFilter: filter online/offline.
func TestGetDevicesFilter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recent := time.Now().Add(-1 * time.Hour).Format("2006-01-02 15:04:05 -0700")
		old := time.Now().Add(-30 * 24 * time.Hour).Format("2006-01-02 15:04:05 -0700")
		w.Write([]byte(`[
			{"_id":"ONT-AAA-001","_lastInform":"` + recent + `"},
			{"_id":"ONT-BBB-002","_lastInform":"` + old + `"}
		]`))
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

// TestIsOnline: berbagai format timestamp.
func TestIsOnline(t *testing.T) {
	recent := time.Now().Add(-1 * time.Hour)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	if !isOnline(map[string]any{"_lastInform": recent.Format("2006-01-02 15:04:05 -0700")}, cutoff) {
		t.Error("recent harusnya online")
	}
	if isOnline(map[string]any{"_lastInform": recent.Format(time.RFC3339)}, cutoff) {
		// RFC3339 juga didukung -> recent = online, jadi ini harus true
	} else {
		t.Error("recent (RFC3339) harusnya online")
	}
	if isOnline(map[string]any{"_lastInform": ""}, cutoff) {
		t.Error("kosong harusnya offline")
	}
}
