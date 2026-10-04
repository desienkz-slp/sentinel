package genieacs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func deviceJSON(id string, lastInform time.Time, rxPower string) string {
	return `{"_id":"` + id + `",` +
		`"_lastInform":"` + lastInform.Format(time.RFC3339) + `",` +
		`"InternetGatewayDevice":{"DeviceInfo":{"Manufacturer":{"_value":"ZTE"},"ModelName":{"_value":"HG8245H5"}}},` +
		`"VirtualParameters":{"RXPower":{"_value":"` + rxPower + `"}}}`
}

func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recent := deviceJSON("ONT-AAA-001", time.Now().Add(-time.Hour), "-23.66")
		old := deviceJSON("ONT-BBB-002", time.Now().Add(-30*24*time.Hour), "-28.00")
		w.Write([]byte(`[` + recent + `,` + old + `]`))
	}))
	defer srv.Close()

	result, err := New(srv.URL, "").Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping error: %v", err)
	}
	if result.TotalDevices != 2 || result.OnlineDevices != 1 {
		t.Fatalf("Ping = %+v, want two devices and one online", result)
	}
}

func TestPingRequestsOnlyAggregateSafeFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projection := r.URL.Query().Get("projection")
		if projection != "_lastInform" {
			t.Errorf("Ping projection = %q, want only _lastInform", projection)
		}
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	if _, err := New(srv.URL, "").Ping(context.Background()); err != nil {
		t.Fatalf("Ping error: %v", err)
	}
}

func TestGetDeviceStateUsesBoundedProjectedExactLookup(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("limit"); got != "1" {
			t.Errorf("limit = %q, want 1", got)
		}
		projection := r.URL.Query().Get("projection")
		for _, field := range []string{"_lastInform", "Manufacturer", "ModelName", "RXPower"} {
			if !strings.Contains(projection, field) {
				t.Errorf("projection = %q, want %q", projection, field)
			}
		}
		if strings.Contains(projection, "_id") {
			t.Errorf("projection must not request raw _id: %q", projection)
		}
		if got := r.URL.Query().Get("query"); got != `{"_id":"ONT-AAA-001"}` {
			t.Errorf("query = %q, want exact device ID query", got)
		}
		w.Write([]byte(`[` + deviceJSON("ONT-AAA-001", time.Now().Add(-time.Hour), "-23.66") + `]`))
	}))
	defer srv.Close()

	out, err := New(srv.URL, "").Invoke(context.Background(), "genieacs.get_device_state", map[string]any{"device_id": "ONT-AAA-001"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if strings.Contains(out.Text, "ONT-AAA-001") {
		t.Errorf("Text must not expose raw device ID: %q", out.Text)
	}
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data = %T, want safe state map", out.Data)
	}
	if _, exposed := data["_id"]; exposed {
		t.Errorf("Data must not expose raw device ID: %+v", data)
	}
	if data["state"] != "ONLINE" || data["vendor"] != "ZTE" || data["model"] != "HG8245H5" || data["rx_power_dbm"] != "-23.66" {
		t.Errorf("Data = %+v, want projected safe state", data)
	}
}

func TestGetDeviceStateRejectsUnboundedServerResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		first := deviceJSON("ONT-AAA-001", time.Now().Add(-time.Hour), "-23.66")
		second := deviceJSON("ONT-BBB-002", time.Now().Add(-time.Hour), "-24.00")
		w.Write([]byte(`[` + first + `,` + second + `]`))
	}))
	defer srv.Close()

	_, err := New(srv.URL, "").Invoke(context.Background(), "genieacs.get_device_state", map[string]any{"device_id": "ONT-AAA-001"})
	if err == nil || !strings.Contains(err.Error(), "lebih dari satu") {
		t.Fatalf("unbounded response error = %v, want more-than-one rejection", err)
	}
}

func TestGetDeviceStateRequiresExactDeviceID(t *testing.T) {
	adapter := New("http://example.invalid", "")
	for _, args := range []map[string]any{{}, {"identity": "customer-a"}, {"device_id": "   "}} {
		if _, err := adapter.Invoke(context.Background(), "genieacs.get_device_state", args); err == nil {
			t.Errorf("args %+v must be rejected without a device_id", args)
		}
	}
}

func TestBulkDeviceListingIsDisabled(t *testing.T) {
	adapter := New("http://example.invalid", "")
	if _, err := adapter.Invoke(context.Background(), "genieacs.get_devices", nil); err == nil {
		t.Fatal("genieacs.get_devices must be disabled")
	}
	for _, name := range adapter.ToolNames() {
		if name == "genieacs.get_devices" {
			t.Fatal("disabled bulk tool must not be advertised")
		}
	}
}

func TestConfiguredAndToolNames(t *testing.T) {
	if New("", "").Configured() {
		t.Error("Configured = true without URL")
	}
	adapter := New("http://x", "")
	if adapter.Domain() != "genieacs" {
		t.Errorf("Domain = %q, want genieacs", adapter.Domain())
	}
	names := adapter.ToolNames()
	if len(names) != 1 || names[0] != "genieacs.get_device_state" {
		t.Errorf("ToolNames = %v, want only bounded device-state lookup", names)
	}
}

func TestIsOnline(t *testing.T) {
	recent := time.Now().Add(-time.Hour)
	cutoff := time.Now().Add(-7 * 24 * time.Hour)
	if !isOnline(map[string]any{"_lastInform": recent.Format(time.RFC3339)}, cutoff) {
		t.Error("recent RFC3339 inform must be online")
	}
	if isOnline(map[string]any{"_lastInform": ""}, cutoff) {
		t.Error("empty inform must be offline")
	}
}

func TestParamValue(t *testing.T) {
	device := map[string]any{
		"InternetGatewayDevice": map[string]any{"DeviceInfo": map[string]any{"Manufacturer": map[string]any{"_value": "ZTE"}}},
		"VirtualParameters":     map[string]any{"RXPower": map[string]any{"_value": "-23.66"}},
	}
	if got := vendor(device); got != "ZTE" {
		t.Errorf("vendor = %q, want ZTE", got)
	}
	if got := rxPower(device); got != "-23.66" {
		t.Errorf("rxPower = %q, want -23.66", got)
	}
	if got := paramValue(device, "tidak.ada"); got != "" {
		t.Errorf("missing param = %q, want empty", got)
	}
}
