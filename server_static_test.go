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
	// Status LLM/Codex tidak lagi memenuhi header Settings.
	for _, legacy := range []string{"dotLLM", "dotCodex", "txtLLM", "txtCodex"} {
		if strings.Contains(body, legacy) {
			t.Errorf("header settings masih memuat elemen status legacy %q", legacy)
		}
	}
	// Form konfigurasi endpoint tetap wajib tersedia untuk operator.
	for _, required := range []string{"sBase", "sModel", "sCodexBase", "sCodex"} {
		if !strings.Contains(body, `id="`+required+`"`) {
			t.Errorf("settings kehilangan field endpoint %q", required)
		}
	}
}

// Kartu "Mode Tim" harus ada, memuat kelima pengendali, dan memanggil endpoint
// yang benar; kartu setup Endpoint A/B tidak boleh hilang karenanya.
func TestSettingsHTMLMemuatKartuModeTim(t *testing.T) {
	s := &Server{cfg: &config.Config{Addr: "127.0.0.1:8090"}}
	h := s.routes()
	r := httptest.NewRequest(http.MethodGet, "http://noc.local/settings.html", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	for _, want := range []string{
		"teamCard", "tmRouting", "tmCSScope", "tmPresenter", "tmHandoff", "tmSeverity",
		"saveTeamModes", "loadTeamModes", "/api/team/metrics", "team_routing",
		"sBase", "sModel", "sCodexBase", "sCodex", // setup Endpoint A/B tetap ada
	} {
		if !strings.Contains(body, want) {
			t.Errorf("settings.html tidak memuat %q", want)
		}
	}
}
