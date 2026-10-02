package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/health"
	"ainoc/internal/observability"
)

func TestObservabilityEndpointsMenyediakanKPIHealthMetricsDanAlerts(t *testing.T) {
	healthRegistry := health.New()
	healthRegistry.Set("llm", health.StatusOnline, "siap", 12)
	collector := observability.NewCollector()
	collector.RecordHTTP(http.MethodGet, "/api/health", http.StatusOK, 10)
	s := &Server{
		cfg:  &config.Config{Addr: "127.0.0.1:8090", OperatorToken: "operator-secret"},
		hreg: healthRegistry,
		aud:  audit.New("", 10),
		obs:  collector,
	}
	h := s.routes()

	for _, path := range []string{"/healthz", "/readyz"} {
		r := httptest.NewRequest(http.MethodGet, "http://noc.local"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status=%d, ingin %d", path, w.Code, http.StatusOK)
		}
		var body map[string]any
		if err := json.NewDecoder(w.Body).Decode(&body); err != nil || body["ok"] != true {
			t.Fatalf("%s: respons bukan health JSON sukses: body=%q err=%v", path, w.Body.String(), err)
		}
	}

	for _, path := range []string{"/api/metrics", "/api/kpi", "/api/alerts", "/api/audit"} {
		r := httptest.NewRequest(http.MethodGet, "http://noc.local"+path, nil)
		r.Header.Set("Authorization", "Bearer operator-secret")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status=%d, ingin %d", path, w.Code, http.StatusOK)
		}
		if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("%s: content type=%q", path, w.Header().Get("Content-Type"))
		}
	}
}
