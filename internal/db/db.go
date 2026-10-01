// Package db menyediakan koneksi PostgreSQL + Redis dengan graceful degradation.
//
// Sesuai blueprint §5 (PostgreSQL) dan §6 (Redis): kedua penyimpanan ini
// menjadi fondasi persistensi. Tapi aplikasi harus TETAP JALAN bila database
// belum tersedia (belum di-provision) — semua store saat ini sudah punya
// fallback JSON file, dan koneksi DB bersifat OPSIONAL sampai operator
// menyalakan infrastruktur lewat deploy/noc-infra.compose.yml.
//
// Prinsip blueprint §47: bila dependensi OFFLINE, statusnya harus tercatat
// UNKNOWN/OFFLINE — jangan pernah menebak "berhasil" bila koneksi gagal.
package db

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
)

// Status adalah hasil uji koneksi.
type Status struct {
	OK        bool   `json:"ok"`
	Kind      string `json:"kind"` // postgres | redis
	DSN       string `json:"dsn"`  // tanpa password (sudah disamarkan)
	LatencyMS int64  `json:"latency_ms"`
	Err       string `json:"err,omitempty"`
}

// Config menampung parameter koneksi.
type Config struct {
	// PostgreSQL
	PGHost     string
	PGPort     string
	PGUser     string
	PGPassword string
	PGDatabase string

	// Redis
	RedisAddr     string
	RedisPassword string
}

// Default mengembalikan config dari environment (deploy/.env), dengan fallback
// ke nilai bawaan bila env tidak diset.
func Default() Config {
	return Config{
		PGHost:        getenv("127.0.0.1", "NOC_POSTGRES_HOST"),
		PGPort:        getenv("5432", "NOC_POSTGRES_PORT"),
		PGUser:        getenv("noc_app", "NOC_POSTGRES_USER"),
		PGPassword:    getenv("", "NOC_POSTGRES_PASSWORD"),
		PGDatabase:    getenv("noc_sentinel", "NOC_POSTGRES_DB"),
		RedisAddr:     getenv("127.0.0.1:6379", "NOC_REDIS_ADDR"),
		RedisPassword: getenv("", "NOC_REDIS_PASSWORD"),
	}
}

// getenv mengembalikan nilai env bila diset, selain itu defaultValue.
func getenv(defaultValue string, keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return defaultValue
}

// PGDSN mengembalikan DSN PostgreSQL (tanpa memaparkan password di log).
func (c Config) PGDSN() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		c.PGUser, c.PGPassword, c.PGHost, c.PGPort, c.PGDatabase)
}

// PGDSNRedacted mengembalikan DSN dengan password disamarkan (aman untuk log).
func (c Config) PGDSNRedacted() string {
	return fmt.Sprintf("postgres://%s:***@%s:%s/%s",
		c.PGUser, c.PGHost, c.PGPort, c.PGDatabase)
}

// RedisDSNRedacted untuk log.
func (c Config) RedisDSNRedacted() string {
	return c.RedisAddr
}

// Configured melaporkan apakah ada konfigurasi DB yang bisa dicoba.
func (c Config) Configured() bool {
	return c.PGPassword != "" || c.RedisPassword != ""
}

// ProbeTimeout adalah batas waktu uji koneksi.
const ProbeTimeout = 3 * time.Second

var _ = context.Background // placeholder; koneksi nyata diimplementasikan saat infra aktif
