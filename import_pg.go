package main

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"ainoc/internal/caseengine"
	"ainoc/internal/config"
	"ainoc/internal/db"
	"ainoc/internal/handoff"
	"ainoc/internal/pgstore"
)

// importPG menyalin data JSON lama (cases.json, handoffs.json) ke PostgreSQL
// (Fase F). Idempoten: UPSERT, jadi diulang tidak menggandakan. Tidak menghapus
// atau mengubah JSON. dryRun=true hanya menghitung dan membandingkan.
//
// Dipanggil dari `ai-noc-go -import-pg [-dry-run]`; keluaran ke w, tanpa isi
// pesan, nomor, atau kredensial.
func importPG(ctx context.Context, w io.Writer, cfg *config.Config, dbcfg db.Config, migrationsDir string, dryRun bool) error {
	if dbcfg.PGPassword == "" {
		return fmt.Errorf("NOC_POSTGRES_PASSWORD kosong - PostgreSQL belum dikonfigurasi")
	}
	pool, err := pgstore.Open(ctx, pgstore.DSN(dbcfg.PGHost, dbcfg.PGPort, dbcfg.PGUser, dbcfg.PGPassword, dbcfg.PGDatabase))
	if err != nil {
		return err
	}
	defer pool.Close()
	if !dryRun {
		ms, err := pgstore.LoadDir(migrationsDir)
		if err != nil {
			return err
		}
		mctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		if _, err := pgstore.Migrate(mctx, pool, ms); err != nil {
			return err
		}
	}
	dataDir := filepath.Dir(cfg.IncidentPath)

	// ---- kasus ----
	tr := caseengine.NewTracker()
	if err := tr.Load(filepath.Join(dataDir, "cases.json")); err != nil {
		return fmt.Errorf("cases.json: %w", err)
	}
	var recs []caseengine.Record
	for _, c := range tr.All() {
		recs = append(recs, c.Record())
	}
	cr := pgstore.NewCaseRepo(pool)
	fmt.Fprintf(w, "kasus  : sumber JSON=%d\n", len(recs))
	if !dryRun {
		for _, r := range recs {
			if err := cr.Upsert(ctx, r); err != nil {
				return fmt.Errorf("impor kasus %s: %w", r.ID, err)
			}
		}
	}
	if sum, err := cr.Summaries(ctx); err == nil {
		d := pgstore.DiffCases(recs, sum)
		fmt.Fprintf(w, "kasus  : PostgreSQL=%d selisih=%d\n", len(sum), len(d))
		if !dryRun && len(d) > 0 {
			return fmt.Errorf("verifikasi kasus gagal: %d selisih (contoh: %s)", len(d), d[0])
		}
	} else if !dryRun {
		return err
	}

	// ---- handoff ----
	led := handoff.New(filepath.Join(dataDir, "handoffs.json"))
	src := led.All()
	hr := pgstore.NewHandoffRepo(pool)
	fmt.Fprintf(w, "handoff: sumber JSON=%d\n", len(src))
	if !dryRun {
		for _, h := range src {
			if err := hr.Upsert(ctx, h); err != nil {
				return fmt.Errorf("impor handoff %s: %w", h.CaseID, err)
			}
		}
	}
	if dst, err := hr.All(ctx); err == nil {
		d := pgstore.Diff(src, dst)
		fmt.Fprintf(w, "handoff: PostgreSQL=%d selisih=%d\n", len(dst), len(d))
		if !dryRun && len(d) > 0 {
			return fmt.Errorf("verifikasi handoff gagal: %d selisih (contoh: %s)", len(d), d[0])
		}
	} else if !dryRun {
		return err
	}
	if dryRun {
		fmt.Fprintln(w, "DRY-RUN: tidak ada yang ditulis.")
	} else {
		fmt.Fprintln(w, "SELESAI: impor terverifikasi (jumlah dan isi sama dengan JSON).")
	}
	return nil
}
