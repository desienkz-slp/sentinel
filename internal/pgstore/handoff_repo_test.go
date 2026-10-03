package pgstore

import (
	"context"
	"testing"
	"time"

	"ainoc/internal/handoff"
)

func repoSiap(t *testing.T) *HandoffRepo {
	t.Helper()
	pool := testPool(t)
	ms, err := LoadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(context.Background(), pool, ms); err != nil {
		t.Fatalf("migrasi: %v", err)
	}
	return NewHandoffRepo(pool)
}

func contoh(id string) handoff.Handoff {
	return handoff.Handoff{CaseID: id, Customer: "628111000111", Domain: "network", Complaint: "internet mati",
		Evidence: map[string]string{"billing": "aktif", "radius": "sesi tidak ada", "mikrotik": handoff.Unknown, "genieacs": handoff.Unknown},
		Severity: "P3", Status: handoff.StatusOpen, CreatedAt: time.Now().UTC().Truncate(time.Microsecond), UpdatedAt: time.Now().UTC().Truncate(time.Microsecond)}
}

func TestHandoffRepoBolakBalik(t *testing.T) {
	r := repoSiap(t)
	ctx := context.Background()
	h := contoh("CASE-20261003-PGA234")
	if err := r.Upsert(ctx, h); err != nil {
		t.Fatal(err)
	}
	got, ok, err := r.Get(ctx, h.CaseID)
	if err != nil || !ok {
		t.Fatalf("get: %v %v", ok, err)
	}
	if got.Evidence["radius"] != "sesi tidak ada" || got.Severity != "P3" || got.Status != handoff.StatusOpen {
		t.Fatalf("isi salah: %+v", got)
	}
	// update + status berubah
	h.Status = handoff.StatusClosed
	h.Updates = []handoff.Update{{At: time.Now().UTC().Truncate(time.Microsecond), By: "noc_senior", Status: handoff.StatusClosed,
		Untuk: "Sudah normal, silakan dicoba lagi.", Notified: false}}
	if err := r.Upsert(ctx, h); err != nil {
		t.Fatal(err)
	}
	h.Updates[0].Notified = true
	if err := r.Upsert(ctx, h); err != nil { // ulang: idempoten, tidak menggandakan
		t.Fatal(err)
	}
	got, _, _ = r.Get(ctx, h.CaseID)
	if len(got.Updates) != 1 || !got.Updates[0].Notified || got.Status != handoff.StatusClosed {
		t.Fatalf("setelah update: %+v", got)
	}
	if n, _ := r.Count(ctx); n != 1 {
		t.Fatalf("count = %d, mau 1", n)
	}
}

func TestHandoffRepoDiff(t *testing.T) {
	r := repoSiap(t)
	ctx := context.Background()
	a, b := contoh("CASE-20261003-PGB234"), contoh("CASE-20261003-PGC234")
	_ = r.Upsert(ctx, a)
	dst, _ := r.All(ctx)
	if d := Diff([]handoff.Handoff{a}, dst); len(d) != 0 {
		t.Fatalf("harus sama: %v", d)
	}
	if d := Diff([]handoff.Handoff{a, b}, dst); len(d) != 1 {
		t.Fatalf("b hilang harus terdeteksi: %v", d)
	}
	a.Status = handoff.StatusClosed
	if d := Diff([]handoff.Handoff{a}, dst); len(d) != 1 {
		t.Fatalf("status beda harus terdeteksi: %v", d)
	}
}

// Ledger JSON -> sink -> PostgreSQL: yang dilihat PG sama dengan yang ada di JSON.
func TestLedgerSinkKePostgres(t *testing.T) {
	r := repoSiap(t)
	ctx := context.Background()
	l := handoff.New("")
	l.SetSink(func(h handoff.Handoff) {
		if err := r.Upsert(context.Background(), h); err != nil {
			t.Errorf("sink: %v", err)
		}
	})
	_, _ = l.Open(contoh("CASE-20261003-PGD234"))
	_, idx, err := l.Apply("CASE-20261003-PGD234", "noc_senior", handoff.StatusClosed, "Selesai.")
	if err != nil {
		t.Fatal(err)
	}
	_ = l.MarkNotified("CASE-20261003-PGD234", idx, "")
	dst, _ := r.All(ctx)
	if d := Diff(l.All(), dst); len(d) != 0 {
		t.Fatalf("JSON vs PG berbeda: %v", d)
	}
}
