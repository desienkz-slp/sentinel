package pgstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"ainoc/internal/handoff"
)

// HandoffRepo menyimpan buku serah-terima di PostgreSQL. Semua operasi idempoten
// (UPSERT), jadi sink boleh dipanggil berulang dan impor boleh diulang.
type HandoffRepo struct{ pool *pgxpool.Pool }

func NewHandoffRepo(pool *pgxpool.Pool) *HandoffRepo { return &HandoffRepo{pool: pool} }

// Upsert menulis satu handoff beserta seluruh update-nya dalam satu transaksi.
func (r *HandoffRepo) Upsert(ctx context.Context, h handoff.Handoff) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("repo PostgreSQL tidak tersedia")
	}
	ev, err := json.Marshal(h.Evidence)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if _, err := tx.Exec(ctx, `
		INSERT INTO handoffs (case_id, customer, domain, complaint, evidence, severity, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9)
		ON CONFLICT (case_id) DO UPDATE SET
			customer=EXCLUDED.customer, domain=EXCLUDED.domain, complaint=EXCLUDED.complaint,
			evidence=EXCLUDED.evidence, severity=EXCLUDED.severity, status=EXCLUDED.status,
			updated_at=EXCLUDED.updated_at`,
		h.CaseID, h.Customer, h.Domain, h.Complaint, string(ev), nullIfEmpty(h.Severity), string(h.Status),
		h.CreatedAt, h.UpdatedAt); err != nil {
		return err
	}
	for i, u := range h.Updates {
		if _, err := tx.Exec(ctx, `
			INSERT INTO handoff_updates (case_id, seq, at, by_role, status, untuk_pelanggan, notified, notify_err)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
			ON CONFLICT (case_id, seq) DO UPDATE SET
				notified=EXCLUDED.notified, notify_err=EXCLUDED.notify_err`,
			h.CaseID, i, u.At, u.By, string(u.Status), u.Untuk, u.Notified, u.NotifyErr); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Get membaca satu handoff lengkap.
func (r *HandoffRepo) Get(ctx context.Context, id string) (handoff.Handoff, bool, error) {
	all, err := r.load(ctx, `WHERE h.case_id = $1`, id)
	if err != nil || len(all) == 0 {
		return handoff.Handoff{}, false, err
	}
	return all[0], true, nil
}

// All membaca semua handoff, tertua dulu.
func (r *HandoffRepo) All(ctx context.Context) ([]handoff.Handoff, error) {
	return r.load(ctx, ``)
}

func (r *HandoffRepo) load(ctx context.Context, where string, args ...any) ([]handoff.Handoff, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT h.case_id, h.customer, h.domain, h.complaint, h.evidence, COALESCE(h.severity,''), h.status,
		       h.created_at, h.updated_at
		FROM handoffs h `+where+` ORDER BY h.created_at, h.case_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []handoff.Handoff
	idx := map[string]int{}
	for rows.Next() {
		var h handoff.Handoff
		var ev []byte
		var st string
		if err := rows.Scan(&h.CaseID, &h.Customer, &h.Domain, &h.Complaint, &ev, &h.Severity, &st, &h.CreatedAt, &h.UpdatedAt); err != nil {
			return nil, err
		}
		h.Status = handoff.Status(st)
		h.Evidence = map[string]string{}
		_ = json.Unmarshal(ev, &h.Evidence)
		h.CreatedAt, h.UpdatedAt = h.CreatedAt.UTC(), h.UpdatedAt.UTC()
		idx[h.CaseID] = len(out)
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	ur, err := r.pool.Query(ctx, `SELECT case_id, seq, at, by_role, status, untuk_pelanggan, notified, notify_err
		FROM handoff_updates ORDER BY case_id, seq`)
	if err != nil {
		return nil, err
	}
	defer ur.Close()
	for ur.Next() {
		var id, by, st, untuk, nerr string
		var seq int
		var at time.Time
		var notified bool
		if err := ur.Scan(&id, &seq, &at, &by, &st, &untuk, &notified, &nerr); err != nil {
			return nil, err
		}
		if i, ok := idx[id]; ok {
			out[i].Updates = append(out[i].Updates, handoff.Update{At: at.UTC(), By: by, Status: handoff.Status(st),
				Untuk: untuk, Notified: notified, NotifyErr: nerr})
		}
	}
	return out, ur.Err()
}

// Count = jumlah handoff.
func (r *HandoffRepo) Count(ctx context.Context) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `SELECT count(*) FROM handoffs`).Scan(&n)
	return n, err
}

// Diff membandingkan sumber (JSON) dengan isi PostgreSQL. Kosong = sama.
// Dipakai mode shadow dan verifikasi impor. Tidak memuat isi pesan di hasil.
func Diff(src []handoff.Handoff, dst []handoff.Handoff) []string {
	var out []string
	sm := map[string]handoff.Handoff{}
	for _, h := range src {
		sm[h.CaseID] = h
	}
	dm := map[string]handoff.Handoff{}
	for _, h := range dst {
		dm[h.CaseID] = h
	}
	for id, s := range sm {
		d, ok := dm[id]
		switch {
		case !ok:
			out = append(out, id+": tidak ada di PostgreSQL")
		case s.Status != d.Status:
			out = append(out, id+": status berbeda")
		case s.Severity != d.Severity:
			out = append(out, id+": keparahan berbeda")
		case len(s.Updates) != len(d.Updates):
			out = append(out, id+": jumlah update berbeda")
		default:
			for i := range s.Updates {
				a, b := s.Updates[i], d.Updates[i]
				if a.Status != b.Status || a.Untuk != b.Untuk || a.Notified != b.Notified {
					out = append(out, fmt.Sprintf("%s: update %d berbeda", id, i))
					break
				}
			}
		}
	}
	for id := range dm {
		if _, ok := sm[id]; !ok {
			out = append(out, id+": hanya ada di PostgreSQL")
		}
	}
	sort.Strings(out)
	return out
}

var _ = pgx.ErrNoRows
