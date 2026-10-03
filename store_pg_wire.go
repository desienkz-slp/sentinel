package main

import (
	"context"
	"log"
	"sync"
	"time"

	"ainoc/internal/caseengine"
	"ainoc/internal/config"
	"ainoc/internal/db"
	"ainoc/internal/handoff"
	"ainoc/internal/pgstore"
)

// Penyimpanan PostgreSQL (docs/PLAN_LANJUTAN_DB_DAN_FASE.md, Fase E).
//
//	off    : tidak membuka koneksi; hanya JSON (perilaku lama).
//	shadow : JSON tetap sumber kebenaran; setiap perubahan handoff DITULIS JUGA ke
//	         PostgreSQL, selisih dicatat. Kegagalan PG tidak mengubah apa pun.
//	on     : sama seperti shadow (JSON tetap ditulis sebagai cadangan). Pembacaan
//	         dari PG baru ditambahkan setelah selisih terbukti nol di produksi.
//
// PostgreSQL mati/tidak dikonfigurasi -> aplikasi tetap jalan lewat JSON.

// pgState menyimpan koneksi opsional + status terakhir untuk panel.
type pgState struct {
	mu       sync.Mutex
	repo     *pgstore.HandoffRepo
	cases    *pgstore.CaseRepo
	lastErr  string
	writes   int64
	failures int64
}

func (p *pgState) ok(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.writes++
		p.lastErr = ""
		return
	}
	p.failures++
	p.lastErr = err.Error()
}

// Snapshot untuk panel/metrik (tanpa kredensial).
func (p *pgState) Snapshot() map[string]any {
	if p == nil {
		return map[string]any{"connected": false}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return map[string]any{"connected": p.repo != nil, "writes": p.writes, "failures": p.failures, "last_error": p.lastErr}
}

// storePGMode membaca mode (nil-aman).
func (s *Server) storePGMode() config.TeamMode {
	if s.cfg == nil {
		return config.TeamOff
	}
	return config.NormalizeTeamMode(s.cfg.StorePG)
}

// connectPG membuka koneksi PostgreSQL dan menjalankan migrasi. Dipanggil sekali
// saat start; gagal = log saja, aplikasi lanjut tanpa PG.
func connectPG(ctx context.Context, cfg *config.Config, dbcfg db.Config, migrationsDir string) *pgState {
	st := &pgState{}
	if config.NormalizeTeamMode(cfg.StorePG) == config.TeamOff || dbcfg.PGPassword == "" {
		return st
	}
	dsn := pgstore.DSN(dbcfg.PGHost, dbcfg.PGPort, dbcfg.PGUser, dbcfg.PGPassword, dbcfg.PGDatabase)
	pool, err := pgstore.Open(ctx, dsn)
	if err != nil {
		log.Printf("[pg] tidak tersedia (%s): %v — memakai JSON", pgstore.Redacted(dbcfg.PGHost, dbcfg.PGPort, dbcfg.PGUser, dbcfg.PGDatabase), err)
		st.ok(err)
		return st
	}
	ms, err := pgstore.LoadDir(migrationsDir)
	if err != nil {
		log.Printf("[pg] migrasi tidak terbaca: %v — memakai JSON", err)
		st.ok(err)
		pool.Close()
		return st
	}
	mctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, err := pgstore.Migrate(mctx, pool, ms)
	if err != nil {
		log.Printf("[pg] migrasi gagal: %v — memakai JSON", err)
		st.ok(err)
		pool.Close()
		return st
	}
	log.Printf("[pg] tersambung; migrasi diterapkan=%v dilewati=%d", res.Applied, len(res.Skipped))
	st.repo = pgstore.NewHandoffRepo(pool)
	st.cases = pgstore.NewCaseRepo(pool)
	return st
}

// attachHandoffSink memasang penulis PostgreSQL ke buku handoff. Tidak memblokir:
// penulisan berjalan di goroutine dengan batas waktu; urutan dijaga oleh Upsert
// yang selalu menulis keadaan lengkap terbaru.
func (s *Server) attachHandoffSink() {
	if s.ho == nil || s.pg == nil || s.pg.repo == nil {
		return
	}
	var mu sync.Mutex // satu penulisan PG pada satu waktu -> urutan terjaga
	s.ho.SetSink(func(h handoff.Handoff) {
		if s.storePGMode() == config.TeamOff {
			return
		}
		go func() {
			mu.Lock()
			defer mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := s.pg.repo.Upsert(ctx, h)
			s.pg.ok(err)
			if err != nil {
				log.Printf("[pg] GAGAL simpan handoff %s: %v (JSON tetap tersimpan)", h.CaseID, err)
				s.teams.RecordHandoff("pg_write_failed")
				return
			}
			s.teams.RecordHandoff("pg_write_ok")
		}()
	})
}

// verifyHandoffPG membandingkan JSON dengan PostgreSQL; dipanggil periodik.
func (s *Server) verifyHandoffPG(ctx context.Context) []string {
	if s.ho == nil || s.pg == nil || s.pg.repo == nil {
		return nil
	}
	dst, err := s.pg.repo.All(ctx)
	if err != nil {
		s.pg.ok(err)
		return []string{"gagal membaca PostgreSQL"}
	}
	d := pgstore.Diff(s.ho.All(), dst)
	if len(d) > 0 {
		s.teams.RecordHandoff("pg_diff")
		log.Printf("[pg] SELISIH JSON vs PostgreSQL: %d (contoh: %s)", len(d), d[0])
	}
	return d
}

// attachCaseSink memasang penulis PostgreSQL ke CaseWire (kasus). Sama seperti
// handoff: JSON tetap sumber kebenaran, kegagalan PG hanya dicatat.
func (s *Server) attachCaseSink(cw interface{ SetSink(func(caseengine.Record)) }) {
	if cw == nil || s.pg == nil || s.pg.cases == nil {
		return
	}
	var mu sync.Mutex
	cw.SetSink(func(rec caseengine.Record) {
		if s.storePGMode() == config.TeamOff {
			return
		}
		go func() {
			mu.Lock()
			defer mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			err := s.pg.cases.Upsert(ctx, rec)
			s.pg.ok(err)
			if err != nil {
				log.Printf("[pg] GAGAL simpan case %s: %v (JSON tetap tersimpan)", rec.ID, err)
				s.teams.RecordHandoff("pg_case_failed")
				return
			}
			s.teams.RecordHandoff("pg_case_ok")
		}()
	})
}
