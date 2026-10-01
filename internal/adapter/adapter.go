// Package adapter menyediakan base HTTP adapter untuk sistem eksternal.
//
// Ini mengisi Stage 2 blueprint: setiap adaptor (Billing/RADIUS/MikroTik/
// GenieACS) membutuhkan client HTTP yang sama — timeout, retry dengan backoff,
// header auth, health check, dan redaksi kredensial. Package ini mewadahi
// semuanya supaya adaptor nyata nanti tinggal mendefinisikan endpoint + parser,
// bukan menulis ulang plumbing HTTP.
package adapter

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config adalah parameter koneksi satu adaptor.
type Config struct {
	Domain      string        // billing | radius | mikrotik | genieacs
	BaseURL     string        // tanpa trailing slash
	Token       string        // token (opsional, untuk HeaderName / Bearer)
	Timeout     time.Duration // timeout per request
	MaxRetries  int           // jumlah retry (hanya untuk GET yang aman)
	HeaderName  string        // header auth (default Authorization: Bearer <token>)
	BasicUser   string        // bila diisi: HTTP Basic Auth (BasicUser:BasicPass)
	BasicPass   string        // (MikroTik REST memakai Basic Auth, bukan API key)
	InsecureTLS bool          // terima sertifikat self-signed (MikroTik default memakai
	// sertifikat self-signed; setara curl -k). Hanya untuk jaringan terpercaya.
}

// HTTP adalah base adapter dengan client + retry + health.
type HTTP struct {
	cfg Config
	cl  *http.Client
}

// New membuat base adapter.
func New(cfg Config) *HTTP {
	if cfg.Timeout <= 0 {
		cfg.Timeout = 8 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 2
	}
	if cfg.HeaderName == "" {
		cfg.HeaderName = "Authorization"
	}
	// Transport khusus bila InsecureTLS: terima sertifikat self-signed
	// (default MikroTik). Tanpa ini, HTTPS ke router self-signed gagal dengan
	// x509: unknown authority. Penting: Transport hanya di-set bila non-nil,
	// karena typed-nil (*http.Transport)(nil) membuat net/http panic.
	cl := &http.Client{Timeout: cfg.Timeout}
	if cfg.InsecureTLS {
		cl.Transport = &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: true}, //nolint:gosec // disengaja: MikroTik default self-signed
		}
	}
	return &HTTP{
		cfg: cfg,
		cl:  cl,
	}
}

// Configured melaporkan apakah ada endpoint + token nyata.
func (h *HTTP) Configured() bool {
	return strings.TrimSpace(h.cfg.BaseURL) != ""
}

// Domain mengembalikan nama domain.
func (h *HTTP) Domain() string { return h.cfg.Domain }

// BaseURL mengembalikan endpoint yang sedang dipakai (untuk tampilan status).
func (h *HTTP) BaseURL() string { return h.cfg.BaseURL }

// Get melakukan HTTP GET dengan retry + backoff (aman untuk read-only).
func (h *HTTP) Get(ctx context.Context, path string) ([]byte, error) {
	url := strings.TrimRight(h.cfg.BaseURL, "/") + path
	var lastErr error
	for attempt := 0; attempt <= h.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			// Backoff eksponensial: 250ms, 500ms, 1s, ...
			backoff := time.Duration(1<<uint(attempt-1)) * 250 * time.Millisecond
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		h.setAuth(req)
		resp, err := h.cl.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		body, rerr := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		resp.Body.Close()
		if rerr != nil {
			lastErr = rerr
			continue
		}
		if resp.StatusCode >= 500 {
			// Error server: layak dicoba ulang.
			lastErr = fmt.Errorf("http %d: %s", resp.StatusCode, truncate(body, 200))
			continue
		}
		if resp.StatusCode >= 300 {
			return nil, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(body, 200))
		}
		return body, nil
	}
	return nil, fmt.Errorf("gagal setelah %d percobaan: %v", h.cfg.MaxRetries+1, lastErr)
}

// GetJSON mengambil GET dan mendekode JSON ke dst.
func (h *HTTP) GetJSON(ctx context.Context, path string, dst any) error {
	b, err := h.Get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("parse JSON: %w (body=%s)", err, truncate(b, 200))
	}
	return nil
}

// Health melakukan request ke path health (default /health) dan melaporkan
// status liveness + error.
func (h *HTTP) Health(ctx context.Context, healthPath string) (detail string, err error) {
	if !h.Configured() {
		return "", fmt.Errorf("%s: belum dikonfigurasi", h.cfg.Domain)
	}
	if healthPath == "" {
		healthPath = "/health"
	}
	start := time.Now()
	_, err = h.Get(ctx, healthPath)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("menjawab dalam %dms", time.Since(start).Milliseconds()), nil
}

func (h *HTTP) setAuth(req *http.Request) {
	// Prioritas: Basic Auth (MikroTik dll.) lebih dulu, lalu token kustom.
	if h.cfg.BasicUser != "" {
		req.SetBasicAuth(h.cfg.BasicUser, h.cfg.BasicPass)
		return
	}
	if h.cfg.Token == "" {
		return
	}
	if h.cfg.HeaderName == "Authorization" {
		req.Header.Set("Authorization", "Bearer "+h.cfg.Token)
		return
	}
	req.Header.Set(h.cfg.HeaderName, h.cfg.Token)
}

func truncate(b []byte, n int) string {
	s := strings.TrimSpace(string(b))
	if len(s) > n {
		return s[:n] + "..."
	}
	return s
}
