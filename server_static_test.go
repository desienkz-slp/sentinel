package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ainoc/internal/config"
)

func TestSettingsHTMLDisablesBrowserCache(t *testing.T) {
	s := &Server{cfg: &config.Config{Addr: "127.0.0.1:8090"}}
	h := s.routes()
	r := httptest.NewRequest(http.MethodGet, "http://noc.local/settings.html", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusOK)
	}
	got := w.Header().Get("Cache-Control")
	if !strings.Contains(got, "no-store") || !strings.Contains(got, "must-revalidate") {
		t.Fatalf("Cache-Control=%q, want no-store + must-revalidate", got)
	}
	body := w.Body.String()
	for _, legacy := range []string{"dotLLM", "dotCodex", "Conversation — Endpoint A", "Reasoning — Endpoint B"} {
		if strings.Contains(body, legacy) {
			t.Errorf("settings masih memuat elemen legacy %q", legacy)
		}
	}
}
