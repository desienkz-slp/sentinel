package healthcheck

import (
	"context"
	"net"
	"testing"
	"time"

	"ainoc/internal/health"
)

type fakeLLM struct {
	err error
}

func (f fakeLLM) Ping(ctx context.Context) (string, time.Duration, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	return "PONG", 10 * time.Millisecond, nil
}

func startTCPServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String()
}

func TestCheckLLMOnlineAndOffline(t *testing.T) {
	reg := health.New()
	p := &Probe{Reg: reg, LLM: fakeLLM{}, LLMURL: "http://127.0.0.1:20128/v1"}
	p.checkLLM(context.Background())
	if d, _ := reg.Get("llm"); d.Status != health.StatusOnline {
		t.Errorf("LLM online = %s, mau ONLINE", d.Status)
	}

	p.LLM = fakeLLM{err: errBoom}
	p.checkLLM(context.Background())
	if d, _ := reg.Get("llm"); d.Status != health.StatusOffline {
		t.Errorf("LLM error = %s, mau OFFLINE", d.Status)
	}
}

func TestCheckTCPOnlineAndDegraded(t *testing.T) {
	addr := startTCPServer(t)

	reg := health.New()
	p := &Probe{Reg: reg, PGAddr: addr}
	p.checkPostgres(context.Background())
	if d, _ := reg.Get("postgres"); d.Status != health.StatusOnline {
		t.Errorf("postgres port terbuka = %s, mau ONLINE", d.Status)
	}

	// Port yang tidak ada -> DEGRADED (fallback JSON), bukan OFFLINE.
	p2 := &Probe{Reg: reg, PGAddr: "127.0.0.1:1"}
	p2.checkPostgres(context.Background())
	if d, _ := reg.Get("postgres"); d.Status != health.StatusDegraded {
		t.Errorf("postgres mati = %s, mau DEGRADED (fallback JSON)", d.Status)
	}
}

func TestCheckRedisDegradedWhenDown(t *testing.T) {
	reg := health.New()
	p := &Probe{Reg: reg, RedisAddr: "127.0.0.1:1"}
	p.checkRedis(context.Background())
	if d, _ := reg.Get("redis"); d.Status != health.StatusDegraded {
		t.Errorf("redis mati = %s, mau DEGRADED (fallback lokal)", d.Status)
	}
}

func TestCheckUnconfiguredUnknown(t *testing.T) {
	reg := health.New()
	p := &Probe{Reg: reg}
	p.checkPostgres(context.Background())
	if d, _ := reg.Get("postgres"); d.Status != health.StatusUnknown {
		t.Errorf("tidak dikonfigurasi = %s, mau UNKNOWN", d.Status)
	}
}

type errBoomType struct{}

func (e errBoomType) Error() string { return "boom" }

var errBoom = errBoomType{}
