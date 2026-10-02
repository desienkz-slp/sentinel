# Analisis Gap Phase 0

## Ringkasan

Fondasi lokal cukup luas: server Go, dashboard, gateway WhatsApp, agent, workflow, policy/registry, adapter read-only, migration SQL, health, audit, incident, cache, dan fallback lokal sudah ada. Namun klaim production-ready belum dapat dibuat karena persistence durable belum terhubung, service eksternal belum diuji staging, n8n tidak tersedia, konfigurasi security gateway belum utuh, dan beberapa dokumentasi berbeda dari kode aktual.

Prioritas setelah discovery adalah mempertahankan mode COPILOT/read-only sambil menutup gap kontrak, keamanan, dan persistence secara bertahap. Tidak ada endpoint eksternal baru yang diasumsikan dalam dokumen ini.

## Gap

| ID | Area | Temuan/gap | Dampak | Prioritas | Bukti/lokasi | Arah tindak lanjut |
|---|---|---|---|---|---|---|
| G-01 | PostgreSQL/Redis | `internal/db` hanya konfigurasi dan placeholder; belum ada pool, migration runner, repository, transaksi outbox, atau Redis client | Durable core, dedupe lintas restart, lease, dan outbox belum terpenuhi | Tinggi | `internal/db/db.go`, `migrations/*`, acceptance matrix | Tambahkan runner dan repository secara additive setelah infra staging tersedia |
| G-02 | Docker | Docker CLI ada tetapi daemon tidak aktif; PostgreSQL/Redis tidak mendengar | Migration/contract test infra tidak dapat dibuktikan | Tinggi | snapshot runtime; `deploy/noc-infra.compose.yml` | Operator menyalakan Docker atau memberi environment staging terisolasi |
| G-03 | Adapter eksternal | Billing/RADIUS/GenieACS/MikroTik punya unit/mock test tetapi tidak ada contract test authenticated terhadap staging | Bentuk response, auth, timeout, dan semantic health belum terbukti | Tinggi | package adapter dan acceptance matrix | Verifikasi satu adapter read-only per tahap dengan kredensial least-privilege |
| G-04 | Registry | Semua tool eksternal `enabled: false` | Workflow keluhan menghasilkan bukti UNKNOWN untuk langkah eksternal | Disengaja/tinggi | `tools/registry.yaml` | Tetap nonaktif sampai G-03 lulus |
| G-05 | Identity workflow | Workflow memakai `${identity_id}`/`${device_id_if_known}`, sementara resolusi customer/service/device durable belum terhubung | Adapter dapat menerima identifier yang salah/kosong | Tinggi | `workflows/customer-internet-down.yaml`, delivery Phase 2–3 | Definisikan resolver typed dan contract identifier sebelum aktivasi |
| G-06 | RADIUS PII/secret | `get_user` menyembunyikan password pada teks, tetapi `tool.Output.Data` tetap berisi field `Password` dari response | Password dapat masuk audit, prompt, atau output lain | Kritis | `internal/radius/radius.go` | Proyeksikan tipe aman tanpa password sebelum data keluar dari adapter |
| G-07 | Billing PII | Adapter mengembalikan objek customer penuh (telepon, email, alamat, koordinat) ke `Data` | PII lebih luas dari kebutuhan diagnosis | Tinggi | `internal/billing/billing.go` | Terapkan customer-safe/evidence projection minimum |
| G-08 | Webhook auth WA | Security source mewajibkan `X-NOC-Webhook-Token`, tetapi gateway hanya mengirim `X-AI-NOC-Source`; supervisor tidak menyuntik token/header | Binary source terbaru akan menolak inbound saat token kosong maupun tidak cocok | Kritis | `internal/security/security.go`, `wa-gateway/app/webhook.js`, `internal/supervisor` | Tambahkan secret-bound header dari env/vault dan contract test end-to-end |
| G-09 | Gateway exposure | Node gateway bind `0.0.0.0:3001`, CORS terbuka, endpoint management tidak ber-auth | Jika port terekspos, sesi WA dan send API dapat disalahgunakan | Kritis | `wa-gateway/app/server.js`, compose WA | Bind loopback dalam embedded mode atau wajibkan auth/reverse proxy |
| G-10 | Runtime/source drift | Source default Go loopback + guard, tetapi proses aktif teramati pada wildcard dan `/api/health` dapat diakses tanpa token | Binary/config berjalan mungkin bukan build source terbaru; audit keamanan runtime tidak pasti | Tinggi | snapshot port/API | Rebuild/restart terkontrol lalu ulangi probe boundary |
| G-11 | Gateway health | Port 3001 aktif dan health agregat menyebut ONLINE, tetapi probe langsung health timeout; banyak koneksi `CLOSE_WAIT` teramati | Risiko resource leak atau event-loop macet | Sedang | snapshot runtime | Investigasi health handler/session DB dan lifecycle koneksi tanpa polling buta |
| G-12 | LLM | Endpoint :20128 aktif tetapi NOC health melaporkan HTTP 401 invalid key | Diagnosis berbasis LLM tidak dapat berjalan andal pada proses aktif | Tinggi | `/api/health` snapshot | Perbaiki konfigurasi secret proses; uji completion nyata tanpa mencatat key |
| G-13 | n8n | Tidak ada service/workflow/export n8n di repo; CLI dan port tidak tersedia | Jalur compose WA yang menunjuk `noc-n8n` tidak reproducible | Sedang | compose WA, `webhook.js`, host | Putuskan: hapus ketergantungan dari deployment embedded atau sediakan bundle/versioned workflow |
| G-14 | Orchestrator fallback | Gateway fallback default menuju `:8000/pipeline/whatsapp`, tetapi NOC Sentinel Go berada di `:8090/api/wa/webhook` | Fallback standalone tidak mencapai aplikasi ini kecuali service lain tersedia | Tinggi | `wa-gateway/app/webhook.js`, `app/server.js` | Konfigurasikan satu kontrak ingress yang eksplisit dan diuji |
| G-15 | MikroTik docs | `docs/mikrotik-api.md` menyatakan REST dipakai, sedangkan kode memakai API native biner | Operator dapat membuka service/port yang salah | Tinggi | docs vs `internal/mikrotik/mikrotik.go` | Selaraskan dokumentasi ke API native; tandai REST hanya referensi alternatif |
| G-16 | MikroTik TLS | API-SSL memakai `InsecureSkipVerify` tanpa opsi CA/pinning | Rentan MITM walau traffic terenkripsi | Tinggi | `internal/mikrotik/mikrotik.go` | Tambahkan trust CA/pinning; izinkan insecure hanya sebagai mode eksplisit staging |
| G-17 | Migration | Migration 001 memakai tipe/extension tanpa runner/checksum yang aktif; urutan dan idempotensi belum diuji pada DB bersih | Bootstrap dapat gagal atau berbeda antar host | Tinggi | `migrations/001_*`, `002_*` | Uji apply bersih dan upgrade dengan PostgreSQL nyata sebelum produksi |
| G-18 | Compose terfragmentasi | Infra compose dan WA compose terpisah; WA memakai user DB/network/orchestrator/n8n yang tidak didefinisikan oleh compose infra | `docker compose up` tunggal belum menghasilkan stack utuh | Sedang | kedua compose | Definisikan deployment profile yang konsisten tanpa menyimpan secret |
| G-19 | Dokumentasi/config drift | README dan contoh config masih memuat default/field lama serta belum mencakup seluruh external adapter/security | Setup operator berpotensi salah | Sedang | `README.md`, `config.example.json`, source config | Sinkronkan pada phase implementasi terkait, tanpa menyalin secret lokal |
| G-20 | Method contracts | Sejumlah handler yang dimaksud GET/POST tidak selalu menolak method lain secara eksplisit | Kontrak API kurang ketat dan audit route sulit | Sedang | `server.go` | Tambahkan method guard/table router dan test setelah scope security disepakati |
| G-21 | Observability | Health ada, tetapi metrics/traces/alerts/KPI belum ada | Sulit mengukur SLO dan menemukan bottleneck | Sedang | acceptance matrix | Kerjakan pada Phase 9 setelah core durable |
| G-22 | Learning | Store pola belajar ada, tetapi governance candidate→review→approved belum lengkap | Risiko pola statistik dianggap kebenaran | Tinggi | `internal/learning`, acceptance matrix | Jangan aktifkan self-promotion; bangun human review gate |
| G-23 | Spesifikasi phase | `docs/MASTER_SPEC_DELIVERY_PLAN.md` memakai “Phase 0 — Secure baseline”, sedangkan master spec §47 dan orchestrator memakai “Phase 0 — Discovery” | Nomor phase ambigu untuk audit/commit | Sedang | kedua dokumen | Gunakan §47/orchestrator untuk urutan eksekusi; pertahankan delivery plan sebagai workstream teknis dan tambahkan crosswalk nanti |

## Asumsi

1. `AI_AGENT_HERMES_MASTER_SPEC.md` §47 dan `orchestrator/phases.json` adalah sumber urutan phase untuk tugas ini; delivery plan adalah rencana capability yang penomorannya berbeda.
2. `NOC_AGENT_API.md` dan `radius_api.md` adalah hasil inspeksi source sistem eksternal, tetapi tetap dianggap kontrak internal yang harus dibuktikan di staging sebelum aktivasi.
3. Tidak adanya nama environment `NOC_*` pada session inspeksi tidak membuktikan bahwa file `config.json` lokal kosong. Isi file secret lokal sengaja tidak diinventarisasi.
4. Port aktif hanya membuktikan listener pada host, bukan kesehatan penuh atau kesesuaian binary dengan working tree.
5. API GenieACS dapat berbeda bila operator menambahkan reverse proxy/auth. Adapter saat ini hanya dikonfigurasi dengan URL pada wiring utama.
6. Router target mendukung API native pada port yang disediakan operator. Versi RouterOS, sertifikat, dan izin read-only belum diketahui.
7. Tidak ada endpoint Billing/RADIUS/GenieACS/MikroTik tambahan yang dianggap tersedia di luar dokumen dan implementasi yang disebutkan.

## Risiko

- Kritis: secret RADIUS dapat bocor melalui `tool.Output.Data`; webhook WA terbaru dapat tertolak karena kontrak header tidak selaras; gateway management terbuka bila port 3001 terekspos.
- Tinggi: runtime/source drift, LLM 401, persistence belum durable, adapter belum diuji staging, resolver identitas belum lengkap, dan TLS MikroTik tidak memverifikasi server.
- Sedang: n8n/fallback deployment tidak reproducible, compose terfragmentasi, dokumentasi drift, serta health gateway timeout.
- Risiko perubahan: repository sudah memiliki banyak perubahan dan file untracked sebelum Phase 0. Commit Phase 0 harus membatasi staging pada artefak discovery/laporan agar pekerjaan lain tidak ikut terambil.

## Keputusan Phase 0

- Tidak mengaktifkan tool eksternal dan tidak menambahkan endpoint hasil tebakan.
- Tidak mengubah deployment/runtime yang sedang aktif selama discovery.
- Menjadikan tiga artefak discovery sebagai exit-gate yang diuji oleh package `internal/discovery`.
- Menunda koneksi live dan perubahan keamanan ke phase implementasi yang memiliki test serta environment staging terisolasi.
