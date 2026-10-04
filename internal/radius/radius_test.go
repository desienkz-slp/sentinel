package radius

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestAdapter membuat Adapter menunjuk ke server tes dengan Bearer token.
func newTestAdapter(srv *httptest.Server) *Adapter {
	return New(srv.URL, "test-token")
}

// TestPing: parse /api/system/stats + verifikasi Bearer header.
func TestPing(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/api/system/stats" {
			t.Errorf("path = %q, mau /api/system/stats", r.URL.Path)
		}
		w.Write([]byte(`{"cpu":{"load":"12.5"},"memory":{"usedPercent":"50.0"},"database":{"sizeMB":"128.45"},"radiusData":{"radcheckCount":500}}`))
	}))
	defer srv.Close()

	a := New(srv.URL, "secret-token")
	d, err := a.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping error: %v", err)
	}
	if !strings.HasPrefix(gotAuth, "Bearer ") {
		t.Errorf("Authorization = %q, mau Bearer ...", gotAuth)
	}
	if d.CPULoad != "12.5" || d.UserCount != 500 || d.DBSizeMB != "128.45" {
		t.Errorf("Ping = %+v, mau cpu 12.5 user 500 db 128.45", d)
	}
}

// TestSessionAndUserToolsAreBlockedBeforeAnyBulkRequest proves that upstream
// only offers unbounded list endpoints, so adapter dispatch cannot fetch them.
func TestSessionAndUserToolsAreBlockedBeforeAnyBulkRequest(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	a := newTestAdapter(srv)
	for _, name := range []string{"radius.get_session", "radius.get_user"} {
		if _, err := a.Invoke(context.Background(), name, map[string]any{"identity": "pelanggan01"}); err == nil {
			t.Errorf("%s must be blocked", name)
		}
	}
	if requests != 0 {
		t.Fatalf("blocked tools made %d upstream requests; must not download bulk data", requests)
	}
}

// TestGetSystemStats: teks ringkas.
func TestGetSystemStats(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"cpu":{"load":"5"},"memory":{"usedPercent":"40"},"database":{"sizeMB":"100"},"radiusData":{"radcheckCount":300}}`))
	}))
	defer srv.Close()

	a := newTestAdapter(srv)
	out, err := a.Invoke(context.Background(), "radius.get_system_stats", map[string]any{})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "300") {
		t.Errorf("Text = %q, mau memuat jumlah user 300", out.Text)
	}
}

// TestToolNames & Domain.
func TestToolNames(t *testing.T) {
	a := New("http://x", "t")
	if a.Domain() != "radius" {
		t.Errorf("Domain = %q, mau radius", a.Domain())
	}
	names := a.ToolNames()
	want := []string{"radius.get_system_stats"}
	if len(names) != len(want) {
		t.Fatalf("ToolNames = %v, mau %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Errorf("ToolNames[%d] = %q, mau %q", i, names[i], want[i])
		}
	}
}

// TestConfigured: butuh base URL.
func TestConfigured(t *testing.T) {
	if New("", "t").Configured() {
		t.Error("Configured = true tanpa URL")
	}
	if !New("http://x", "t").Configured() {
		t.Error("Configured = false padahal URL ada")
	}
}
