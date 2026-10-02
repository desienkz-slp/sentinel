// Package hardening menyediakan pengaman deterministik untuk replay pesan dan
// aksi. Package ini tidak menggantikan persistence PostgreSQL; ia menjaga
// semantik yang sama untuk worker in-memory dan suite replay terisolasi.
package hardening

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Status menjelaskan hasil yang aman untuk direkam pada audit/replay.
type Status string

const (
	StatusSucceeded        Status = "SUCCEEDED"
	StatusRepeated         Status = "REPEATED"
	StatusDeferred         Status = "DEFERRED"
	StatusDependencyFailed Status = "DEPENDENCY_FAILED"
	StatusFailed           Status = "FAILED"
	StatusUnauthorized     Status = "UNAUTHORIZED"
	StatusRolledBack       Status = "ROLLED_BACK"
	StatusRollbackFailed   Status = "ROLLBACK_FAILED"
)

// Event adalah satu unit pesan yang dapat diputar ulang. ID wajib stabil dari
// gateway/upstream; caller tidak boleh memakai payload sebagai pengganti ID.
type Event struct {
	ID        string
	DependsOn []string
}

// Result menyimpan hasil tanpa mengubah error menjadi klaim keberhasilan.
type Result struct {
	Status Status
	Err    error
}

// Replay menjamin satu eksekusi sukses per ID dan menahan event yang dependency-
// nya belum selesai. Kegagalan tidak di-cache sebagai sukses agar reconnect/retry
// eksplisit dapat dijalankan lagi.
type Replay struct {
	mu        sync.Mutex
	completed map[string]Result
	failed    map[string]Result
	inflight  map[string]chan struct{}
}

func NewReplay() *Replay {
	return &Replay{
		completed: map[string]Result{},
		failed:    map[string]Result{},
		inflight:  map[string]chan struct{}{},
	}
}

// Process menjalankan handler hanya jika seluruh dependency berhasil. Pesan
// duplikat yang sedang diproses menunggu hasil eksekusi pertama, bukan memulai
// eksekusi kedua. Event gagal boleh dipanggil ulang untuk reconnect/retry.
func (r *Replay) Process(ctx context.Context, event Event, handler func(context.Context, Event) error) Result {
	if r == nil {
		return Result{Status: StatusFailed, Err: fmt.Errorf("replay coordinator tidak tersedia")}
	}
	id := strings.TrimSpace(event.ID)
	if id == "" {
		return Result{Status: StatusFailed, Err: fmt.Errorf("event ID kosong")}
	}
	event.ID = id

	r.mu.Lock()
	if _, ok := r.completed[id]; ok {
		r.mu.Unlock()
		return Result{Status: StatusRepeated}
	}
	for _, dependency := range event.DependsOn {
		dependency = strings.TrimSpace(dependency)
		if dependency == "" {
			continue
		}
		if failed, ok := r.failed[dependency]; ok {
			r.mu.Unlock()
			return Result{Status: StatusDependencyFailed, Err: fmt.Errorf("dependency %q gagal: %w", dependency, failed.Err)}
		}
		if _, ok := r.completed[dependency]; !ok {
			r.mu.Unlock()
			return Result{Status: StatusDeferred, Err: fmt.Errorf("dependency %q belum selesai", dependency)}
		}
	}
	if done, ok := r.inflight[id]; ok {
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			return Result{Status: StatusFailed, Err: ctx.Err()}
		case <-done:
		}
		r.mu.Lock()
		defer r.mu.Unlock()
		if _, ok := r.completed[id]; ok {
			return Result{Status: StatusRepeated}
		}
		return r.failed[id]
	}
	done := make(chan struct{})
	r.inflight[id] = done
	r.mu.Unlock()

	var err error
	if handler != nil {
		err = safeCall(func() error { return handler(ctx, event) })
	}
	result := Result{Status: StatusSucceeded, Err: err}
	if err != nil {
		result.Status = StatusFailed
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, id)
	if err != nil {
		r.failed[id] = result
	} else {
		delete(r.failed, id)
		r.completed[id] = result
	}
	close(done)
	return result
}

// Action adalah mutasi yang wajib memiliki idempotency key stabil dari case,
// approval, dan parameter aksi. Eksekusi eksternal tetap berada di adapter.
type Action struct{ Key string }

// ActionGuard serializes one action key and menjalankan rollback ketika aksi
// sukses tetapi verifikasi pasca-aksi gagal.
type ActionGuard struct {
	mu       sync.Mutex
	finished map[string]Result
	inflight map[string]chan struct{}
}

func NewActionGuard() *ActionGuard {
	return &ActionGuard{finished: map[string]Result{}, inflight: map[string]chan struct{}{}}
}

// Run menjalankan authorize -> execute -> verify. Unauthorized tidak dicatat
// sebagai aksi selesai sehingga identitas operator yang benar masih dapat
// mengajukan permintaan baru; semua hasil setelah eksekusi di-cache agar aksi
// berulang tidak menambah mutasi eksternal.
func (g *ActionGuard) Run(ctx context.Context, action Action, authorize func(Action) error, execute func(context.Context, Action) error, verify func(context.Context, Action) error, rollback func(context.Context, Action) error) Result {
	if g == nil {
		return Result{Status: StatusFailed, Err: fmt.Errorf("action guard tidak tersedia")}
	}
	action.Key = strings.TrimSpace(action.Key)
	if action.Key == "" {
		return Result{Status: StatusFailed, Err: fmt.Errorf("idempotency key aksi kosong")}
	}
	if authorize != nil {
		if err := authorize(action); err != nil {
			return Result{Status: StatusUnauthorized, Err: err}
		}
	}

	g.mu.Lock()
	if _, ok := g.finished[action.Key]; ok {
		g.mu.Unlock()
		return Result{Status: StatusRepeated}
	}
	if done, ok := g.inflight[action.Key]; ok {
		g.mu.Unlock()
		select {
		case <-ctx.Done():
			return Result{Status: StatusFailed, Err: ctx.Err()}
		case <-done:
		}
		// Pemanggil concurrent harus menerima hasil terminal yang sama. Mengubah
		// kegagalan awal menjadi REPEATED akan menyembunyikan error mutasi atau
		// rollback dari worker/outbox yang menunggu.
		g.mu.Lock()
		defer g.mu.Unlock()
		if result, ok := g.finished[action.Key]; ok {
			return result
		}
		return Result{Status: StatusFailed, Err: fmt.Errorf("hasil aksi concurrent tidak tersedia")}
	}
	done := make(chan struct{})
	g.inflight[action.Key] = done
	g.mu.Unlock()

	result := Result{Status: StatusSucceeded}
	if execute != nil {
		result.Err = safeCall(func() error { return execute(ctx, action) })
	}
	if result.Err != nil {
		result.Status = StatusFailed
	} else if verify != nil {
		if err := safeCall(func() error { return verify(ctx, action) }); err != nil {
			result = Result{Status: StatusFailed, Err: err}
			if rollback != nil {
				if rollbackErr := safeCall(func() error { return rollback(ctx, action) }); rollbackErr != nil {
					result = Result{Status: StatusRollbackFailed, Err: fmt.Errorf("verifikasi gagal: %v; rollback gagal: %w", err, rollbackErr)}
				} else {
					result.Status = StatusRolledBack
				}
			}
		}
	}

	g.mu.Lock()
	delete(g.inflight, action.Key)
	g.finished[action.Key] = result
	close(done)
	g.mu.Unlock()
	return result
}

// safeCall mengubah panic callback adaptor menjadi kegagalan terukur agar
// cleanup inflight tetap berjalan dan waiter tidak menggantung.
func safeCall(call func() error) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("callback panic: %v", recovered)
		}
	}()
	return call()
}
