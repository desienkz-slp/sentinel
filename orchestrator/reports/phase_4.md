# Phase 4 — Tool Gateway

## File dibuat/diubah

- `internal/tool/tool.go` — dispatcher kini gagal tertutup bila registry atau policy tidak tersedia. Adapter tidak dapat dieksekusi tanpa kedua gerbang tersebut.
- `internal/tool/tool_test.go` — menambahkan pengujian penolakan saat registry atau policy tidak tersedia.
- `internal/tool/phase4_contract_test.go` — kontrak integrasi lokal untuk Billing, RADIUS, MikroTik, dan GenieACS: setiap tool adapter harus tercatat pada registry produksi, bertipe `READ`, nonaktif secara default, dan ditolak dispatcher.
- `orchestrator/reports/phase_4.md` — laporan Phase 4 ini.

## Test yang lulus

- `go test ./internal/tool -count=1`
- `go build -o bin/ai-noc-go.exe .`
- `go test ./... -count=1`

Semua paket lulus. Kontrak lokal mencakup adapter Billing, RADIUS, MikroTik API native RouterOS, dan GenieACS NBI.

## Asumsi

- Kontrak endpoint dan autentikasi adapter yang telah ada tetap menjadi sumber implementasi: Billing NETORA memakai `/api/noc/v1` dan `X-NOC-API-Key`; RADIUS memakai HTTP REST dengan Bearer token; MikroTik memakai API native RouterOS; GenieACS memakai NBI read-only.
- Tidak ada endpoint eksternal baru yang dibuat atau diasumsikan pada Phase 4 ini.
- Operator harus mengaktifkan tool secara eksplisit pada `tools/registry.yaml` hanya setelah endpoint staging dan kredensial least-privilege tervalidasi.

## Risiko

- Uji kontrak ini bersifat lokal dan tidak menggantikan validasi staging terhadap endpoint nyata; kredensial serta endpoint staging belum tersedia di repository.
- Semua tool eksternal tetap `enabled: false`, sehingga integrasi live belum dapat dijalankan sampai operator melakukan konfigurasi dan validasi terkontrol.
- Tool tulis tetap tidak memiliki adapter dan tetap ditolak oleh registry/policy; tidak ada aksi eksternal yang diaktifkan oleh perubahan ini.
