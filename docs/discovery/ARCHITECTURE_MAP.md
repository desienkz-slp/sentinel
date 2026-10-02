# Peta Arsitektur Phase 0

Status inspeksi: 2026-10-02 (UTC+07:00).

Dokumen ini memetakan keadaan yang ditemukan di repository dan host pengembangan. “Ada di kode” tidak berarti “aktif di produksi”. Status runtime dan kontrak eksternal dipisahkan agar tidak mengarang ketersediaan layanan.

## Diagram

```text
Pelanggan/Operator WhatsApp
          |
          | Baileys WebSocket
          v
WhatsApp Gateway Node.js (:3001)
  - sesi/QR/kirim pesan/health
  - POST inbound ke N8N_WEBHOOK_URL
          |
          | mode embedded: supervisor Go mengubah URL langsung ke
          | http://127.0.0.1:8090/api/wa/webhook
          v
NOC Sentinel Go (:8090, dashboard + REST + SSE)
  |       |             |                 |
  |       |             |                 +--> JSON lokal: memory, incident, audit
  |       |             +--> Policy -> Registry -> Dispatcher -> adapter read-only
  |       +--> Workflow deterministik CUSTOMER_INTERNET_DOWN
  +--> LLM OpenAI-compatible (:20128/v1) dan Codex CLI read-only
          |
          +--> Billing NETORA HTTP /api/noc/v1 (X-NOC-API-Key)
          +--> Radius UI HTTP /api/* (Bearer)
          +--> MikroTik API native TCP 8728/8729/custom
          +--> GenieACS NBI HTTP :7557

Infra opsional (Docker Compose terpisah):
  PostgreSQL 16 + pgvector (:5432)     Redis 7.4 (:6379)
  - migration SQL tersedia            - cache lokal tetap menjadi fallback
  - repository/runner belum terhubung

n8n (:5678)
  - ada sebagai opsi/rute lama pada gateway dan blueprint
  - tidak ada service/workflow n8n di repository ini
  - bukan bagian jalur embedded yang dipakai supervisor Go

Hermes Agent
  - alat pengembangan/orchestrator phase, bukan dependency runtime NOC Sentinel
  - nama env Hermes hanya menjadi salah satu fallback API key LLM
```

## Komponen

| Komponen | Lokasi | Tanggung jawab | Persistensi | Keadaan ditemukan |
|---|---|---|---|---|
| Entry point Go | `main.go` | Wiring config, LLM, agent, workflow, adapter, health, supervisor, HTTP | — | Ada; aplikasi terdeteksi mendengar pada port 8090 |
| HTTP server/dashboard | `server.go`, `web/` | REST, SSE, dashboard embedded, proxy WA, security guard | incident/audit/memory melalui store | Ada; source default loopback, proses aktif teramati pada wildcard IPv4/IPv6 sehingga konfigurasi proses aktif perlu diverifikasi |
| Agent dan reasoning | `internal/agent`, `internal/correlation`, `internal/standard` | Klasifikasi, OODA, workflow, korelasi UNKNOWN-safe, respons | session/memory lokal | Ada dan diuji unit |
| Policy/tool gateway | `internal/policy`, `internal/registry`, `internal/tool`, `tools/registry.yaml` | deny-by-default, izin/risk, dispatch adapter | YAML | Ada; seluruh tool eksternal tetap `enabled: false` |
| Workflow | `internal/workflow`, `workflows/customer-internet-down.yaml` | Langkah diagnosis deterministik | YAML | Ada; bergantung pada resolusi identitas dan adapter yang belum diaktifkan |
| Adapter eksternal | `internal/{billing,radius,mikrotik,genieacs}` | Akses read-only ke sumber bukti | config/env | Implementasi dan mock/unit test ada; belum ada bukti contract test staging |
| WhatsApp gateway | `wa-gateway/`, `internal/wa`, `internal/supervisor` | Baileys, QR, sesi, inbound/outbound, restart | `wa-gateway/data/auth` dan opsional PostgreSQL gateway | Port 3001 aktif; NOC health menilai ONLINE, tetapi probe langsung `/api/whatsapp/health` timeout saat inspeksi |
| Database | `migrations/`, `internal/db` | Skema PostgreSQL/pgvector, konfigurasi PostgreSQL/Redis | PostgreSQL/Redis | Compose ada; daemon Docker tidak aktif, port 5432/6379 tidak mendengar, kode koneksi/runner masih placeholder |
| Fallback lokal | `internal/{memory,incident,audit,cache,dedupe,session}` | Operasi saat DB/Redis tidak tersedia | JSON/memori | Aktif sebagai fallback |
| LLM/Codex | `internal/llm`, `internal/codexbridge` | Conversation/reasoning dan analisis CLI | konfigurasi lokal | 9Router port 20128 aktif; health aplikasi melaporkan LLM OFFLINE karena kredensial proses aktif tidak valid; Codex dipaksa read-only oleh config |
| Hermes | `orchestrator/`, instalasi host | Menjalankan phase pengembangan | state/report orchestrator | Hermes Agent v0.21.3 terpasang; tidak ada pada jalur runtime |
| n8n | referensi di blueprint, compose WA, dan `webhook.js` | Opsi otomasi integrasi | di luar repo | CLI tidak terpasang, port 5678 tidak aktif, tidak ada workflow/export/compose n8n di repo |

## Alur Runtime

1. `main.go` memuat konfigurasi dengan prioritas default → file `config.json` → environment → fallback kredensial LLM.
2. Policy, registry, workflow, incident, audit, cache, dedupe, health, agent, dan dispatcher dibangun dalam proses Go.
3. Bila folder `wa-gateway/` ditemukan dan autostart aktif, supervisor menjalankan Node.js dan mengarahkan `N8N_WEBHOOK_URL` langsung ke webhook Go. Nama variabel adalah warisan kompatibilitas; n8n tidak dilalui pada mode ini.
4. Gateway menerima pesan Baileys, membentuk payload inbound, lalu POST ke webhook. Respons dengan field `reply` dikirim kembali ke WhatsApp.
5. Webhook Go melakukan dedupe, filter grup/blocklist, lalu menjalankan agent. Keluhan yang cocok memakai workflow deterministik; tool eksternal tetap melewati registry → policy → adapter.
6. Registry saat ini menonaktifkan semua tool eksternal. Bukti yang tidak tersedia harus tetap `UNKNOWN`, bukan ditebak.
7. Health probe memeriksa LLM, gateway WA, PostgreSQL, dan Redis setiap 30 detik. PostgreSQL/Redis yang tidak tersedia menurunkan status ke `DEGRADED` dan memakai fallback lokal.

## Batas Kepercayaan Peta

- Peta kode berasal dari source repository, bukan klaim bahwa endpoint eksternal dapat dijangkau.
- Snapshot runtime hanya berlaku pada waktu inspeksi; tidak menyimpan nilai secret atau isi `config.json` lokal.
- Kontrak Billing dan RADIUS berasal dari dokumen internal repository dan adapter. Kontrak GenieACS/MikroTik berasal dari referensi serta implementasi lokal, tetapi belum dibuktikan terhadap staging operator.
- `docs/mikrotik-api.md` masih menyatakan keputusan REST, sedangkan kode aktif memakai API native biner. Kode dan preferensi arsitektur saat ini diperlakukan sebagai keadaan aktual; konflik dicatat dalam analisis gap.
