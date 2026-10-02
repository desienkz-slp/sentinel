# Phase 5 — Policy/Risk

## File dibuat

- `internal/policy/execution.go` — execution guard fail-closed: allowlist aksi dan scope, klasifikasi LOW/MEDIUM/HIGH/CRITICAL, approval terikat case+aksi+digest parameter, expiry, role eksekutor, dan evaluasi verification plan.
- `internal/policy/phase5_test.go` — pengujian allowlist, enforcement tier risiko/scope, approval parameter-bound dan expired, otoritas role, serta verification evidence yang kontradiktif.
- `orchestrator/reports/phase_5.md` — laporan hasil Phase 5 ini.

## File diubah

- `internal/policy/policy.go` — menambahkan tier `CRITICAL`, normalisasi risk engine, dan enforcement `max_scope`.
- `policies/default-policy.yaml` — menambahkan konfigurasi `critical` yang wajib approval dan membatasi HIGH/CRITICAL ke satu pelanggan.

## Test yang lulus

- `go test ./internal/policy -count=1` — lulus.
- `go vet ./...` — lulus tanpa keluaran.
- `go test ./... -count=1` — lulus untuk seluruh package.

## Asumsi

- Tidak ada adapter write eksternal yang aktif. Execution guard disediakan sebagai gerbang deterministik sebelum capability write reversible yang dikendalikan Phase remediasi berikutnya dihubungkan.
- Approval disimpan in-memory karena belum ada repository approval transaksional PostgreSQL pada scope perubahan ini. Kontrak binding case+aksi+parameter+expiry tidak berubah saat penyimpanan durable ditambahkan.
- HIGH dan CRITICAL wajib disetujui serta dieksekusi oleh `NOC_SENIOR`; aksi lain dapat mendeklarasikan `RequiredRole` sendiri.

## Risiko

- Approval in-memory hilang saat proses restart; jangan aktifkan aksi write sampai storage durable tersedia.
- `VerificationPlan` memverifikasi bukti yang disediakan caller; integrasi dengan sumber kebenaran RADIUS/MikroTik/GenieACS tetap memerlukan adapter write terkontrol dan contract test staging.
- Allowlist hanya aman bila semua jalur mutation diwiring melalui `ExecutionGuard`; saat adapter write pertama ditambahkan, jalur bypass harus ditolak oleh test integrasi.
