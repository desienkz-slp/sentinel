package pgstore

import (
	"context"
	"strings"
	"testing"

	"ainoc/internal/caseengine"
)

func buatCase(t *testing.T, tr *caseengine.Tracker, ident string, steps ...caseengine.State) *caseengine.Case {
	t.Helper()
	c := tr.Replace(ident, "whatsapp")
	for _, s := range steps {
		if err := c.Transition(s, "uji", "alasan uji"); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func caseRepoSiap(t *testing.T) *CaseRepo {
	t.Helper()
	pool := testPool(t)
	ms, err := LoadDir("../../migrations")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(context.Background(), pool, ms); err != nil {
		t.Fatalf("migrasi: %v", err)
	}
	return NewCaseRepo(pool)
}

func TestCaseRepoBolakBalikDanIdempoten(t *testing.T) {
	r := caseRepoSiap(t)
	ctx := context.Background()
	tr := caseengine.NewTracker()
	c := buatCase(t, tr, "628111000001", caseengine.StateIdentifying, caseengine.StateConversation,
		caseengine.StateReadyForDiagnosis, caseengine.StateEscalation)
	for i := 0; i < 3; i++ { // diulang: tidak menggandakan
		if err := r.Upsert(ctx, c.Record()); err != nil {
			t.Fatal(err)
		}
	}
	sum, err := r.Summaries(ctx)
	if err != nil || len(sum) != 1 {
		t.Fatalf("summaries: %v %v", sum, err)
	}
	s := sum[string(c.ID)]
	if s.State != "ESCALATION" || s.Events != 4 || s.Version != c.Version() {
		t.Fatalf("isi salah: %+v (versi sumber %d)", s, c.Version())
	}
	if d := DiffCases([]caseengine.Record{c.Record()}, sum); len(d) != 0 {
		t.Fatalf("harus sama: %v", d)
	}
}

// Trigger skema: RESOLVED tanpa verifikasi lulus ditolak oleh DATABASE sendiri,
// bahkan bila aplikasi (salah) mengirimnya.
func TestCaseRepoResolvedButuhVerifikasi(t *testing.T) {
	r := caseRepoSiap(t)
	ctx := context.Background()
	tr := caseengine.NewTracker()
	c := buatCase(t, tr, "628111000002", caseengine.StateIdentifying)
	rec := c.Record()
	rec.State = caseengine.StateResolved // dipalsukan: tanpa verifikasi
	err := r.Upsert(ctx, rec)
	if err == nil || !strings.Contains(err.Error(), "verification") {
		t.Fatalf("DB harus menolak RESOLVED tanpa verifikasi: %v", err)
	}
	rec.Verifications = nil
	// dengan verifikasi lulus -> diterima (urutan tulis benar)
	rec.Verifications = append(rec.Verifications, caseengine.Verification{Passed: true, Source: "uji", Summary: "ok"})
	rec.Verifications[0].At = rec.UpdatedAt
	if err := r.Upsert(ctx, rec); err != nil {
		t.Fatalf("RESOLVED + verifikasi lulus harus diterima: %v", err)
	}
	s := func() Summary { m, _ := r.Summaries(ctx); return m[string(c.ID)] }()
	if s.State != "RESOLVED" || s.Verifications != 1 {
		t.Fatalf("state: %+v", s)
	}
}

func TestCaseRepoDiffMendeteksiSelisih(t *testing.T) {
	r := caseRepoSiap(t)
	ctx := context.Background()
	tr := caseengine.NewTracker()
	a := buatCase(t, tr, "628111000003", caseengine.StateIdentifying)
	b := buatCase(t, tr, "628111000004", caseengine.StateIdentifying)
	_ = r.Upsert(ctx, a.Record())
	sum, _ := r.Summaries(ctx)
	if d := DiffCases([]caseengine.Record{a.Record(), b.Record()}, sum); len(d) != 1 {
		t.Fatalf("b hilang harus terdeteksi: %v", d)
	}
	_ = a.Transition(caseengine.StateConversation, "uji", "maju")
	if d := DiffCases([]caseengine.Record{a.Record()}, sum); len(d) == 0 {
		t.Fatal("state/versi berbeda harus terdeteksi")
	}
}
