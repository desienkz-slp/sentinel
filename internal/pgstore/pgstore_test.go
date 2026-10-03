package pgstore

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Uji integrasi memakai PostgreSQL ASLI. Dilewati bila NOC_PG_TEST_DSN kosong.
// Setiap uji memakai SKEMA sementara sendiri (dibuang di akhir), jadi tidak
// menyentuh tabel sungguhan.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("NOC_PG_TEST_DSN")
	if dsn == "" {
		t.Skip("NOC_PG_TEST_DSN tidak diset - uji integrasi PostgreSQL dilewati")
	}
	ctx := context.Background()
	admin, err := Open(ctx, dsn)
	if err != nil {
		t.Fatalf("buka admin: %v", err)
	}
	schema := "t_" + strings.ReplaceAll(strings.ToLower(t.Name()), "/", "_")
	if len(schema) > 50 {
		schema = schema[:50]
	}
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE`); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	cfg, _ := pgxpool.ParseConfig(dsn)
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		_, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`)
		admin.Close()
	})
	return pool
}

func fakeFS(files map[string]string) fstest.MapFS {
	m := fstest.MapFS{}
	for k, v := range files {
		m[k] = &fstest.MapFile{Data: []byte(v)}
	}
	return m
}

func TestDSNTidakBocorDiRedacted(t *testing.T) {
	d := DSN("127.0.0.1", "5432", "u", "p@ss:w/rd", "db")
	if !strings.Contains(d, "p%40ss%3Aw%2Frd") {
		t.Fatalf("kata sandi harus di-escape: %s", d)
	}
	r := Redacted("127.0.0.1", "5432", "u", "db")
	if strings.Contains(r, "p@ss") || !strings.Contains(r, "[REDACTED]") {
		t.Fatalf("redacted salah: %s", r)
	}
}

func TestOpenGagalTanpaBocorKataSandi(t *testing.T) {
	_, err := Open(context.Background(), DSN("127.0.0.1", "1", "u", "RAHASIA-XYZ", "db"))
	if err == nil {
		t.Fatal("harus gagal")
	}
	if strings.Contains(err.Error(), "RAHASIA-XYZ") {
		t.Fatalf("error membocorkan kata sandi: %v", err)
	}
	_, err = Open(context.Background(), "bukan-dsn RAHASIA-XYZ")
	if err == nil || strings.Contains(err.Error(), "RAHASIA-XYZ") {
		t.Fatalf("DSN rusak tidak boleh membocorkan isi: %v", err)
	}
}

func TestChecksumCRLFSamaDenganLF(t *testing.T) {
	a, _ := LoadMigrations(fakeFS(map[string]string{"001_a.sql": "SELECT 1;\nSELECT 2;\n"}), ".")
	b, _ := LoadMigrations(fakeFS(map[string]string{"001_a.sql": "SELECT 1;\r\nSELECT 2;\r\n"}), ".")
	if a[0].Checksum != b[0].Checksum {
		t.Fatal("CRLF vs LF tidak boleh mengubah checksum")
	}
}

func TestUrutanMigrasi(t *testing.T) {
	ms, _ := LoadMigrations(fakeFS(map[string]string{"010_c.sql": "x", "002_b.sql": "x", "001_a.sql": "x", "catatan.txt": "x"}), ".")
	if len(ms) != 3 || ms[0].Version != "001_a" || ms[1].Version != "002_b" || ms[2].Version != "010_c" {
		t.Fatalf("urutan salah: %+v", ms)
	}
}

func TestMigrateIdempoten(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ms, _ := LoadMigrations(fakeFS(map[string]string{
		"001_a.sql": "CREATE TABLE a (id int primary key);",
		"002_b.sql": "CREATE TABLE b (id int primary key, a_id int references a(id));",
	}), ".")
	r1, err := Migrate(ctx, pool, ms)
	if err != nil || len(r1.Applied) != 2 {
		t.Fatalf("pertama: %+v %v", r1, err)
	}
	r2, err := Migrate(ctx, pool, ms)
	if err != nil || len(r2.Applied) != 0 || len(r2.Skipped) != 2 {
		t.Fatalf("kedua harus tidak menerapkan apa pun: %+v %v", r2, err)
	}
}

func TestMigrateMenolakIsiBerubah(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ms, _ := LoadMigrations(fakeFS(map[string]string{"001_a.sql": "CREATE TABLE a (id int);"}), ".")
	if _, err := Migrate(ctx, pool, ms); err != nil {
		t.Fatal(err)
	}
	ms2, _ := LoadMigrations(fakeFS(map[string]string{"001_a.sql": "CREATE TABLE a (id int, extra text);"}), ".")
	if _, err := Migrate(ctx, pool, ms2); !errors.Is(err, ErrChecksum) {
		t.Fatalf("harus ErrChecksum, dapat %v", err)
	}
}

func TestMigrateGagalRollbackDanBerhenti(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	ms, _ := LoadMigrations(fakeFS(map[string]string{
		"001_ok.sql":    "CREATE TABLE ok (id int);",
		"002_rusak.sql": "CREATE TABLE sebagian (id int); SELECT tidak_ada_fungsi();",
		"003_nanti.sql": "CREATE TABLE nanti (id int);",
	}), ".")
	r, err := Migrate(ctx, pool, ms)
	if err == nil {
		t.Fatal("harus gagal")
	}
	if len(r.Applied) != 1 || r.Applied[0] != "001_ok" {
		t.Fatalf("hanya 001 yang diterapkan: %+v", r)
	}
	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name IN ('sebagian','nanti')`).Scan(&n)
	if n != 0 {
		t.Fatalf("rollback harus bersih, tabel tersisa=%d", n)
	}
	var v int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations WHERE version='002_rusak'`).Scan(&v)
	if v != 0 {
		t.Fatal("migrasi gagal tidak boleh tercatat")
	}
}

// Migrasi nyata proyek (001, 002, 003) berjalan bersih pada DB kosong dan diulang aman.
func TestMigrasiNyataProyek(t *testing.T) {
	pool := testPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ms, err := LoadDir("../../migrations")
	if err != nil || len(ms) < 3 {
		t.Fatalf("migrasi: %d %v", len(ms), err)
	}
	r, err := Migrate(ctx, pool, ms)
	if err != nil {
		t.Fatalf("migrasi nyata gagal: %v (diterapkan: %v)", err, r.Applied)
	}
	r2, err := Migrate(ctx, pool, ms)
	if err != nil || len(r2.Applied) != 0 {
		t.Fatalf("ulang harus no-op: %+v %v", r2, err)
	}
	for _, tbl := range []string{"cases", "case_events", "handoffs", "handoff_updates"} {
		var n int
		if e := pool.QueryRow(ctx, `SELECT count(*) FROM information_schema.tables WHERE table_schema=current_schema() AND table_name=$1`, tbl).Scan(&n); e != nil || n != 1 {
			t.Errorf("tabel %s tidak ada", tbl)
		}
	}
}
