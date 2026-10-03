package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ainoc/internal/caseengine"
	"ainoc/internal/config"
	"ainoc/internal/db"
	"ainoc/internal/handoff"
	"ainoc/internal/pgstore"
)

func siapkanDataLama(t *testing.T) (*config.Config, string) {
	t.Helper()
	d := t.TempDir()
	cfg := config.Default()
	cfg.IncidentPath = filepath.Join(d, "incidents.json")
	tr := caseengine.NewTracker()
	tr.SetPath(filepath.Join(d, "cases.json"))
	c := tr.Replace("628111000001", "whatsapp")
	for _, s := range []caseengine.State{caseengine.StateIdentifying, caseengine.StateConversation, caseengine.StateEscalation} {
		if err := c.Transition(s, "uji", "uji"); err != nil {
			t.Fatal(err)
		}
	}
	c2 := tr.Replace("628111000002", "whatsapp")
	_ = c2.Transition(caseengine.StateIdentifying, "uji", "uji")
	if err := tr.Save(); err != nil {
		t.Fatal(err)
	}
	led := handoff.New(filepath.Join(d, "handoffs.json"))
	_, _ = led.Open(handoff.Handoff{CaseID: string(c.ID), Customer: "628111000001", Domain: "network", Complaint: "mati"})
	_, idx, _ := led.Apply(string(c.ID), "noc_senior", handoff.StatusClosed, "Sudah normal.")
	_ = led.MarkNotified(string(c.ID), idx, "")
	return cfg, d
}

func dbCfgDari(t *testing.T, schema string) db.Config {
	return db.Config{}
}

func TestImportPGTanpaKonfigurasi(t *testing.T) {
	cfg, _ := siapkanDataLama(t)
	err := importPG(context.Background(), &bytes.Buffer{}, cfg, db.Config{}, t.TempDir(), true)
	if err == nil || strings.Contains(err.Error(), "RAHASIA") {
		t.Fatalf("harus error jelas tanpa bocor: %v", err)
	}
}

func TestImportPGDryRunDanNyata(t *testing.T) {
	dsn := os.Getenv("NOC_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("NOC_PG_TEST_DSN tidak diset")
	}
	host, port, user, pass, name := os.Getenv("NOC_PG_TEST_HOST"), os.Getenv("NOC_PG_TEST_PORT"), os.Getenv("NOC_PG_TEST_USER"),
		os.Getenv("NOC_PG_TEST_PASS"), os.Getenv("NOC_PG_TEST_DBIMPORT")
	if name == "" {
		t.Skip("NOC_PG_TEST_DBIMPORT tidak diset (butuh DB khusus impor)")
	}
	dbc := db.Config{PGHost: host, PGPort: port, PGUser: user, PGPassword: pass, PGDatabase: name}
	ctx := context.Background()
	cfg, d := siapkanDataLama(t)
	jsonSebelum := map[string][]byte{}
	for _, f := range []string{"cases.json", "handoffs.json"} {
		b, _ := os.ReadFile(filepath.Join(d, f))
		jsonSebelum[f] = b
	}
	mig, _ := filepath.Abs("migrations")

	var out bytes.Buffer
	if err := importPG(ctx, &out, cfg, dbc, mig, true); err != nil {
		t.Logf("dry-run sebelum migrasi: %v", err)
	}
	// impor nyata
	out.Reset()
	if err := importPG(ctx, &out, cfg, dbc, mig, false); err != nil {
		t.Fatalf("impor gagal: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "kasus  : sumber JSON=2") || !strings.Contains(out.String(), "selisih=0") {
		t.Fatalf("ringkasan salah:\n%s", out.String())
	}
	// diulang: tidak menggandakan
	out.Reset()
	if err := importPG(ctx, &out, cfg, dbc, mig, false); err != nil {
		t.Fatalf("ulang gagal: %v", err)
	}
	pool, _ := pgstore.Open(ctx, pgstore.DSN(host, port, user, pass, name))
	defer pool.Close()
	nc, _ := pgstore.NewCaseRepo(pool).Count(ctx)
	nh, _ := pgstore.NewHandoffRepo(pool).Count(ctx)
	if nc != 2 || nh != 1 {
		t.Fatalf("setelah 2x impor: kasus=%d handoff=%d, mau 2 dan 1", nc, nh)
	}
	// JSON tidak berubah sama sekali
	for f, b := range jsonSebelum {
		now, _ := os.ReadFile(filepath.Join(d, f))
		if !bytes.Equal(b, now) {
			t.Errorf("%s berubah oleh impor", f)
		}
	}
	// ringkasan tidak memuat isi pesan/nomor
	if strings.Contains(out.String(), "628111") || strings.Contains(out.String(), "Sudah normal") {
		t.Errorf("keluaran memuat data: %s", out.String())
	}
}
