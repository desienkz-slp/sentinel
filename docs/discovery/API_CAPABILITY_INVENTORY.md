# Inventaris API dan Kapabilitas Phase 0

Legenda status:

- TERIMPLEMENTASI: ada jalur kode lokal dan unit/mock test.
- TERSEDIA LOKAL: teramati aktif pada host saat inspeksi.
- NONAKTIF: sengaja tidak dapat dipanggil karena registry/config.
- BELUM TERVERIFIKASI: kontrak ada, tetapi belum diuji ke staging nyata.
- REFERENSI SAJA: hanya dokumentasi/konfigurasi, bukan service di repo.

## NOC Sentinel

Sumber: `server.go`, `main.go`, `internal/*`. Server memuat 39 pola handler `/api/*`, termasuk proxy prefix. Semua route `/api/*` melewati operator guard kecuali `/api/wa/webhook` yang melewati webhook guard pada source saat ini.

| Kelompok | Method/path yang dimaksud | Kapabilitas | Status/catatan |
|---|---|---|---|
| Health/status | `GET /api/health`, `GET /api/status`, `GET /api/blueprint` | Liveness/dependency, config teredaksi, status blueprint | TERIMPLEMENTASI; proses aktif mengembalikan health tanpa token, mengindikasikan binary/config aktif tidak sama dengan source terbaru atau guard belum aktif |
| Config/model | `POST /api/config`, `POST /api/llm/ping`, `GET /api/models` | Ubah config, completion nyata, katalog model dua endpoint | TERIMPLEMENTASI; secret dimask; mutasi config memerlukan operator guard di source |
| Diagnostik/agent | `POST /api/diag`, `GET /api/tools`, `POST /api/ask` | Probe allowlisted, daftar tool, diagnosis penuh/SSE | TERIMPLEMENTASI; target jaringan default-deny bila allowlist kosong |
| Riwayat/konteks | `GET /api/reports`, `GET /api/sesi`, `GET /api/memory`, `POST /api/memory/simpan`, `POST /api/sesi/cache`, `GET /api/learning` | Laporan, sesi, memory, cache, pola belajar | TERIMPLEMENTASI; sebagian masih JSON/in-memory |
| Policy/tool | `POST /api/policy/decide`, `GET /api/registry`, `GET /api/tool/adapters`, `POST /api/correlate` | Dry policy, registry, adapter, korelasi bukti | TERIMPLEMENTASI; dry/read-only |
| External checks | `GET /api/billing/check`, `GET /api/radius/check`, `GET /api/mikrotik/check`, `GET /api/mikrotik/routers`, `GET /api/genieacs/check` | Uji semantik koneksi adapter | TERIMPLEMENTASI; belum diverifikasi ke staging nyata |
| Incident/audit | `GET /api/incidents`, `GET /api/audit` | Riwayat insiden dan audit append-only | TERIMPLEMENTASI dengan store JSON fallback |
| Codex | `POST /api/codex` | Analisis CLI manual | TERIMPLEMENTASI; config memaksa sandbox read-only |
| WhatsApp facade | `POST /api/wa/webhook`, `GET /api/wa/status`, `GET /api/wa/qr`, `POST /api/wa/connect`, `POST /api/wa/logout`, `POST /api/wa/reconnect`, `POST /api/wa/send` | Inbound, pairing, sesi, outbound | TERIMPLEMENTASI |
| Supervisor WA | `GET /api/wa/gateway`, `POST /api/wa/start`, `POST /api/wa/stop`, `POST /api/wa/restart` | Lifecycle child process | TERIMPLEMENTASI |
| Proxy WA | `/api/whatsapp/*` | Proxy terbatas menuju gateway Node | TERIMPLEMENTASI bila supervisor embedded tersedia |

Kapabilitas non-HTTP utama: klasifikasi intent berbasis kode, workflow deterministik, policy COPILOT, dispatcher deny-by-default, cache/dedupe, normalisasi identitas, health periodik, fallback JSON/lokal, dashboard embedded, dan installer satu perintah.

## WhatsApp

Sumber: `wa-gateway/app/server.js`, `wa-gateway/app/webhook.js`, `internal/wa`, `internal/supervisor`.

| Method/path | Kapabilitas | Status/catatan |
|---|---|---|
| `GET /api/whatsapp/status` | Status sesi Baileys | TERIMPLEMENTASI |
| `GET /api/whatsapp/qr` | QR pairing | TERIMPLEMENTASI |
| `POST /api/whatsapp/connect`, `/reconnect`, `/logout` | Lifecycle sesi | TERIMPLEMENTASI |
| `GET /api/whatsapp/session`, `/logs`, `/health` | Metadata, log, health | TERIMPLEMENTASI; probe health langsung timeout saat inspeksi |
| `POST /api/whatsapp/send` | Kirim pesan | TERIMPLEMENTASI |
| `POST /api/whatsapp/simulate-query` | Simulasi pipeline | TERIMPLEMENTASI; default menuju orchestrator eksternal `:8000`, bukan route Go `:8090` |
| outbound webhook | POST payload inbound ke `N8N_WEBHOOK_URL`; fallback `ORCHESTRATOR_URL` | TERIMPLEMENTASI; supervisor embedded mengarahkan URL pertama langsung ke `/api/wa/webhook` |

Gateway mendengar pada `0.0.0.0:3001` dalam source Node. Tidak ada autentikasi di endpoint Node langsung. Kredensial Baileys berada di `wa-gateway/data/auth/` dan harus tetap tidak terlacak Git.

## Billing

Sumber kontrak: `NOC_AGENT_API.md` §20–21 dan `internal/billing`.

| Kontrak | Nilai terverifikasi dari repo |
|---|---|
| Base path adapter | `/api/noc/v1` |
| Auth | `X-NOC-API-Key`; key 64-hex dikelola lewat endpoint konfigurasi Sanctum terpisah |
| Read yang dipakai | `GET /customers?search=...&per_page=10`; health `GET /customers?per_page=1` |
| Tool | `billing.get_customer` |
| Data | akun/status/isolir, paket, area, router, RADIUS, koordinat, kontak |
| Registry | NONAKTIF (`enabled: false`) |
| Bukti live | BELUM TERVERIFIKASI; endpoint/kredensial staging tidak tersedia pada environment inspeksi |

Dokumen internal juga menginventarisasi endpoint NETORA `/api/v1` untuk monitoring, router, RADIUS, pelanggan, pembayaran, isolir, dan konfigurasi key. Endpoint mutasi itu tidak diekspos oleh adapter NOC Sentinel Phase 0.

## RADIUS

Sumber kontrak: `radius_api.md` dan `internal/radius`.

| Kontrak | Nilai terverifikasi dari repo |
|---|---|
| Transport | HTTP REST Radius UI, bukan RADIUS UDP |
| Auth | `Authorization: Bearer <machine token>` |
| Read yang dipakai | `GET /api/sessions`, `GET /api/users`, `GET /api/system/stats` |
| Tool | `radius.get_session`, `radius.get_user`, `radius.get_system_stats` |
| Kapabilitas referensi lain | auth/accounting log, traffic harian, NAS, profile, FUP, tunnel/VPN; termasuk mutasi yang belum diekspos adapter |
| Registry | NONAKTIF (`enabled: false`) |
| Bukti live | BELUM TERVERIFIKASI |

Catatan keamanan: response `/api/users` menurut kontrak memuat password cleartext. Formatter teks menyembunyikannya, tetapi objek `Data` adapter saat ini masih membawa field password; ini dicatat sebagai gap kebocoran data.

## GenieACS

Sumber: `docs/genieacs-api.md`, `internal/genieacs`.

| Kontrak | Nilai terverifikasi dari repo |
|---|---|
| Base | NBI HTTP(S), port default 7557 |
| Auth | NBI standar tidak menyediakan token header; pembatasan jaringan/VPN diperlukan |
| Read yang dipakai | `GET /devices/?projection=...`, `GET /devices/?query=<MongoDB-JSON-terencode>` |
| Tool | `genieacs.get_device_state`, `genieacs.get_devices` |
| Status online | dihitung dari `_lastInform`; implementasi memakai ambang 7 hari |
| Registry | NONAKTIF (`enabled: false`) |
| Bukti live | BELUM TERVERIFIKASI |

Endpoint task/reboot/set parameter/delete hanya tercatat sebagai kemampuan NBI WRITE dan tidak diimplementasikan oleh adapter.

## MikroTik

Sumber aktual: `internal/mikrotik`; referensi konflik: `docs/mikrotik-api.md`.

| Kontrak | Nilai terverifikasi dari kode aktual |
|---|---|
| Transport | API native RouterOS (protokol sentence biner) TCP |
| Port | 8728 plaintext, 8729 TLS, atau custom |
| Auth | `/login` plaintext RouterOS 6.43+ dan fallback challenge MD5 legacy |
| Read yang dipakai | `/system/resource/print`, `/system/identity/print`, `/ppp/active/print`, `/interface/print`, `/interface/monitor-traffic` |
| Tool | `mikrotik.get_pppoe_status`, `mikrotik.get_interface_stats`, `mikrotik.get_interface_live`, `mikrotik.get_customer_traffic` |
| Multi-router | Didukung melalui `mikrotik_routers` dan pool adapter |
| Registry | Semua read NONAKTIF; deklarasi write `mikrotik.disconnect_pppoe` juga NONAKTIF dan belum punya adapter |
| Bukti live | BELUM TERVERIFIKASI |

TLS native memakai `InsecureSkipVerify`; penggunaan hanya dapat diterima pada jaringan terkontrol sampai trust certificate yang benar tersedia.

## Hermes

- Hermes Agent v0.21.3 terpasang di host dan dipakai oleh `orchestrator/run.sh` untuk session pengembangan per phase.
- Hermes tidak di-import dan tidak diperlukan oleh binary NOC Sentinel.
- `HERMES_CUSTOM_9ROUTER_API_KEY` hanya salah satu fallback konfigurasi LLM; ini bukan integrasi runtime Hermes.
- Port 20128 aktif sebagai endpoint OpenAI-compatible/9Router, tetapi health proses NOC aktif melaporkan HTTP 401 saat inspeksi. Nilai key tidak dibaca atau dicatat.

## n8n

- Blueprint memberi n8n peran integrasi/webhook/notifikasi, bukan reasoning inti.
- `wa-gateway/docker-compose.yml` menunjuk `http://noc-n8n:5678/webhook/whatsapp-inbound` pada network eksternal.
- `webhook.js` memiliki default localhost `:5678` dan fallback orchestrator `:8000`.
- Mode embedded yang dibangun `internal/supervisor` mengganti `N8N_WEBHOOK_URL` menjadi webhook Go `:8090`; dengan demikian n8n dilewati.
- Tidak ada image/service n8n, workflow export, credential mapping, atau contract test n8n di repository. CLI n8n tidak terpasang dan port 5678 tidak aktif saat inspeksi. Status: REFERENSI SAJA/BELUM TERSEDIA.

## Database dan Environment

- Compose `deploy/noc-infra.compose.yml`: PostgreSQL `pgvector/pgvector:pg16` dan Redis 7.4, seluruh port host loopback-only, volume persisten, healthcheck, password wajib dari `deploy/.env`.
- Compose WA terpisah bergantung pada network eksternal `noc_internal`, PostgreSQL, n8n, dan orchestrator eksternal.
- Migration `001_autonomous_noc.sql` dan `002_case_engine.sql` tersedia. `internal/db` baru memuat konfigurasi/DSN teredaksi; belum ada pool, migration runner, repository, atau Redis client aktif.
- Docker CLI tersedia, tetapi daemon tidak aktif. Port PostgreSQL 5432, Redis 6379, dan n8n 5678 tidak mendengar saat inspeksi.
- Toolchain teramati: Go 1.27.0, Node.js 22.23.2, npm 10.9.8, Git 2.54.0.windows.1. `go.mod` menargetkan Go 1.25.0.
- Environment yang diamati hanya dicatat berdasarkan nama variabel, tidak nilainya. Tidak ada nama `NOC_*`, `N8N_*`, PostgreSQL, atau Redis yang aktif pada session inspeksi.
