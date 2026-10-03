// Package pgstore = koneksi PostgreSQL, pelari migrasi, dan repositori kasus +
// handoff (docs/PLAN_LANJUTAN_DB_DAN_FASE.md, Fase D-F).
//
// Prinsip: PostgreSQL OPSIONAL. Bila tidak tersedia, pemanggil tetap berjalan
// lewat JSON; paket ini tidak pernah membuat aplikasi gagal start.
package pgstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DSN menyusun URL koneksi dari nilai terpisah. Hasilnya MENGANDUNG kata sandi:
// jangan dicatat. Gunakan Redacted untuk log.
func DSN(host, port, user, password, db string) string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable&connect_timeout=3",
		escape(user), escape(password), host, port, db)
}

// Redacted = bentuk aman untuk log.
func Redacted(host, port, user, db string) string {
	return fmt.Sprintf("postgres://%s:[REDACTED]@%s:%s/%s", user, host, port, db)
}

func escape(s string) string {
	r := strings.NewReplacer("%", "%25", "@", "%40", ":", "%3A", "/", "%2F", "?", "%3F", "#", "%23", " ", "%20")
	return r.Replace(s)
}

// Open membuka pool dan memverifikasi koneksi. Error -> pemanggil memakai JSON.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, errors.New("DSN tidak valid") // pesan asli bisa memuat kata sandi
	}
	cfg.MaxConns = 6
	cfg.MaxConnLifetime = time.Hour
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("tidak bisa membuka pool PostgreSQL")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("PostgreSQL tidak menjawab")
	}
	return pool, nil
}

// Migration = satu berkas migrasi berurutan.
type Migration struct {
	Version  string // nama berkas tanpa .sql, mis. 003_handoffs
	SQL      string
	Checksum string
}

// LoadMigrations membaca *.sql dari dir, terurut menurut nama.
func LoadMigrations(fsys fs.FS, dir string) ([]Migration, error) {
	ents, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range ents {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		b, err := fs.ReadFile(fsys, filepath.ToSlash(filepath.Join(dir, e.Name())))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256([]byte(normalizeEOL(string(b))))
		out = append(out, Migration{
			Version:  strings.TrimSuffix(e.Name(), ".sql"),
			SQL:      string(b),
			Checksum: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

// normalizeEOL: checksum tidak boleh berbeda hanya karena CRLF vs LF.
func normalizeEOL(s string) string { return strings.ReplaceAll(s, "\r\n", "\n") }

// LoadDir = LoadMigrations dari direktori nyata.
func LoadDir(dir string) ([]Migration, error) { return LoadMigrations(os.DirFS(dir), ".") }

// Result = ringkasan satu kali Migrate.
type Result struct {
	Applied []string
	Skipped []string
}

// ErrChecksum = migrasi yang sudah diterapkan isinya berubah.
var ErrChecksum = errors.New("isi migrasi berubah setelah diterapkan")

// Migrate menerapkan migrasi yang belum ada. Aman diulang. Tiap migrasi dalam
// satu transaksi; gagal -> rollback dan berhenti (migrasi berikutnya tidak jalan).
func Migrate(ctx context.Context, pool *pgxpool.Pool, ms []Migration) (Result, error) {
	var res Result
	if _, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), checksum TEXT NOT NULL)`); err != nil {
		return res, fmt.Errorf("schema_migrations: %w", err)
	}
	// Satu proses migrasi pada satu waktu.
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return res, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(727301)`); err != nil {
		return res, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(727301)`) //nolint:errcheck

	for _, m := range ms {
		var have string
		err := conn.QueryRow(ctx, `SELECT checksum FROM schema_migrations WHERE version=$1`, m.Version).Scan(&have)
		if err == nil {
			if have != m.Checksum {
				return res, fmt.Errorf("%w: %s", ErrChecksum, m.Version)
			}
			res.Skipped = append(res.Skipped, m.Version)
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return res, err
		}
		if _, err := tx.Exec(ctx, m.SQL); err != nil {
			_ = tx.Rollback(ctx)
			return res, fmt.Errorf("migrasi %s gagal: %w", m.Version, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version, checksum) VALUES ($1,$2)`, m.Version, m.Checksum); err != nil {
			_ = tx.Rollback(ctx)
			return res, err
		}
		if err := tx.Commit(ctx); err != nil {
			return res, err
		}
		res.Applied = append(res.Applied, m.Version)
	}
	return res, nil
}
