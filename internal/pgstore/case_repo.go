package pgstore

import (
	"context"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"ainoc/internal/caseengine"
)

// CaseRepo menyimpan case di PostgreSQL. Idempoten (UPSERT). Skema punya trigger
// yang menolak state RESOLVED tanpa verifikasi lulus, jadi urutan tulis penting:
//  1. case dibuat/diperbarui TANPA mengubah state;
//  2. event dan verifikasi diganti utuh;
//  3. baru state diset (trigger memeriksa verifikasi yang sudah tersimpan).
type CaseRepo struct{ pool *pgxpool.Pool }

func NewCaseRepo(pool *pgxpool.Pool) *CaseRepo { return &CaseRepo{pool: pool} }

func (r *CaseRepo) Upsert(ctx context.Context, rec caseengine.Record) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("repo PostgreSQL tidak tersedia")
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	var pk string
	if err := tx.QueryRow(ctx, `
		INSERT INTO cases (case_id, channel, identity, state, version, created_at, updated_at)
		VALUES ($1,$2,$3,'NEW',1,$4,$5)
		ON CONFLICT (case_id) DO UPDATE SET channel=EXCLUDED.channel, identity=EXCLUDED.identity
		RETURNING case_pk::text`,
		string(rec.ID), rec.Channel, rec.Identity, rec.CreatedAt, rec.UpdatedAt).Scan(&pk); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM case_events WHERE case_pk=$1::uuid`, pk); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM case_verifications WHERE case_pk=$1::uuid`, pk); err != nil {
		return err
	}
	for _, v := range rec.Verifications {
		if _, err := tx.Exec(ctx, `INSERT INTO case_verifications (case_pk, passed, source, summary, verified_at)
			VALUES ($1::uuid,$2,$3,$4,$5)`, pk, v.Passed, v.Source, v.Summary, v.At); err != nil {
			return err
		}
	}
	for _, e := range rec.Events {
		if _, err := tx.Exec(ctx, `INSERT INTO case_events (case_pk, from_state, to_state, actor, reason, occurred_at)
			VALUES ($1::uuid, $2::case_state, $3::case_state, $4, $5, $6)`,
			pk, nullIfEmpty(string(e.From)), string(e.To), e.Actor, e.Reason, e.At); err != nil {
			return err
		}
	}
	var resolvedAt any
	if rec.State == caseengine.StateResolved {
		resolvedAt = rec.UpdatedAt
	}
	if _, err := tx.Exec(ctx, `UPDATE cases SET state=$2::case_state, version=$3, updated_at=$4, resolved_at=$5
		WHERE case_pk=$1::uuid`, pk, string(rec.State), rec.Version, rec.UpdatedAt, resolvedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Summary = ringkasan satu case untuk perbandingan (tanpa isi bebas).
type Summary struct {
	ID            string
	State         string
	Version       int64
	Events        int
	Verifications int
}

func (r *CaseRepo) Summaries(ctx context.Context) (map[string]Summary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT c.case_id, c.state::text, c.version,
		       (SELECT count(*) FROM case_events e WHERE e.case_pk=c.case_pk),
		       (SELECT count(*) FROM case_verifications v WHERE v.case_pk=c.case_pk)
		FROM cases c`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]Summary{}
	for rows.Next() {
		var s Summary
		if err := rows.Scan(&s.ID, &s.State, &s.Version, &s.Events, &s.Verifications); err != nil {
			return nil, err
		}
		out[s.ID] = s
	}
	return out, rows.Err()
}

func (r *CaseRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM cases`).Scan(&n)
	return n, err
}

// DiffCases membandingkan record sumber dengan isi PostgreSQL. Kosong = sama.
func DiffCases(src []caseengine.Record, dst map[string]Summary) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range src {
		id := string(s.ID)
		seen[id] = true
		d, ok := dst[id]
		switch {
		case !ok:
			out = append(out, id+": tidak ada di PostgreSQL")
		case d.State != string(s.State):
			out = append(out, id+": state berbeda")
		case d.Version != s.Version:
			out = append(out, id+": versi berbeda")
		case d.Events != len(s.Events):
			out = append(out, id+": jumlah event berbeda")
		case d.Verifications != len(s.Verifications):
			out = append(out, id+": jumlah verifikasi berbeda")
		}
	}
	for id := range dst {
		if !seen[id] {
			out = append(out, id+": hanya ada di PostgreSQL")
		}
	}
	sort.Strings(out)
	return out
}
