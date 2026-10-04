package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return srv
}

func TestNewBuildsNOCPathAndHeader(t *testing.T) {
	var gotURL, gotKey string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.Path
		gotKey = r.Header.Get("X-NOC-API-Key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: []Customer{}})
	})
	defer srv.Close()

	a := New(srv.URL, "abc123key")
	if !a.Configured() {
		t.Fatal("harus Configured")
	}
	_, err := a.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "0812"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	// Base path harus /api/noc/v1, bukan /api/v1.
	if !strings.HasPrefix(gotURL, "/api/noc/v1/customers") {
		t.Errorf("URL path = %q, mau berawalan /api/noc/v1/customers", gotURL)
	}
	if gotKey != "abc123key" {
		t.Errorf("X-NOC-API-Key = %q, mau abc123key", gotKey)
	}
}

func TestHealthReportsTotal(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customersResponse{
			Status: "success",
			Meta: struct {
				CurrentPage int `json:"current_page"`
				PerPage     int `json:"per_page"`
				Total       int `json:"total"`
				LastPage    int `json:"last_page"`
			}{Total: 243},
		})
	})
	defer srv.Close()

	a := New(srv.URL, "k")
	detail, err := a.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !strings.Contains(detail, "243") {
		t.Errorf("detail = %q, mau memuat total 243", detail)
	}
}

func TestGetCustomerFound(t *testing.T) {
	cust := Customer{
		ID:       12,
		Name:     "Budi Santoso",
		Phone:    "08123456789",
		Username: "budi.pppoe",
		Status:   "active",
	}
	cust.Package.Name = "Paket 10Mbps"
	cust.Area.Name = "Kec. Cimahi"
	cust.Coordinate.Latitude = -6.914744
	cust.Coordinate.Longitude = 107.603012

	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: []Customer{cust}})
	})
	defer srv.Close()

	a := New(srv.URL, "k")
	out, err := a.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "08123456789"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if strings.Contains(out.Text, "Budi Santoso") || strings.Contains(out.Text, "08123456789") {
		t.Errorf("Text must not leak name or WA: %q", out.Text)
	}
	if !strings.Contains(out.Text, "active") {
		t.Errorf("Text = %q, mau memuat status active", out.Text)
	}
}

func TestGetCustomerNotFound(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: []Customer{}})
	})
	defer srv.Close()

	a := New(srv.URL, "k")
	out, err := a.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "nomor-tak-ada"})
	if err != nil {
		t.Fatalf("invoke: %v", err)
	}
	if !strings.Contains(out.Text, "Tidak ditemukan") {
		t.Errorf("Text = %q, mau 'Tidak ditemukan'", out.Text)
	}
}

func TestGetCustomerMissingIdentity(t *testing.T) {
	a := New("http://x", "k")
	if _, err := a.Invoke(context.Background(), "billing.get_customer", map[string]any{}); err == nil {
		t.Error("tanpa identity harus error")
	}
}

func TestUnconfiguredNotConfigured(t *testing.T) {
	a := New("", "")
	if a.Configured() {
		t.Error("tanpa base URL harus Configured=false")
	}
	if _, err := a.Health(context.Background()); err == nil {
		t.Error("Health tanpa konfigurasi harus error")
	}
}

func TestToolNames(t *testing.T) {
	a := New("http://x", "k")
	names := a.ToolNames()
	if len(names) != 3 || names[0] != "billing.get_customer" || names[1] != "billing.get_history" || names[2] != "billing.list_customers" {
		t.Errorf("ToolNames = %v, mau [billing.get_customer billing.get_history billing.list_customers]", names)
	}
}

func TestInvokeUnknownTool(t *testing.T) {
	a := New("http://x", "k")
	if _, err := a.Invoke(context.Background(), "billing.hapus_semua", nil); err == nil {
		t.Error("tool tak dikenal harus error")
	}
}
