# Phase 10 — Production Hardening

## File dibuat/diubah

- `internal/hardening/replay.go` — coordinator replay ber-ID stabil untuk menunda event dependency yang belum selesai, mencegah eksekusi ganda saat duplikat/concurrent, dan mengizinkan retry setelah kegagalan gateway/dependency.
- `internal/hardening/replay_test.go` — replay suite untuk delayed/reordered/duplicate messages, dependency failure, gateway outage/reconnect, API/network/database failure, LLM/tool timeout, race/concurrent case, repeated action, unauthorized action, escalation failure, dan rollback.
- `orchestrator/reports/phase_10.md` — laporan Phase 10 ini.

## Test yang lulus

- `go test ./... -count=1`
- `CGO_ENABLED=1 CC=<WinLibs gcc> go test -race ./... -count=1`
- `go vet ./...`

Semua package lulus; race detector juga lulus. Compiler C WinLibs per-user dipasang karena host awalnya memiliki `CGO_ENABLED=0` dan tidak memiliki `gcc` di `PATH`.

## Asumsi

- ID event dan idempotency key action diberikan caller secara stabil dari pesan/case/approval; payload tidak dipakai sebagai pengganti ID.
- Rollback tersedia hanya untuk aksi eksternal yang sudah memiliki callback rollback terdefinisi dan aman dijalankan.
- Adapter eksternal tetap read-only/nonaktif sesuai registry dan policy saat ini; tidak ada endpoint eksternal baru yang diasumsikan.

## Risiko

- `internal/hardening` adalah guard in-memory untuk worker dan replay terisolasi; ketahanan lintas restart tetap membutuhkan persistence PostgreSQL/outbox/lease yang belum tersedia.
- Kegagalan adapter nyata, database PostgreSQL, dan gateway WhatsApp produksi masih perlu chaos/staging test dengan kredensial serta infrastruktur operator yang terisolasi.
- Replay in-memory belum menggantikan hasil terminal durable per delivery attempt; worker produksi perlu menyimpan outcome per-attempt agar retry setelah kegagalan tidak menyamarkan error pada konsumen yang menunggu.
- Path `gcc` WinLibs perlu diteruskan sebagai `CC` atau shell baru setelah perubahan PATH agar perintah race detector dapat berjalan dari terminal baru.
