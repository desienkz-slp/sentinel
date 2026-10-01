package adapter

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigured(t *testing.T) {
	if New(Config{Domain: "billing"}).Configured() {
		t.Error("tanpa BaseURL harus Configured=false")
	}
	if !New(Config{Domain: "billing", BaseURL: "http://x"}).Configured() {
		t.Error("dengan BaseURL harus Configured=true")
	}
}

func TestGetRetriesOn500(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			w.WriteHeader(500)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	a := New(Config{Domain: "test", BaseURL: srv.URL, MaxRetries: 3, Timeout: 2 * time.Second})
	b, err := a.Get(context.Background(), "/x")
	if err != nil {
		t.Fatalf("harus berhasil setelah retry: %v", err)
	}
	if !strings.Contains(string(b), "ok") {
		t.Errorf("body = %s", b)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("calls = %d, mau 3 (2 gagal + 1 sukses)", calls)
	}
}

func TestGetNoRetryOn4xx(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(404)
		w.Write([]byte("not found"))
	}))
	defer srv.Close()

	a := New(Config{Domain: "test", BaseURL: srv.URL, MaxRetries: 3})
	if _, err := a.Get(context.Background(), "/x"); err == nil {
		t.Fatal("404 harus error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("4xx tidak boleh di-retry: calls = %d, mau 1", calls)
	}
}

func TestAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := New(Config{Domain: "billing", BaseURL: srv.URL, Token: "secret-token"})
	a.Get(context.Background(), "/x")
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q, mau Bearer secret-token", gotAuth)
	}
}

func TestCustomHeaderName(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-API-Key")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := New(Config{Domain: "radius", BaseURL: srv.URL, Token: "k", HeaderName: "X-API-Key"})
	a.Get(context.Background(), "/x")
	if got != "k" {
		t.Errorf("X-API-Key = %q, mau k", got)
	}
}

// MikroTik REST memakai HTTP Basic Auth (user:pass), bukan token.
func TestBasicAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := New(Config{Domain: "mikrotik", BaseURL: srv.URL, BasicUser: "admin", BasicPass: "rahasia"})
	a.Get(context.Background(), "/x")
	want := "Basic " + basicAuthHeader("admin", "rahasia")
	if gotAuth != want {
		t.Errorf("Authorization = %q, mau %q", gotAuth, want)
	}
}

// Basic Auth menang atas token bila keduanya diisi.
func TestBasicAuthBeatsToken(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := New(Config{Domain: "mikrotik", BaseURL: srv.URL, BasicUser: "admin", BasicPass: "rahasia", Token: "token-x"})
	a.Get(context.Background(), "/x")
	want := "Basic " + basicAuthHeader("admin", "rahasia")
	if gotAuth != want {
		t.Errorf("Authorization = %q, mau Basic Auth %q", gotAuth, want)
	}
}

func basicAuthHeader(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

func TestGetJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "ACTIVE"})
	}))
	defer srv.Close()

	a := New(Config{Domain: "billing", BaseURL: srv.URL})
	var out map[string]any
	if err := a.GetJSON(context.Background(), "/customer", &out); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if out["status"] != "ACTIVE" {
		t.Errorf("out = %v", out)
	}
}

func TestHealthNotConfigured(t *testing.T) {
	a := New(Config{Domain: "billing"})
	if _, err := a.Health(context.Background(), ""); err == nil {
		t.Error("belum dikonfigurasi harus error")
	}
}

func TestHealthConfigured(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	a := New(Config{Domain: "billing", BaseURL: srv.URL})
	if detail, err := a.Health(context.Background(), "/"); err != nil {
		t.Fatalf("Health: %v", err)
	} else if detail == "" {
		t.Error("detail health kosong")
	}
}
