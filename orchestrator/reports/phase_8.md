# Phase 8 — Knowledge/Learning

## File dibuat/diubah

- `internal/learning/knowledge.go` — lifecycle KB tervalidasi: candidate, antrean review, approval/rejection manusia melalui authority tepercaya, segregation of duties pengusul-reviewer, pola resolusi, koreksi manusia sebagai candidate baru, dan pencatatan feedback.
- `internal/learning/knowledge_test.go` — pengujian gerbang review manusia, larangan promosi otomatis, larangan self-approval, koreksi tanpa menimpa knowledge produksi, dan feedback loop.
- `orchestrator/reports/phase_8.md` — ringkasan implementasi Phase 8.

## Test yang lulus

- `go test ./internal/learning -count=1`
- `go vet ./internal/learning`
- `go test ./... -count=1`

## Asumsi

- Penyimpanan KB saat ini in-memory sebagai fondasi domain; persistence PostgreSQL belum dihubungkan karena repository transaksional yang menjadi source-of-truth belum tersedia.
- `ReviewAuthority` harus dihubungkan ke sistem autentikasi/RBAC operator; tanpa authority eksplisit, approval ditolak secara deny-by-default.
- Identitas pengusul knowledge wajib dicatat; reviewer yang sama tidak dapat menyetujui kandidatnya sendiri.
- Knowledge production hanya berarti entri `APPROVED`; pola statistik/playbook diagnosis yang sudah ada bukan approval knowledge dan tidak dapat mengubah status KB.

## Risiko

- State in-memory tidak bertahan setelah restart dan tidak cocok untuk review lintas instance; perlu repository PostgreSQL dengan audit/outbox sebelum klaim production-live.
- Belum ada endpoint operator untuk submit/review/feedback karena kontrak autentikasi dan RBAC dashboard belum dipastikan; API eksternal tidak diasumsikan.
