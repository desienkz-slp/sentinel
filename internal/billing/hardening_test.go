package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

func TestGetCustomerUsesOneExplicitServerPageAndSafeProjection(t *testing.T) {
	var query url.Values
	customer := Customer{
		ID:       42,
		Name:     "Pelanggan Rahasia",
		Phone:    "628111222333",
		Email:    "rahasia@example.test",
		Address:  "Jl. Rahasia 1",
		Username: "uji42",
		Status:   "active",
	}
	customer.Coordinate.Latitude = -6.9
	customer.Coordinate.Longitude = 107.6
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: []Customer{customer}})
	})

	out, err := New(srv.URL, "k").Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "uji42"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := query.Get("per_page"); got != "1" {
		t.Errorf("per_page = %q, want 1", got)
	}
	if got := query.Get("page"); got != "1" {
		t.Errorf("page = %q, want 1", got)
	}
	if got := query.Get("search"); got != "uji42" {
		t.Errorf("search = %q, want identity", got)
	}
	data, ok := out.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data type = %T, want safe projection map", out.Data)
	}
	for _, forbidden := range []string{"name", "phone", "email", "address", "coordinate", "id", "username"} {
		if _, exists := data[forbidden]; exists {
			t.Errorf("safe projection leaked %q: %#v", forbidden, data)
		}
		if strings.Contains(out.Text, "Pelanggan Rahasia") || strings.Contains(out.Text, "628111222333") || strings.Contains(out.Text, "rahasia@example.test") {
			t.Errorf("Text leaked customer PII: %q", out.Text)
		}
	}
}

func TestGetCustomerRejectsAmbiguousIdentity(t *testing.T) {
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: []Customer{{Status: "active"}}, Meta: struct {
			CurrentPage int `json:"current_page"`
			PerPage     int `json:"per_page"`
			Total       int `json:"total"`
			LastPage    int `json:"last_page"`
		}{Total: 2}})
	})
	_, err := New(srv.URL, "k").Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "budi"})
	if err == nil || !strings.Contains(err.Error(), "tidak unik") {
		t.Fatalf("ambiguous identity must fail closed, err=%v", err)
	}
}

func TestHistoryIsBlockedBeforeAnyUnboundedUpstreamRead(t *testing.T) {
	calls := 0
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusInternalServerError)
	})

	_, err := New(srv.URL, "k").Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji42"})
	if err == nil || !strings.Contains(err.Error(), "diblokir") {
		t.Fatalf("history must be blocked, err=%v", err)
	}
	if calls != 0 {
		t.Fatalf("blocked history made %d upstream calls", calls)
	}
}

func TestListReturnsAggregateOnlyFromOneExplicitServerPage(t *testing.T) {
	var query url.Values
	customer := Customer{Name: "Pelanggan Rahasia", Username: "uji42", Phone: "628111222333", Status: "active"}
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.Query()
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: []Customer{customer}, Meta: struct {
			CurrentPage int `json:"current_page"`
			PerPage     int `json:"per_page"`
			Total       int `json:"total"`
			LastPage    int `json:"last_page"`
		}{Total: 7}})
	})

	out, err := New(srv.URL, "k").Invoke(context.Background(), ToolList, map[string]any{"filter": "isolir"})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if got := query.Get("per_page"); got != "1" {
		t.Errorf("per_page = %q, want 1", got)
	}
	if got := query.Get("page"); got != "1" {
		t.Errorf("page = %q, want 1", got)
	}
	if got := query.Get("is_isolated"); got != "1" {
		t.Errorf("is_isolated = %q, want 1", got)
	}
	if _, ok := out.Data.(map[string]int); !ok {
		t.Fatalf("Data type = %T, want aggregate map", out.Data)
	}
	if strings.Contains(out.Text, "Pelanggan Rahasia") || strings.Contains(out.Text, "628111222333") {
		t.Errorf("list leaked raw customer record: %q", out.Text)
	}
}
