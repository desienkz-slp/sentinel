# Phase 2 — Conversation AI

## File dibuat/diubah

- `internal/conversation/sufficiency.go` — matriks kecukupan data per intent tanpa meminta data yang harus diperoleh adaptor eksternal.
- `internal/conversation/sufficiency_test.go` — uji kebutuhan data dan perlindungan terhadap pertanyaan data API.
- `internal/conversation/flow.go` — klasifikasi intent deterministik, ekstraksi fakta pelanggan, transisi lifecycle case, dan proyeksi balasan customer-safe.
- `internal/conversation/flow_test.go` — uji transisi hanya ke `READY_FOR_DIAGNOSIS` setelah data wajib lengkap, intent tidak jelas tetap menunggu klarifikasi, dan balasan tidak membocorkan internal.
- `orchestrator/reports/phase_2.md` — laporan implementasi ini.

## Test yang lulus

- `go test ./internal/conversation -count=1`
- `go test ./... -count=1`

Semua paket Go lulus, termasuk `ainoc/internal/conversation` dan `ainoc/internal/caseengine`.

## Asumsi

- Identitas WhatsApp telah tersedia dari gateway sebagai `Case.Identity`; conversation tidak meminta nomor atau ID pelanggan kembali.
- Resolusi customer/service/device dari Billing, RADIUS, MikroTik, dan GenieACS tetap pekerjaan adaptor baca-saja pada phase berikutnya. Tidak ada endpoint eksternal baru yang diasumsikan.
- Intent awal yang didukung: `slow_connection`, `internet_down`, `intermittent`, dan `wifi_issue`. Pesan di luar kategori ini tetap `WAITING_CUSTOMER` dengan pertanyaan klarifikasi.

## Risiko

- Deteksi intent dan ekstraksi fakta masih berbasis aturan kata kunci Bahasa Indonesia; variasi bahasa yang lebih luas perlu diuji sebelum dipakai sebagai satu-satunya classifier produksi.
- State case saat ini in-memory pada level domain; persistensi transaksional conversation/message/case membutuhkan PostgreSQL sesuai gate Phase 1.
- Customer-safe response secara sengaja tidak memakai `InternalFacts`; integrasi ke pengirim WhatsApp harus selalu memakai proyeksi ini, bukan output diagnosis mentah.
