# Phase 3 — Reasoning

## File dibuat/diubah

- `internal/reasoning/reasoning.go` — schema input case, kontrak collector evidence read-only, diagnostik, hipotesis, confidence, dan output reasoning terstruktur `reasoning.v1`.
- `internal/reasoning/reasoning_test.go` — pengujian output JSON terstruktur, koleksi evidence, confidence, serta invariant UNKNOWN.
- `orchestrator/reports/phase_3.md` — ringkasan implementasi Phase 3.

## Test yang lulus

- `go test ./internal/reasoning -count=1`
- `go test ./... -count=1`

Semua package dalam `go test ./... -count=1` lulus, termasuk `ainoc/internal/reasoning`.

## Asumsi

- Integrasi endpoint Billing, RADIUS, MikroTik, dan GenieACS belum diaktifkan pada Phase 3 ini. `reasoning.Collector` adalah kontrak read-only yang nanti diimplementasikan melalui dispatcher/registry/policy existing.
- Domain evidence yang diperlukan untuk diagnosis koneksi adalah `billing`, `radius`, `mikrotik`, dan `genieacs`, mengikuti workflow dan korelator yang sudah ada.

## Risiko

- Collector production harus mencatat source dan timestamp evidence dari adapter sebenarnya; interface sudah menyediakan field tersebut tetapi belum ada adapter live yang diaktifkan.
- Kesimpulan deterministik saat ini menggunakan aturan korelasi existing. Diagnosis baru memerlukan aturan dan test eksplisit, bukan fallback berbasis tebakan.
- Bukti yang hilang, error, atau state tidak dikenali selalu dipertahankan sebagai `UNKNOWN`; akibatnya confidence sengaja rendah dan kasus perlu pemeriksaan lanjutan.
