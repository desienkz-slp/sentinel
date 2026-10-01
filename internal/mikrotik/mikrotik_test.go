package mikrotik

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ainoc/internal/adapter"
)

// newTestAdapter membuat Adapter yang menunjuk langsung ke server tes (http),
// memakai Basic Auth noc:rahasia. Ini menguji jalur auth + parsing tanpa
// melewati pembangunan base URL dari host:port (yang diuji terpisah di
// TestNewBuildsBaseURL).
func newTestAdapter(srv *httptest.Server) *Adapter {
	return &Adapter{
		http: adapter.New(adapter.Config{
			Domain:    "mikrotik",
			BaseURL:   srv.URL + "/rest",
			BasicUser: "noc",
			BasicPass: "rahasia",
		}),
	}
}

// TestNewBuildsBaseURL memastikan base URL dibangun benar (scheme + port).
func TestNewBuildsBaseURL(t *testing.T) {
	cases := []struct {
		cfg  Config
		want string
	}{
		{Config{Host: "192.168.88.1", User: "admin", TLS: true}, "https://192.168.88.1:443/rest"},
		{Config{Host: "192.168.88.1", User: "admin", TLS: false}, "http://192.168.88.1:80/rest"},
		{Config{Host: "192.168.88.1", Port: 8443, User: "admin", TLS: true}, "https://192.168.88.1:8443/rest"},
		{Config{Host: "192.168.88.1", Port: 8080, User: "admin", TLS: false}, "http://192.168.88.1:8080/rest"},
	}
	for _, c := range cases {
		a := New(c.cfg)
		if got := a.http.BaseURL(); got != c.want {
			t.Errorf("New(%+v).BaseURL = %q, mau %q", c.cfg, got, c.want)
		}
	}
}

// TestNewUnconfigured: tanpa host/user, adapter tidak terkonfigurasi.
func TestNewUnconfigured(t *testing.T) {
	if a := New(Config{}); a.Configured() {
		t.Error("Configured() = true untuk config kosong, mau false")
	}
	if a := New(Config{Host: "x"}); a.Configured() {
		t.Error("Configured() = true tanpa user, mau false")
	}
}

// TestGetPPPoEStatus verifikasi Basic Auth + cocokkan username (persis/contains).
func TestGetPPPoEStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verifikasi Basic Auth diterima.
		u, p, ok := r.BasicAuth()
		if !ok || u != "noc" || p != "rahasia" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/rest/ppp/active" {
			t.Errorf("path = %q, mau /rest/ppp/active", r.URL.Path)
		}
		w.Write([]byte(`[
			{"name":"628123456789@netlayer","service":"pppoe","caller-id":"AA:BB","address":"10.0.0.5","uptime":"1h2m"},
			{"name":"other-user","service":"pppoe","caller-id":"CC:DD","address":"10.0.0.6","uptime":"2h"}
		]`))
	}))
	defer srv.Close()

	a := newTestAdapter(srv)
	out, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{"identity": "628123456789"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "628123456789@netlayer") {
		t.Errorf("Text = %q, mau memuat username yang cocok", out.Text)
	}
	if strings.Contains(out.Text, "other-user") {
		t.Errorf("Text = %q, tidak boleh memuat username tidak cocok", out.Text)
	}
}

// TestGetPPPoEStatusNoMatch: tidak ada sesi → pesan "TIDAK aktif".
func TestGetPPPoEStatusNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	a := newTestAdapter(srv)
	out, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{"identity": "nobody"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "TIDAK aktif") {
		t.Errorf("Text = %q, mau memuat 'TIDAK aktif'", out.Text)
	}
}

// TestGetPPPoEStatusAuthFail: kredensial salah → error (401).
func TestGetPPPoEStatusAuthFail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	// Pass salah.
	a := &Adapter{http: adapter.New(adapter.Config{
		Domain: "mikrotik", BaseURL: srv.URL + "/rest",
		BasicUser: "noc", BasicPass: "salah",
	})}
	_, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{"identity": "x"})
	if err == nil {
		t.Fatal("Invoke harusnya error saat 401, malah sukses")
	}
}

// TestPing parses /system/resource.
func TestPing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rest/system/resource" {
			t.Errorf("path = %q, mau /rest/system/resource", r.URL.Path)
		}
		w.Write([]byte(`[{"version":"7.14.2","board-name":"CCR1036","uptime":"3d","cpu-load":"1"}]`))
	}))
	defer srv.Close()

	a := newTestAdapter(srv)
	d, err := a.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping error: %v", err)
	}
	if d.Version != "7.14.2" || d.BoardName != "CCR1036" {
		t.Errorf("Ping = %+v, mau version 7.14.2 board CCR1036", d)
	}
}

// TestToolNames hanya mengekspos tool read-only.
func TestToolNames(t *testing.T) {
	a := New(Config{Host: "192.168.88.1", User: "admin"})
	names := a.ToolNames()
	if len(names) != 1 || names[0] != "mikrotik.get_pppoe_status" {
		t.Errorf("ToolNames = %v, mau [mikrotik.get_pppoe_status]", names)
	}
	if a.Domain() != "mikrotik" {
		t.Errorf("Domain = %q, mau mikrotik", a.Domain())
	}
}
