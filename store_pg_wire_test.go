package main

import (
	"ainoc/internal/dedupe"
	"ainoc/internal/redisx"
	"context"
	"strings"
	"testing"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/db"
	"ainoc/internal/handoff"
)

// Tanpa PostgreSQL (nil/mati/off): semuanya tetap jalan lewat JSON.
func TestStorePGOffTanpaKoneksi(t *testing.T) {
	s, _ := handoffServer(t, "on")
	s.pg = nil // seperti uji lain / produksi sebelum PG dipasang
	s.attachHandoffSink()
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar+" Sudah normal")
	if !strings.Contains(got, "sudah dikirim") {
		t.Fatalf("alur handoff harus tetap jalan tanpa PG: %q", got)
	}
	if snap := s.pg.Snapshot(); snap["connected"] != false {
		t.Fatalf("snapshot nil harus connected=false: %v", snap)
	}
	if len(s.verifyHandoffPG(context.Background())) != 0 {
		t.Fatal("verify tanpa PG harus kosong")
	}
}

// PostgreSQL ditunjuk tapi MATI: connectPG tidak boleh panic/menghalangi start,
// dan kata sandi tidak boleh ada di log/error.
func TestConnectPGMatiTidakMenggagalkanStart(t *testing.T) {
	cfg := config.Default()
	cfg.StorePG = "shadow"
	st := connectPG(context.Background(), cfg, db.Config{PGHost: "127.0.0.1", PGPort: "1", PGUser: "u",
		PGPassword: "RAHASIA-XYZ", PGDatabase: "d"}, t.TempDir())
	if st == nil || st.repo != nil {
		t.Fatalf("harus tidak tersambung: %+v", st)
	}
	snap := st.Snapshot()
	if snap["connected"] != false || snap["failures"].(int64) != 1 {
		t.Fatalf("snapshot: %v", snap)
	}
	if e, _ := snap["last_error"].(string); strings.Contains(e, "RAHASIA-XYZ") {
		t.Fatalf("kata sandi bocor ke status: %q", e)
	}
}

func TestConnectPGOffTidakMembukaKoneksi(t *testing.T) {
	cfg := config.Default()
	cfg.StorePG = "off"
	st := connectPG(context.Background(), cfg, db.Config{PGHost: "127.0.0.1", PGPort: "1", PGPassword: "x"}, t.TempDir())
	if st.Snapshot()["failures"].(int64) != 0 {
		t.Fatal("off tidak boleh mencoba koneksi")
	}
}

func TestStorePGModeIlegalDitolak(t *testing.T) {
	s, h := teamTestServer(t)
	if code, _ := call(t, h, "POST", "/api/config", `{"store_pg":"onn"}`); code != 400 {
		t.Fatalf("harus 400, dapat %d", code)
	}
	if s.cfg.StorePG != "" {
		t.Fatal("nilai ilegal tidak boleh tersimpan")
	}
	if code, _ := call(t, h, "POST", "/api/config", `{"store_pg":"shadow"}`); code != 200 || s.storePGMode() != config.TeamShadow {
		t.Fatalf("shadow harus diterima: %d", code)
	}
	_, out := call(t, h, "GET", "/api/team/metrics", "")
	pg, _ := out["store_pg"].(map[string]any)
	if pg == nil || pg["mode"] != "shadow" {
		t.Fatalf("status PG harus tampil: %v", out["store_pg"])
	}
}

var _ = handoff.Unknown

func TestStoreRedisModeIlegalDitolakDanStatusTampil(t *testing.T) {
	s, h := teamTestServer(t)
	if code, _ := call(t, h, "POST", "/api/config", `{"store_redis":"hidup"}`); code != 400 {
		t.Fatalf("harus 400, dapat %d", code)
	}
	if s.cfg.StoreRedis != "" {
		t.Fatal("nilai ilegal tidak boleh tersimpan")
	}
	if code, _ := call(t, h, "POST", "/api/config", `{"store_redis":"shadow"}`); code != 200 {
		t.Fatalf("shadow harus diterima: %d", code)
	}
	_, out := call(t, h, "GET", "/api/team/metrics", "")
	r, _ := out["store_redis"].(map[string]any)
	if r == nil || r["mode"] != "shadow" || r["connected"] != false {
		t.Fatalf("status Redis salah: %v", out["store_redis"])
	}
}

// Redis mati + mode on: webhook WA tetap men-dedupe dari lokal (tidak ada pesan ganda).
func TestDedupeWebhookRedisMati(t *testing.T) {
	s, _ := teamTestServer(t)
	s.cfg.StoreRedis = "on"
	s.ded = &dedupe.Hybrid{Local: dedupe.New(time.Minute, 10), TTL: time.Minute,
		Remote: redisx.New("127.0.0.1:1", "x"), Mode: func() string { return "on" }}
	fp := dedupe.Fingerprint("wa", "628111000001", "halo")
	if s.ded.Seen(fp) {
		t.Fatal("pertama bukan duplikat")
	}
	if !s.ded.Seen(fp) {
		t.Fatal("kedua harus duplikat walau Redis mati")
	}
	if s.ded.Stats()["remote_errors"] < 1 {
		t.Fatal("error Redis harus tercatat")
	}
}
