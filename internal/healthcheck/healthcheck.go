// Package healthcheck menjalankan probe kesehatan nyata untuk dependensi.
//
// Ini melengkapi health.Registry: registry hanya menyimpan status; package ini
// yang benar-benar mengecek LLM, WhatsApp gateway, PostgreSQL, dan Redis —
// lalu menuliskan hasilnya ke registry. Dijalankan sekali saat startup dan
// berkala di goroutine terpisah.
package healthcheck

import (
	"context"
	"net"
	"time"

	"ainoc/internal/health"
)

// LLMPinger adalah antarmuka minimal untuk mengecek LLM hidup.
type LLMPinger interface {
	Ping(ctx context.Context) (string, time.Duration, error)
}

// HTTPGetter mengecek sebuah URL HTTP (untuk WhatsApp gateway).
type HTTPGetter func(ctx context.Context, url string) error

// Probe mengisi registry dengan hasil cek nyata.
type Probe struct {
	Reg       *health.Registry
	LLM       LLMPinger
	LLMURL    string
	WAURL     string
	PGAddr    string // host:port PostgreSQL
	RedisAddr string
}

// Run mengecek semua dependensi sekali.
func (p *Probe) Run(ctx context.Context) {
	p.checkLLM(ctx)
	p.checkWA(ctx)
	p.checkPostgres(ctx)
	p.checkRedis(ctx)
}

func (p *Probe) checkLLM(ctx context.Context) {
	if p.LLM == nil || p.LLMURL == "" {
		p.Reg.Set("llm", health.StatusUnknown, "tidak dikonfigurasi", 0)
		return
	}
	start := time.Now()
	_, _, err := p.LLM.Ping(ctx)
	lat := time.Since(start).Milliseconds()
	if err != nil {
		p.Reg.Set("llm", health.StatusOffline, err.Error(), lat)
		return
	}
	p.Reg.Set("llm", health.StatusOnline, "menjawab", lat)
}

func (p *Probe) checkWA(ctx context.Context) {
	if p.WAURL == "" {
		p.Reg.Set("whatsapp", health.StatusUnknown, "tidak dikonfigurasi", 0)
		return
	}
	start := time.Now()
	ok := dialHTTP(ctx, p.WAURL)
	lat := time.Since(start).Milliseconds()
	if !ok {
		p.Reg.Set("whatsapp", health.StatusOffline, "gateway tidak menjawab", lat)
		return
	}
	p.Reg.Set("whatsapp", health.StatusOnline, "gateway siap", lat)
}

func (p *Probe) checkPostgres(ctx context.Context) {
	if p.PGAddr == "" {
		p.Reg.Set("postgres", health.StatusUnknown, "tidak dikonfigurasi", 0)
		return
	}
	start := time.Now()
	ok := dialTCP(ctx, p.PGAddr)
	lat := time.Since(start).Milliseconds()
	if !ok {
		// PostgreSQL belum dinyalakan bukan error fatal: sistem memakai JSON.
		p.Reg.Set("postgres", health.StatusDegraded, "belum tersedia — memakai JSON fallback", lat)
		return
	}
	p.Reg.Set("postgres", health.StatusOnline, "port terbuka", lat)
}

func (p *Probe) checkRedis(ctx context.Context) {
	if p.RedisAddr == "" {
		p.Reg.Set("redis", health.StatusUnknown, "tidak dikonfigurasi", 0)
		return
	}
	start := time.Now()
	ok := dialTCP(ctx, p.RedisAddr)
	lat := time.Since(start).Milliseconds()
	if !ok {
		p.Reg.Set("redis", health.StatusDegraded, "belum tersedia — memakai cache lokal", lat)
		return
	}
	p.Reg.Set("redis", health.StatusOnline, "port terbuka", lat)
}

func dialTCP(ctx context.Context, addr string) bool {
	d := net.Dialer{Timeout: 1500 * time.Millisecond}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func dialHTTP(ctx context.Context, url string) bool {
	if url == "" {
		return false
	}
	// Cek TCP saja — cukup untuk tahu proses mendengar di port itu.
	host := url
	if len(host) > 7 && (host[:7] == "http://") {
		host = host[7:]
	}
	if len(host) > 8 && host[:8] == "https://" {
		host = host[8:]
	}
	// Buang path.
	for i := 0; i < len(host); i++ {
		if host[i] == '/' {
			host = host[:i]
			break
		}
	}
	if host == "" {
		return false
	}
	if _, _, err := net.SplitHostPort(host); err != nil {
		host = host + ":3001" // fallback port WA
	}
	return dialTCP(ctx, host)
}
