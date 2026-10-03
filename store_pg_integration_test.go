package main

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/db"
	"ainoc/internal/pgstore"
)

// Uji ujung ke ujung dengan PostgreSQL ASLI. Dilewati bila NOC_PG_TEST_DSN kosong.
// Memakai skema sementara; tidak menyentuh data sungguhan.
func pgServer(t *testing.T, mode string) (*Server, string) {
	t.Helper()
	dsn := os.Getenv("NOC_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("NOC_PG_TEST_DSN tidak diset")
	}
	s, _ := handoffServer(t, "on")
	s.cfg.StorePG = mode
	ctx := context.Background()
	admin, err := pgstore.Open(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	schema := "e_" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "_"))
	if len(schema) > 50 {
		schema = schema[:50]
	}
	_, _ = admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		admin.Close()
	})
	return s, schema + "|" + dsn
}

func sambungPG(t *testing.T, s *Server, schemaDSN string) {
	t.Helper()
	parts := strings.SplitN(schemaDSN, "|", 2)
	dsn := parts[1]
	if strings.Contains(dsn, "?") {
		dsn += "&search_path=" + parts[0]
	} else {
		dsn += "?search_path=" + parts[0]
	}
	pool, err := pgstore.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	ms, _ := pgstore.LoadDir("migrations")
	if _, err := pgstore.Migrate(context.Background(), pool, ms); err != nil {
		t.Fatalf("migrasi: %v", err)
	}
	s.pg = &pgState{repo: pgstore.NewHandoffRepo(pool)}
	s.attachHandoffSink()
}

func tunggu(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("kondisi tidak tercapai dalam 5 detik")
}

func TestPGShadowHandoffTertulisDanSama(t *testing.T) {
	s, sd := pgServer(t, "shadow")
	sambungPG(t, s, sd)
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "tutup "+caseJar+" Gangguan sudah diperbaiki")
	tunggu(t, func() bool {
		d := s.verifyHandoffPG(context.Background())
		return len(d) == 0 && s.pg.Snapshot()["writes"].(int64) >= 3
	})
	if s.pg.Snapshot()["failures"].(int64) != 0 {
		t.Fatalf("tidak boleh ada kegagalan: %v", s.pg.Snapshot())
	}
	dst, _ := s.pg.repo.All(context.Background())
	if len(dst) != 1 || len(dst[0].Updates) != 1 || !dst[0].Updates[0].Notified {
		t.Fatalf("isi PG salah: %+v", dst)
	}
}

func TestPGOffTidakMenulisWalauTersambung(t *testing.T) {
	s, sd := pgServer(t, "off")
	sambungPG(t, s, sd)
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "tutup "+caseJar+" Selesai ya")
	time.Sleep(400 * time.Millisecond)
	n, _ := s.pg.repo.Count(context.Background())
	if n != 0 {
		t.Fatalf("mode off tidak boleh menulis ke PG: %d baris", n)
	}
}

// PostgreSQL MATI di tengah jalan: alur nyata tetap sukses lewat JSON, kegagalan tercatat.
func TestPGMatiDiTengahJalanTidakMengganggu(t *testing.T) {
	s, sd := pgServer(t, "shadow")
	sambungPG(t, s, sd)
	s.pg.repo = pgstore.NewHandoffRepo(nil) // memutus koneksi (pool nil)
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar+" Sudah normal")
	if !strings.Contains(got, "sudah dikirim ke pelanggan") {
		t.Fatalf("balasan staf harus normal saat PG mati: %q", got)
	}
	if h, _ := s.ho.Get(caseJar); h.Status != "selesai" {
		t.Fatalf("JSON tetap sumber kebenaran: %s", h.Status)
	}
	tunggu(t, func() bool { return s.pg.Snapshot()["failures"].(int64) >= 1 })
	if s.teams.Snapshot().Handoffs["pg_write_failed"] < 1 {
		t.Fatal("kegagalan harus tercatat di metrik")
	}
}

func TestPGKataSandiTidakDiLog(t *testing.T) {
	cfg := config.Default()
	cfg.StorePG = "shadow"
	st := connectPG(context.Background(), cfg, db.Config{PGHost: "127.0.0.1", PGPort: "1", PGUser: "u", PGPassword: "RAHASIA-XYZ", PGDatabase: "d"}, t.TempDir())
	if e, _ := st.Snapshot()["last_error"].(string); strings.Contains(e, "RAHASIA-XYZ") {
		t.Fatal("bocor")
	}
}
