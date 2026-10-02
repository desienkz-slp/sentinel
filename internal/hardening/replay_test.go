// Package hardening menguji replay deterministik pada kegagalan produksi.
package hardening

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReplayDelayedReorderedAndDuplicateMessages(t *testing.T) {
	r := NewReplay()
	var calls atomic.Int32
	handler := func(context.Context, Event) error {
		calls.Add(1)
		return nil
	}

	child := Event{ID: "msg-child", DependsOn: []string{"msg-parent"}}
	if got := r.Process(context.Background(), child, handler); got.Status != StatusDeferred {
		t.Fatalf("pesan terurut terbalik harus ditunda, got=%+v", got)
	}
	if got := r.Process(context.Background(), Event{ID: "msg-parent"}, handler); got.Status != StatusSucceeded {
		t.Fatalf("pesan induk harus berhasil, got=%+v", got)
	}
	if got := r.Process(context.Background(), child, handler); got.Status != StatusSucceeded {
		t.Fatalf("pesan tertunda harus dapat diputar ulang, got=%+v", got)
	}
	if got := r.Process(context.Background(), Event{ID: "msg-parent"}, handler); got.Status != StatusRepeated {
		t.Fatalf("pesan duplikat harus tidak dieksekusi ulang, got=%+v", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("handler calls=%d, ingin 2", calls.Load())
	}
}

func TestReplayConcurrentDuplicateExecutesOnce(t *testing.T) {
	r := NewReplay()
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got := r.Process(context.Background(), Event{ID: "msg-sama"}, func(context.Context, Event) error {
				calls.Add(1)
				time.Sleep(5 * time.Millisecond)
				return nil
			})
			if got.Status != StatusSucceeded && got.Status != StatusRepeated {
				t.Errorf("status concurrent=%s", got.Status)
			}
		}()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("pesan duplikat concurrent dieksekusi %d kali, ingin 1", calls.Load())
	}
}

func TestReplayDependencyFailureAndGatewayOutage(t *testing.T) {
	r := NewReplay()
	if got := r.Process(context.Background(), Event{ID: "gateway"}, func(context.Context, Event) error {
		return errors.New("gateway WhatsApp tidak terjangkau")
	}); got.Status != StatusFailed {
		t.Fatalf("gateway outage harus gagal terukur, got=%+v", got)
	}

	var dependentCalls atomic.Int32
	got := r.Process(context.Background(), Event{ID: "balasan", DependsOn: []string{"gateway"}}, func(context.Context, Event) error {
		dependentCalls.Add(1)
		return nil
	})
	if got.Status != StatusDependencyFailed || dependentCalls.Load() != 0 {
		t.Fatalf("dependency failure tidak boleh menjalankan dependent, got=%+v calls=%d", got, dependentCalls.Load())
	}
}

func TestReplayFailureModesAndRetryAfterReconnect(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{name: "API failure", err: errors.New("api HTTP 503")},
		{name: "network failure", err: errors.New("network unreachable")},
		{name: "database failure", err: errors.New("database unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NewReplay().Process(context.Background(), Event{ID: tt.name}, func(context.Context, Event) error { return tt.err })
			if got.Status != StatusFailed || !errors.Is(got.Err, tt.err) {
				t.Fatalf("kegagalan harus tercatat, got=%+v", got)
			}
		})
	}

	r := NewReplay()
	var attempts atomic.Int32
	first := r.Process(context.Background(), Event{ID: "wa-reconnect"}, func(context.Context, Event) error {
		if attempts.Add(1) == 1 {
			return errors.New("gateway terputus")
		}
		return nil
	})
	second := r.Process(context.Background(), Event{ID: "wa-reconnect"}, func(context.Context, Event) error {
		attempts.Add(1)
		return nil
	})
	if first.Status != StatusFailed || second.Status != StatusSucceeded || attempts.Load() != 2 {
		t.Fatalf("reconnect harus boleh retry setelah outage: first=%+v second=%+v attempts=%d", first, second, attempts.Load())
	}
}

func TestActionGuardTimeoutUnauthorizedRepeatedAndRollback(t *testing.T) {
	g := NewActionGuard()
	action := Action{Key: "CASE-UJI-01/restart-ont"}

	unauthorized := g.Run(context.Background(), action, func(Action) error { return errors.New("tidak berwenang") }, nil, nil, nil)
	if unauthorized.Status != StatusUnauthorized {
		t.Fatalf("aksi tanpa otorisasi harus ditolak, got=%+v", unauthorized)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	timeout := g.Run(ctx, Action{Key: "CASE-UJI-01/tool"}, nil, func(ctx context.Context, _ Action) error {
		<-ctx.Done()
		return ctx.Err()
	}, nil, nil)
	if timeout.Status != StatusFailed || !errors.Is(timeout.Err, context.DeadlineExceeded) {
		t.Fatalf("timeout LLM/tool harus gagal tertelusur, got=%+v", timeout)
	}

	var executes atomic.Int32
	var rollbacks atomic.Int32
	rolledBack := g.Run(context.Background(), action, nil, func(context.Context, Action) error {
		executes.Add(1)
		return nil
	}, func(context.Context, Action) error {
		return errors.New("verifikasi gagal")
	}, func(context.Context, Action) error {
		rollbacks.Add(1)
		return nil
	})
	if rolledBack.Status != StatusRolledBack || executes.Load() != 1 || rollbacks.Load() != 1 {
		t.Fatalf("verifikasi gagal harus rollback sekali, got=%+v execute=%d rollback=%d", rolledBack, executes.Load(), rollbacks.Load())
	}

	repeated := g.Run(context.Background(), action, nil, func(context.Context, Action) error {
		executes.Add(1)
		return nil
	}, nil, nil)
	if repeated.Status != StatusRepeated || executes.Load() != 1 {
		t.Fatalf("aksi berulang tidak boleh dieksekusi lagi, got=%+v execute=%d", repeated, executes.Load())
	}
}

func TestActionGuardEscalationFailure(t *testing.T) {
	got := NewActionGuard().Run(context.Background(), Action{Key: "CASE-UJI-02/escalation"}, nil, func(context.Context, Action) error {
		return errors.New("seluruh jalur eskalasi habis")
	}, nil, nil)
	if got.Status != StatusFailed || got.Err == nil {
		t.Fatalf("gagal eskalasi harus tercatat tanpa sukses semu, got=%+v", got)
	}
}

func TestActionGuardConcurrentWaiterReceivesTerminalFailure(t *testing.T) {
	g := NewActionGuard()
	started := make(chan struct{})
	release := make(chan struct{})
	terminal := errors.New("mutasi eksternal gagal")
	firstResult := make(chan Result, 1)
	go func() {
		firstResult <- g.Run(context.Background(), Action{Key: "CASE-UJI-03/action"}, nil, func(context.Context, Action) error {
			close(started)
			<-release
			return terminal
		}, nil, nil)
	}()
	<-started

	waiterResult := make(chan Result, 1)
	go func() {
		waiterResult <- g.Run(context.Background(), Action{Key: "CASE-UJI-03/action"}, nil, nil, nil, nil)
	}()
	// Eksekusi pertama tetap diblokir, sehingga waiter pasti melihat aksi inflight.
	time.Sleep(10 * time.Millisecond)
	close(release)

	if got := <-firstResult; got.Status != StatusFailed || !errors.Is(got.Err, terminal) {
		t.Fatalf("hasil pertama=%+v", got)
	}
	if got := <-waiterResult; got.Status != StatusFailed || !errors.Is(got.Err, terminal) {
		t.Fatalf("waiter harus menerima kegagalan terminal, got=%+v", got)
	}
}

func TestPanicCallbackBecomesFailureWithoutBlockingReplay(t *testing.T) {
	replayResult := NewReplay().Process(context.Background(), Event{ID: "msg-panic"}, func(context.Context, Event) error {
		panic("adapter crash")
	})
	if replayResult.Status != StatusFailed || replayResult.Err == nil {
		t.Fatalf("panic replay harus menjadi failure, got=%+v", replayResult)
	}

	actionResult := NewActionGuard().Run(context.Background(), Action{Key: "CASE-UJI-04/action"}, nil, func(context.Context, Action) error {
		panic("executor crash")
	}, nil, nil)
	if actionResult.Status != StatusFailed || actionResult.Err == nil {
		t.Fatalf("panic action harus menjadi failure, got=%+v", actionResult)
	}
}
