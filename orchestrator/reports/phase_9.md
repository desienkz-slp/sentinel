# Phase 9 — Observability

## File dibuat

- `internal/observability/observability.go` — collector metrik HTTP in-memory, KPI diagnosis konservatif, dan pembentukan alert deterministik.
- `internal/observability/observability_test.go` — uji counter HTTP, KPI tanpa klaim resolusi tanpa verifikasi, dan alert dependency/kegagalan diagnosis.
- `server_observability_test.go` — uji endpoint KPI, metrik, alert, audit, liveness, dan readiness.
- `orchestrator/reports/phase_9.md` — laporan implementasi phase ini.

## File diubah

- `server.go` — endpoint `/healthz`, `/readyz`, `/api/metrics`, `/api/kpi`, `/api/alerts`; middleware mencatat status/latensi request dan jejak audit akses API tanpa request body atau query string.
- `main.go` — inisialisasi collector observability.
- `web/index.html` — panel dashboard observabilitas untuk KPI dan alert.
- `docs/MASTER_SPEC_ACCEPTANCE_MATRIX.md` — gate observability menjadi `foundation` dengan bukti endpoint yang tersedia.

## Test yang lulus

Perintah: `go test ./... -count=1`

Hasil: seluruh package lulus, termasuk root `ainoc` dan `ainoc/internal/observability`.

## Asumsi

- Laporan agen yang ada belum membawa bukti verifikasi resolusi case. Karena itu `verified_resolved` sengaja bernilai nol; verdict diagnosis `SEHAT` tidak diperlakukan sebagai resolusi terverifikasi.
- Alert disediakan sebagai endpoint/dashboard untuk operator. Pengiriman notifikasi eksternal tidak diaktifkan karena kanal, penerima on-call, dan kebijakan eskalasi alert belum dikonfigurasi.

## Risiko dan batasan

- Counter metrik disimpan di memori dan reset ketika proses restart; persistensi/retensi metrik produksi belum ada.
- Belum ada trace lintas layanan yang durable. Audit request dan audit diagnosis sudah ada, tetapi korelasi trace penuh memerlukan penyimpanan transaksi/telemetri produksi.
- `/api/*` tetap membutuhkan autentikasi operator; hanya `/healthz` dan `/readyz` yang disediakan untuk probe orchestrator tanpa kredensial.
