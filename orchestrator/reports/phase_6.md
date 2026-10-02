# Phase 6 — Escalation

## File dibuat/diubah

- `internal/escalation/escalation.go`
  - Menambahkan payload handoff `Context` dengan konteks lengkap untuk NOC Senior/Admin.
  - Menambahkan validasi `MissingRequired()` yang mempertahankan data tidak tersedia sebagai kekurangan konteks, bukan fakta buatan.
  - Menambahkan direktori staff in-memory, seleksi staff aktif dan on-call berdasarkan prioritas, status acknowledgement, timeout, dan fallback.
  - Menegakkan routing deterministik: domain network hanya ke `noc_senior`; domain non-network hanya ke `admin`.
  - Menolak fallback lintas authority role.
- `internal/escalation/escalation_test.go`
  - Menambah pengujian payload lengkap, routing network/non-network, timeout, fallback, dan acknowledgement.
- `orchestrator/reports/phase_6.md`
  - Ringkasan implementasi phase ini.

## Test yang lulus

- `go test ./internal/escalation -count=1` — lulus.
- `go test ./... -count=1` — lulus untuk seluruh package.

## Asumsi

- Direktori staff dan state escalation saat ini in-memory karena belum ada source staff/on-call operator yang dipersisten.
- Pengiriman notifikasi ke WhatsApp/outbox belum dihubungkan karena endpoint dan kontrak delivery eksternal tidak tersedia di repository.
- `ResponseTimeout` menggunakan satuan detik dan default 300 detik apabila policy tidak memberikan nilai positif.

## Risiko

- State assignment, acknowledgement, dan timeout tidak bertahan setelah proses restart sampai repository transaksional/outbox dihubungkan.
- Tidak ada staff aktif/on-call dengan role yang tepat menghasilkan status `EXHAUSTED`; sistem sengaja tidak memindahkan network case ke Admin atau sebaliknya.
- Payload yang belum lengkap tetap dapat dieskalasi dengan daftar kekurangan konteks agar human dapat bertindak tanpa sistem mengarang data.
