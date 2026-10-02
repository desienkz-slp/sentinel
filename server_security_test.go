package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"ainoc/internal/config"
	"ainoc/internal/diag"
)

func TestOperationalAPIsRejectRequestsWithoutOperatorCredential(t *testing.T) {
	s := &Server{cfg: &config.Config{Addr: "127.0.0.1:8090"}}
	h := s.routes()
	for _, path := range []string{
		"/api/health",
		"/api/config",
		"/api/diag",
		"/api/ask",
		"/api/codex",
		"/api/wa/send",
	} {
		r := httptest.NewRequest(http.MethodPost, "http://noc.local"+path, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status=%d, want %d", path, w.Code, http.StatusServiceUnavailable)
		}
	}
}

func TestManualDiagnosticRejectsTargetOutsideApprovedAllowlist(t *testing.T) {
	s := &Server{
		cfg:  &config.Config{Addr: "127.0.0.1:8090", OperatorToken: "operator-secret"},
		diag: diag.New(1),
	}
	h := s.routes()
	r := httptest.NewRequest(http.MethodPost, "http://noc.local/api/diag", bytes.NewBufferString(`{"tool":"ping","target":"8.8.8.8"}`))
	r.Header.Set("Authorization", "Bearer operator-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusForbidden)
	}
}

func TestWebhookRejectsUnsignedTraffic(t *testing.T) {
	s := &Server{cfg: &config.Config{Addr: "127.0.0.1:8090", OperatorToken: "operator-secret"}}
	h := s.routes()
	r := httptest.NewRequest(http.MethodPost, "http://noc.local/api/wa/webhook", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
