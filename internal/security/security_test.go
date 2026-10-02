package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNonLoopbackControlRequestRequiresConfiguredToken(t *testing.T) {
	guard := New(":8090", "", "")
	h := guard.Operator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://noc.example/api/config", nil)
	r.RemoteAddr = "203.0.113.11:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestNonLoopbackControlRequestAcceptsBearerToken(t *testing.T) {
	guard := New(":8090", "operator-secret", "")
	h := guard.Operator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://noc.example/api/config", nil)
	r.RemoteAddr = "203.0.113.11:41000"
	r.Header.Set("Authorization", "Bearer operator-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestWebhookRejectsUnsignedNonLoopbackTraffic(t *testing.T) {
	guard := New(":8090", "", "webhook-secret")
	h := guard.Webhook(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://noc.example/api/wa/webhook", nil)
	r.RemoteAddr = "203.0.113.11:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestLoopbackControlAllowedWithoutConfiguredToken(t *testing.T) {
	// Keputusan operator: dashboard/CLI lokal dipercaya tanpa token.
	// Yang tetap ditolak adalah akses NON-loopback tanpa konfigurasi token.
	guard := New("127.0.0.1:8090", "", "")
	h := guard.Operator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/config", nil)
	r.RemoteAddr = "127.0.0.1:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestWebhookAllowsUnsignedLoopbackTraffic(t *testing.T) {
	// Gateway WhatsApp berjalan di mesin ini dan tidak menandatangani
	// request; loopback dipercaya. Non-loopback tetap wajib tanda tangan.
	guard := New("127.0.0.1:8090", "operator-secret", "")
	h := guard.Webhook(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/api/wa/webhook", nil)
	r.RemoteAddr = "127.0.0.1:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestTrustedCIDRAllowedWithoutToken(t *testing.T) {
	// Operator mengakses dashboard dari subnet internal (bukan loopback).
	// Dengan NOC_TRUSTED_CIDRS, subnet itu dipercaya tanpa token.
	guard := New(":8090", "", "")
	guard.SetTrustedCIDRs([]string{"172.18.20.0/25"})
	h := guard.Operator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://172.18.20.133/api/config", nil)
	r.RemoteAddr = "172.18.20.10:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusNoContent)
	}
}

func TestOutsideTrustedCIDRStillRequiresToken(t *testing.T) {
	// IP di luar subnet tepercaya tetap ditolak tanpa token.
	guard := New(":8090", "", "")
	guard.SetTrustedCIDRs([]string{"172.18.20.0/25"})
	h := guard.Operator(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodPost, "http://203.0.113.11/api/config", nil)
	r.RemoteAddr = "203.0.113.11:41000"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}
