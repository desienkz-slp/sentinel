# Phase 7 — Incident Correlation

## File dibuat/diubah

- `internal/incident/correlation.go` — mesin korelasi deterministik untuk duplikat dan mass incident berdasarkan intent, waktu, serta topologi terverifikasi (upstream, PON/OLT, OLT, router, atau area).
- `internal/incident/incident.go` — menambah model `Topology`, kebijakan/hasil korelasi, ID korelasi, dan ringkasan mass incident.
- `internal/incident/correlation_test.go` — cakupan duplicate suppression (dengan dan tanpa topologi), grouping mass incident menurut topologi/waktu, stabilitas group ID, dan penolakan korelasi untuk topologi atau jendela waktu berbeda.

## Test yang lulus

- `go test ./internal/incident -count=1`
- `go test ./... -count=1`

Semua package lulus pada 2026-10-02.

## Asumsi

- Metadata topologi hanya boleh diisi dari sumber sistem yang terverifikasi; field kosong tidak dipakai untuk mengorelasikan pelanggan.
- Default jendela korelasi adalah 15 menit dan ambang mass incident adalah 3 pelanggan, tetapi keduanya dapat diberikan melalui `CorrelationPolicy`.
- PON merupakan domain yang lebih spesifik daripada OLT/router/area; pelanggan pada PON berbeda tidak dikelompokkan hanya karena OLT-nya sama.

## Risiko

- Integrasi pengisian topologi dari Billing/OLT/monitoring live belum dilakukan karena kontrak/topologi API eksternal belum tersedia dan adapter eksternal tetap read-only/nonaktif.
- Penyimpanan JSON saat ini adalah fallback pengembangan; korelasi lintas proses membutuhkan repository transaksional PostgreSQL pada fase durable core.
