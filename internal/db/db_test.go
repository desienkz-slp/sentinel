package db

import (
	"strings"
	"testing"
)

func TestPGDSN(t *testing.T) {
	c := Config{PGHost: "127.0.0.1", PGPort: "5432", PGUser: "noc_app", PGPassword: "s3cret", PGDatabase: "noc"}
	dsn := c.PGDSN()
	if !strings.Contains(dsn, "noc_app:s3cret@127.0.0.1:5432/noc") {
		t.Errorf("PGDSN = %q, mau berisi user:pass@host:port/db", dsn)
	}
}

func TestPGDSNRedactedNeverLeaksPassword(t *testing.T) {
	c := Config{PGHost: "127.0.0.1", PGPort: "5432", PGUser: "noc_app", PGPassword: "super-rahasia-123", PGDatabase: "noc"}
	red := c.PGDSNRedacted()
	if strings.Contains(red, "super-rahasia-123") {
		t.Errorf("redacted DSN membocorkan password: %q", red)
	}
	if !strings.Contains(red, "***") {
		t.Errorf("redacted DSN harus menampilkan ***: %q", red)
	}
}

func TestConfigured(t *testing.T) {
	if (Config{}).Configured() {
		t.Error("config kosong harus Configured=false")
	}
	if !(Config{PGPassword: "x"}).Configured() {
		t.Error("ada PGPassword harus Configured=true")
	}
	if !(Config{RedisPassword: "x"}).Configured() {
		t.Error("ada RedisPassword harus Configured=true")
	}
}

func TestRedisDSNRedactedNoPassword(t *testing.T) {
	c := Config{RedisAddr: "127.0.0.1:6379", RedisPassword: "rahasia-redis"}
	red := c.RedisDSNRedacted()
	if strings.Contains(red, "rahasia-redis") {
		t.Errorf("Redis redacted membocorkan password: %q", red)
	}
}

func TestDefaultReadsEnv(t *testing.T) {
	t.Setenv("NOC_POSTGRES_HOST", "db.internal")
	t.Setenv("NOC_POSTGRES_PASSWORD", "pw")
	t.Setenv("NOC_REDIS_ADDR", "redis.internal:6379")
	c := Default()
	if c.PGHost != "db.internal" || c.RedisAddr != "redis.internal:6379" {
		t.Errorf("Default tidak membaca env: %+v", c)
	}
	if c.PGPassword != "pw" {
		t.Errorf("PGPassword env tidak terbaca: %q", c.PGPassword)
	}
}
