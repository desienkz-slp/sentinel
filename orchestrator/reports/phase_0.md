# Laporan Phase 0 — Discovery

## File dibuat/diubah

- `docs/discovery/ARCHITECTURE_MAP.md` — peta komponen, dependency, alur runtime, deployment opsional, dan batas kepercayaan hasil inspeksi.
- `docs/discovery/API_CAPABILITY_INVENTORY.md` — inventaris API/kapabilitas NOC Sentinel, WhatsApp, Billing, RADIUS, GenieACS, MikroTik, Hermes, n8n, database, Docker, dan environment.
- `docs/discovery/GAP_ANALYSIS.md` — gap terprioritas, asumsi, risiko, bukti source, dan arah tindak lanjut tanpa mengaktifkan integrasi eksternal.
- `internal/discovery/discovery.go` — validator deterministik untuk artefak discovery, sambil mempertahankan validasi baseline Phase 0 yang sudah ada.
- `internal/discovery/discovery_test.go` — test exit-gate dokumen Phase 0 dari root repository.
- `orchestrator/reports/phase_0.md` — laporan hasil phase ini.

## Test yang lulus

- RED terverifikasi: `go test ./internal/discovery -count=1` gagal karena fungsi `Validate` belum tersedia.
- Target GREEN: `go test ./internal/discovery -count=1` lulus (`ok ainoc/internal/discovery`).
- Suite penuh: `go test ./... -count=1` lulus untuk package root dan seluruh 35 package `internal/*`, termasuk `internal/discovery`.

## Asumsi

- Urutan phase mengikuti `AI_AGENT_HERMES_MASTER_SPEC.md` §47 dan `orchestrator/phases.json`; penomoran pada delivery plan diperlakukan sebagai workstream capability yang berbeda.
- Dokumen kontrak Billing/RADIUS di repo adalah referensi internal, tetapi belum menjadi bukti konektivitas staging.
- Tidak adanya nama environment `NOC_*` pada session inspeksi tidak membuktikan isi `config.json` lokal; secret lokal sengaja tidak dibaca atau disalin ke laporan.
- Listener/health snapshot hanya menggambarkan host pada 2026-10-02 13:09 UTC+07, bukan jaminan production readiness.
- Endpoint eksternal yang belum dikonfirmasi ditandai `BELUM TERVERIFIKASI`; tidak ada endpoint baru yang dibuat berdasarkan tebakan.

## Risiko

- Kritis: objek data adapter RADIUS masih dapat membawa password cleartext; kontrak header webhook WA belum selaras dengan security guard; gateway Node bind wildcard dan endpoint management belum ber-auth.
- Tinggi: PostgreSQL/Redis belum aktif atau terhubung, Docker daemon mati, seluruh adapter eksternal belum diuji staging dan tetap disabled, LLM proses aktif melaporkan 401, serta source/runtime tampak drift.
- Sedang: n8n tidak tersedia di repo/host, fallback gateway mengarah ke orchestrator `:8000` yang tidak ada di repo, compose terfragmentasi, dan dokumentasi MikroTik lama bertentangan dengan implementasi API native.
- Repository sudah memiliki banyak perubahan lain sebelum Phase 0; commit phase ini harus men-stage hanya enam file yang tercantum di atas agar perubahan pengguna lain tidak ikut terambil.
