# Phase 1 — Foundation

## File dibuat/diubah

- `internal/caseengine/case.go`: aggregate Case, ID `CASE-YYYYMMDD-XXXXXX`, graph state privat, event transition, version, dan invariant `RESOLVED` wajib memiliki verification sukses.
- `internal/caseengine/case_test.go`: uji format ID, transisi ilegal, invariant verification, jalur escalation/human handling, dan penolakan verification di luar `VERIFYING`.
- `internal/caseengine/context.go`: model customer context yang memisahkan fakta sistem dari conversation, serta staff directory in-memory deterministik untuk staff aktif/on-call.
- `internal/caseengine/context_test.go`: uji validasi customer context dan pemilihan staff aktif/on-call menurut role.
- `internal/audit/audit.go`: metadata audit case lengkap; argumen, hasil, before, dan after disalin lalu direduksi rekursif untuk key sensitif sebelum persistensi. File audit fallback dibuat mode `0600`.
- `internal/audit/audit_test.go`: uji preservasi metadata audit case dan redaksi nilai sensitif.
- `migrations/002_case_engine.sql`: migration additive untuk conversation/message, cases/events/verification/escalation/human decision, idempotency, lease, outbox, customer service/context snapshot, staff/availability/permission, audit case, serta trigger PostgreSQL yang menolak `RESOLVED` tanpa verification lulus.

## Test yang lulus

- `go test ./internal/caseengine -count=1`
- `go test ./internal/audit -count=1`
- `go test ./... -count=1`
- `go vet ./...`

## Asumsi

- PostgreSQL adalah source of truth produksi; `internal/caseengine.StaffDirectory` hanya fallback in-memory sampai repository PostgreSQL dan operator directory dihubungkan.
- Migration `002_case_engine.sql` diterapkan setelah `001_autonomous_noc.sql`, karena mereferensikan tabel `customers` dan `audit_logs` yang telah ada.
- Tidak ada endpoint eksternal baru; customer context hanya kontrak internal/proyeksi dari sumber terverifikasi.

## Risiko

- Runner migration dan repository PostgreSQL transaksi belum dihubungkan ke ingestion WhatsApp; schema tersedia tetapi belum dapat diklaim durabel lintas restart.
- Belum ada kredensial/infrastruktur PostgreSQL staging untuk mengeksekusi migration secara integrasi.
- Format ID enam karakter suffix membatasi entropy; repository PostgreSQL wajib menangani pelanggaran unique constraint dengan retry saat wiring persistence Phase 1 lanjutan.
- Routing escalation/fallback staff produksi masih Phase 4; directory Phase 1 hanya menyediakan metadata dan seleksi dasar aktif/on-call.
