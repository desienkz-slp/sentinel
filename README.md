# NOC Sentinel — AI NOC berbasis Go + WhatsApp Gateway

Sistem NOC (Network Operations Center) yang mendiagnosis gangguan jaringan **secara otomatis
lewat WhatsApp**. Agen AI memilih dan menjalankan probe sendiri, lalu menyimpulkan akar masalah
beserta rekomendasi konkret untuk teknisi.

- **100% Go, nol dependensi eksternal** untuk inti sistem (`go.mod` tanpa `require`)
- **Satu perintah install**, satu perintah jalan
- **WhatsApp Gateway sudah termasuk** — Go yang menyalakan & mengawasinya
- **Dashboard web** di-embed ke binary (`go:embed`), tanpa build frontend
- Terhubung ke **LLM apa pun** yang OpenAI-compatible (9Router, OpenAI, Ollama, vLLM, LM Studio)
- **Eskalasi otomatis ke Codex CLI** saat keyakinan agen rendah

---

## Mulai cepat

```bash
./install.sh     # cek + pasang semua kebutuhan, build, jalankan unit test
./start.sh       # jalankan aplikasi
```

- Windows: `start.bat` (klik dua kali juga bisa)
- Pasang dependensi saja: `install.sh --no-build`
- Periksa tanpa memasang apa pun: `install.sh --check`

Lalu buka **http://127.0.0.1:8090**.

**Pertama kali pakai:**

1. Panel **WhatsApp Gateway** → tunggu status **✅ berjalan**
2. Klik **Tampilkan QR pairing** → scan dengan WhatsApp (Pengaturan → Perangkat Tertaut)
3. **Isi allowlist nomor** — tanpa ini siapa pun bisa memicu diagnostik ke jaringan Anda

   **Di mana:** dashboard → panel **WhatsApp Gateway** → kolom
   **"Allowlist nomor (pisah koma)"** → tulis nomor → klik
   **"Simpan pengaturan WA"**. Perubahan langsung ditulis ke `config.json`,
   jadi bertahan setelah aplikasi di-restart.

   ⚠️ **Mengosongkan kolom ini = SEMUA nomor boleh** (tidak ada penyaring).
   Isi minimal nomor Anda sendiri.
4. Kirim pesan WhatsApp ke nomor gateway → dibalas laporan otomatis

---

## Cara kerja

```
Pelanggan / teknisi WhatsApp
        │  pesan (chat pribadi; grup diabaikan bawaan)
        ▼
WhatsApp Gateway (Node/Baileys, child process :3001)
        │  POST payload ke N8N_WEBHOOK_URL (di-set otomatis oleh Go)
        ▼
ai-noc-go  /api/wa/webhook
        │
        ├─► AGEN (loop OODA): LLM memilih tool → Go menjalankan probe
        │     ping → dns → tcp → http → traceroute → radius → service
        │
        ├─► LLM menyimpulkan: VERDICT / KEYAKINAN / AKAR_MASALAH / BUKTI / REKOMENDASI
        │
        └─► keyakinan < 60% atau LLM gagal? → eskalasi ke Codex CLI
        ▼
Gateway mengirim field "reply" kembali ke chat  ◄── perilaku bawaan webhook.js
```

---

## Isi sistem

| Komponen | Isi |
|---|---|
| **Dashboard** | `web/index.html` di-embed ke binary; streaming langkah live (SSE) |
| **REST API** | `/api/*` — lihat tabel di bawah |
| **Agen** | Loop OODA + function calling; maks `max_steps` langkah per sesi |
| **9 tool jaringan** | `ping`, `traceroute`, `dns`, `tcp`, `http`, `radius`, `interface`, `service`, `system` |
| **WhatsApp** | Gateway Node/Baileys di-vendor di `wa-gateway/`, dikelola Go |
| **Supervisor** | Menjalankan, memantau, dan me-restart gateway otomatis |
| **Codex bridge** | `codex exec` dipanggil dari Go sebagai mesin eskalasi |
| **Keamanan** | Validasi target ketat, tanpa shell, whitelist tool, allowlist nomor |

---

## Cara berpikir agen (bertahap, bukan seragam)

**Standar konteks dijalankan KODE, bukan penilaian model** — supaya perilaku tetap sama
walau model diganti. Detail lengkap: **[STANDARD.md](STANDARD.md)**.

Agen **tidak** menjalankan semua probe untuk setiap pesan. Alurnya:

```
Pesan masuk
   │
   ├─ TAHAP 1 — PAHAMI: apa keluhannya? perlu dicek sama sekali?
   │     sapaan / pertanyaan umum / broadcast  → jawab langsung, TANPA tool
   │     target tidak jelas                    → tanya balik, TANPA probe buta
   │     keluhan nyata                         → lanjut
   │
   ├─ TAHAP 2 — CEK SESUAI KELUHAN (1-3 probe saja, yang relevan)
   │     "lambat"              → ping (latensi & loss), lalu http/tcp
   │     "tidak bisa buka situs" → dns, lalu http
   │     "mati total"          → ping gateway lokal, baru ping publik
   │     "putus-nyambung"      → ping paket banyak, lalu traceroute
   │     "wifi lemah"          → interface, lalu ping gateway
   │     "PPPoE/RADIUS gagal"  → radius, lalu service
   │
   └─ TAHAP 3 — SIMPULKAN (VERDICT/KEYAKINAN/BUKTI/REKOMENDASI)
```

Balasan menyesuaikan jenis pesan:

Balasan menyesuaikan jenis pesan **dan** menyesuaikan apakah percakapan baru atau lanjutan —
"Halo" hanya di pesan pertama, dan bot tidak meminta ID pelanggan karena nomor pengirim
sudah diketahui.

| Pesan | Yang dilakukan | Yang diterima pengirim |
|---|---|---|
| "halo" | tidak ada pengecekan | obrolan biasa, tanpa header laporan |
| "tahlil kan ?" (tidak jelas) | tidak ada pengecekan | minta perjelas keluhan |
| broadcast "STATUS USER…" | tidak ada pengecekan | diakui singkat |
| "internet lambat sejak pagi" | agen **memilih sendiri** target & probe | laporan diagnosis lengkap |
| "cek ke 8.8.8.8" | target dipakai apa adanya | laporan diagnosis lengkap |

Kalau pengirim tidak menyebut target, agen yang menentukan alamat mana yang masuk akal untuk
keluhan itu — dan laporan menampilkan alamat yang **benar-benar dicek**, bukan tanda "—".

**Riwayat di dashboard** menandai balasan tanpa pengecekan sebagai `CHAT` (bukan
`TIDAK DIKETAHUI`) supaya tidak terlihat seperti diagnosis yang gagal.

> Sebelumnya sistem ini **memukul rata**: pesan seperti `"tahlil kan ?"` memicu 9 probe
> (`interface`, `ping`×3, `dns`, `system`, `service`×2, `http`) ke alamat yang tidak diminta,
> lalu mencoba eskalasi ke Codex. Fungsi penebak target otomatis sudah dihapus.

### Standar konteks, sesi per nomor, dan cache

Tiga mekanisme yang membuat perilaku konsisten dan hemat:

| Mekanisme | Isi |
|---|---|
| **Standar konteks** | Klasifikasi intent + gerbang tool dijalankan **kode Go**, bukan model. Intent non-keluhan → **daftar tool dikosongkan**, jadi model tidak bisa memaksa probe. |
| **Sesi per nomor** | Riwayat & target terakhir **terpisah per pengirim**. Konteks pelanggan A tidak bercampur dengan B. |
| **Cache** | Kunci `hash(nomor + pertanyaan ternormalisasi)`. Pertanyaan sama dari nomor sama → jawaban dipakai ulang. **Dikosongkan otomatis saat model berganti.** |

Hasil uji:

```
GERBANG INTENT (kode, bukan model)
  "halo"                    intent=CHAT       tool: (TIDAK ADA)
  "STATUS USER: …"          intent=INFO       tool: (TIDAK ADA)
  "tahlil kan ?"            intent=UNCLEAR    tool: (TIDAK ADA)
  "apakah bisa cek jaringan?" intent=UNCLEAR  tool: (TIDAK ADA)
  "internet saya lambat"    intent=COMPLAINT  tool: [ping, http]

CACHE & ISOLASI
  1) nomor A, "internet lambat…"        cache=False  18.399 ms
  2) nomor A, "Internet Lambat…" (beda) cache=True        0 ms
  3) nomor B, pertanyaan sama           cache=False  24.884 ms  ← konteks terpisah
```

Panel **Standar Konteks & Sesi** di dashboard menampilkan versi standar, rasio cache
hit/miss, dan tabel percakapan aktif per nomor. Standar bisa diganti tanpa build ulang
lewat `NOC_STANDARD_DOC=/path/standar.md`.

---

## Kebutuhan sistem

| Kebutuhan | Versi | Wajib? | Untuk apa |
|---|---|---|---|
| **Go** | ≥ 1.24 | ✅ | Membangun binary |
| **Node.js + npm** | ≥ 18 (LTS) | ✅ | WhatsApp Gateway (Baileys) |
| **Git** | apa saja | ✅ | Versi & backup |
| **9Router** atau LLM OpenAI-compatible lain | — | ✅ | Otak agen |
| **API key** penyedia LLM | — | ✅ | Autentikasi model |
| **Codex CLI** | terbaru | ⚪ opsional | Eskalasi analisis mendalam |
| **Hermes** | — | ❌ **tidak perlu** | Tidak ada di jalur runtime |

`install.sh` memeriksa semuanya, memasang yang kurang (winget / choco / brew / apt / dnf / pacman),
lalu memverifikasi hasilnya. Idempoten — aman dijalankan berulang kali.

> **Bukti lepas dari Hermes.** Aplikasi dijalankan tanpa satu pun variabel Hermes
> (`env -u HERMES_CUSTOM_9ROUTER_API_KEY …`) → diagnosis penuh berhasil
> (`verdict=SEHAT conf=100`, tools `ping→dns→tcp`), Codex berhasil, sesi WhatsApp tetap CONNECTED.
> API key diambil dari `~/.codex/auth.json`. Satu-satunya jejak Hermes di kode adalah **nama
> env var** (`internal/config/config.go:197`), dan itu pun punya 3 cadangan di belakangnya.

---

## Konfigurasi

**Setup model lengkap: [MODEL_SETUP.md](MODEL_SETUP.md)** — tiga tempat konfigurasi model,
hasil uji function calling semua kandidat, preset cepat/seimbang/mendalam, dan troubleshooting.

Urutan prioritas: **default → `config.json` → environment → `~/.codex/auth.json`**

```bash
cp config.example.json config.json   # opsional, semua nilai bisa lewat env
```

| Env | Default | Keterangan |
|---|---|---|
| `NOC_ADDR` | `:8090` | alamat listen |
| `NOC_LLM_BASE_URL` | `http://127.0.0.1:20128/v1` | endpoint OpenAI-compatible |
| `NOC_LLM_API_KEY` | *(otomatis)* | fallback: `HERMES_CUSTOM_9ROUTER_API_KEY` → `OPENAI_API_KEY` → `~/.codex/auth.json` |
| `NOC_LLM_MODEL` | `ag/gemini-3.8-flash-high` | model analisis (wajib dukung function calling) |
| `NOC_LLM_TIMEOUT` | `120` | timeout LLM (detik) |
| `NOC_MAX_STEPS` | `12` | maks langkah tool per sesi |
| `NOC_DIAG_TIMEOUT` | `30` | timeout per probe (detik) |
| `NOC_CODEX_PATH` | `codex` | path ke `codex.exe` native |
| `NOC_CODEX_SANDBOX` | `danger-full-access` | sandbox Codex |
| `NOC_WA_BASE_URL` | `http://127.0.0.1:3001` | base URL gateway (fallback `N8N_WEBHOOK_URL`) |
| `NOC_WA_TIMEOUT` | `45` | timeout panggilan ke gateway (detik) |
| `NOC_WA_ALLOWLIST` | *(kosong = SEMUA nomor boleh)* | nomor diizinkan, pisah koma |
| `NOC_WA_AUTO_REPLY` | `true` | kirim hasil diagnosis kembali ke WhatsApp |
| `NOC_WA_ASYNC` | `false` | `true` = balas "diterima" lalu kirim hasil menyusul |
| `NOC_WA_AUTOSTART` | `true` | Go menyalakan gateway otomatis saat start |
| `NOC_WA_DIR` | *(otomatis)* | lokasi folder gateway |
| `NOC_WA_GROUP` | `false` | `true` = proses juga pesan dari grup |
| `NOC_STANDARD_DOC` | *(kosong)* | path file standar konteks pengganti (lihat [STANDARD.md](STANDARD.md)) |
| `NOC_CACHE_TTL_MIN` | `10` | umur cache jawaban per nomor (menit) |
| `NOC_SESI_TTL_MIN` | `120` | percakapan dianggap selesai setelah idle (menit) |

Semua nilai juga bisa diubah dari **panel ⚙ Pengaturan** di dashboard (tanpa restart), termasuk
tombol **Tes koneksi LLM** dan **Tes Codex CLI**.

---

## REST API

| Method | Path | Fungsi |
|---|---|---|
| GET | `/api/health` | liveness |
| GET | `/api/status` | konfigurasi (key ter-mask) + status Codex |
| POST | `/api/config` | ubah base URL / key / model / max_steps / model Codex / setelan WA saat runtime |
| POST | `/api/llm/ping` | **completion nyata** untuk verifikasi kredensial (bukan sekadar daftar model) |
| GET | `/api/models` | daftar model dari endpoint |
| GET | `/api/tools` | daftar tool diagnostik |
| POST | `/api/diag` | satu probe manual `{tool, target}` |
| POST | `/api/ask` | sesi diagnosis penuh; `stream:true` → SSE (`step`, `report`) |
| POST | `/api/codex` | tugas analisis ke Codex CLI `{task}` |
| GET | `/api/reports` | riwayat insiden |
| GET | `/api/sesi` | statistik sesi per nomor + cache + versi standar |
| POST | `/api/sesi/cache` | kosongkan cache jawaban |
| POST | `/api/wa/webhook` | **penerima pesan WhatsApp masuk** dari gateway |
| GET | `/api/wa/status` | status sesi WhatsApp (tertaut / belum login) |
| GET | `/api/wa/qr` | QR pairing WhatsApp |
| POST | `/api/wa/connect` / `/api/wa/logout` | kontrol sesi gateway |
| POST | `/api/wa/send` | kirim pesan/notifikasi WhatsApp `{to, message}` |
| GET | `/api/wa/gateway` | status proses gateway (state, PID, log) |
| POST | `/api/wa/start` · `/stop` · `/restart` | kontrol proses WhatsApp Gateway |
| GET | `/api/whatsapp/*` | API internal gateway Node (diproxy, otomatis) |

Contoh:

```bash
# diagnosis penuh
curl -X POST http://127.0.0.1:8090/api/ask -H 'Content-Type: application/json' \
  -d '{"query":"pelanggan lapor lambat sejak pagi","target":"8.8.8.8"}'

# streaming live
curl -N -X POST http://127.0.0.1:8090/api/ask -H 'Content-Type: application/json' \
  -d '{"query":"cek DNS","target":"google.com","stream":true}'

# probe RADIUS (khas ISP)
curl -X POST http://127.0.0.1:8090/api/diag -H 'Content-Type: application/json' \
  -d '{"tool":"radius","target":"10.10.10.1:1812"}'

# notifikasi keluar
curl -X POST http://127.0.0.1:8090/api/wa/send -H 'Content-Type: application/json' \
  -d '{"to":"628123456789","message":"*Gangguan NOC*\nUplink ke IX terdeteksi loss."}'
```

---

## Integrasi WhatsApp (embedded)

WhatsApp Gateway **sudah termasuk di dalam aplikasi ini** (`wa-gateway/`, Node + Baileys).
Tidak perlu menjalankan apa pun secara terpisah — **Go yang mengurusnya**:

- Menjalankan `npm install` otomatis **sekali** saat pertama start (±30 detik)
- Menyalakan proses Node, memantau kesehatannya lewat `/api/whatsapp/health`
- **Restart otomatis** bila proses mati tak sengaja
- Mematikan gateway saat aplikasi dihentikan (tidak ada proses menggantung)
- Mengarahkan webhook gateway ke `/api/wa/webhook` di aplikasi ini

Semua dikendalikan dari **panel WhatsApp di dashboard** (`http://127.0.0.1:8090`):
▶ Mulai · ■ Hentikan · ↻ Restart · status proses + log langsung.
Seluruh kontrol WhatsApp ada di panel ini — **tidak ada halaman terpisah**. Cukup satu alamat: **`http://127.0.0.1:8090`**.

### Kenapa loop-nya tertutup tanpa patch gateway

`wa-gateway/app/webhook.js` sudah punya baris ini:

```js
if (resp.data && resp.data.reply && sessionManager) {
  await sessionManager.sendMessage(payload.chat_id, resp.data.reply);
}
```

Jadi cukup mengisi field `reply` pada respons → gateway mengirimkannya ke chat. Tidak ada
modifikasi logika gateway, hanya penyesuaian timeout (lihat bagian timeout di bawah).

### Contoh balasan yang diterima pelanggan

Balasan dipisah dua bagian: **pesan manusiawi untuk pelanggan**, lalu **ringkasan
teknis ringkas** untuk teknisi.

```
pak wifi saya mati total dari tadi malam

→ Halo Pak, mohon maaf atas ketidaknyamanannya ya. Dari pengecekan sistem kami,
  jalur jaringan dan koneksi internet utama saat ini normal serta lancar, jadi
  kemungkinan kendalanya ada pada perangkat router di rumah. Boleh coba cabut
  kabel adaptor router sekitar 30 detik lalu colokkan kembali ya Pak, sambil
  diperhatikan apakah lampu indikatornya menyala normal atau ada yang merah/mati.
  Kalau setelah di-restart wifinya masih mati total, kabari saya ya biar langsung
  kami jadwalkan teknisi untuk cek ke lokasi.

  ———
  ✅ SEHAT · 90%
  Penyebab: Jaringan gateway ISP dan internet normal, kendala diduga pada
  router/ONT lokal pelanggan atau suplai daya perangkat.
```

Obrolan biasa dibalas tanpa ringkasan teknis sama sekali:

```
terima kasih pak   →  Sama-sama, Pak. Senang bisa membantu.
                      Kalau nanti ada kendala lagi, langsung kabari saja ya.
```

### Pilihan mode

| Mode | Perilaku | Kapan dipakai |
|---|---|---|
| **Sinkron** (default) | Pelanggan menunggu 10–60 detik, lalu menerima laporan lengkap | Pelanggan terbiasa menunggu |
| **Async** | Balasan "diterima" instan, hasil menyusul otomatis | WhatsApp jangan terlihat menggantung |
| **Auto-reply mati** | Laporan hanya tersimpan di dashboard, tidak dikirim | Operator ingin meninjau dulu |

Mode async berjalan di goroutine dengan `context.Background()` sendiri — bukan
context request yang sudah dibatalkan (kalau salah, LLM gagal `context canceled`).

### Hanya balas chat pribadi (bawaan)

Pesan dari **grup WhatsApp diabaikan** secara bawaan. Grup NOC biasanya berisi
broadcast otomatis (`STATUS USER`, `TERHUBUNG KEMBALI`) yang kalau diproses akan
membanjiri antrean diagnosis dan mengirim balasan ke seluruh anggota grup.

Deteksi memakai JID: grup selalu berakhiran **`@g.us`** (diperiksa pada `chat_id`
maupun `sender`), pengguna `@s.whatsapp.net`. Filter dijalankan di ai-noc-go,
bukan di gateway, supaya bisa diubah tanpa restart.

Pesan grup yang diabaikan tetap dicatat di log:

```
[WA] diabaikan (pesan grup): 120363000000000000@g.us
```

Kalau ingin grup internal tertentu ikut diproses (mis. grup NOC tempat operator
mengetik pertanyaan), ubah **Pesan dari grup WhatsApp → Proses juga** di panel
dashboard, atau `NOC_WA_GROUP=true`.

### Timeout gateway harus lebih longgar dari durasi diagnosis

`wa-gateway/app/webhook.js` memakai `timeout: 180000` (3 menit). Nilai aslinya 45 detik
dan itu **memutus diagnosis di tengah jalan**: agen butuh 45–90 detik (beberapa probe
berurutan + LLM), sehingga gateway menutup koneksi lebih dulu dan hasilnya selalu
`TIDAK DIKETAHUI` dengan error `dibatalkan / timeout`.

Kalau diagnosis makin berat (tool lebih banyak, model lebih lambat), naikkan nilai itu — dan
jaga agar anggaran waktu agen berikut tetap di bawahnya:

```
NOC_LLM_TIMEOUT × 2 + NOC_MAX_STEPS × NOC_DIAG_TIMEOUT + 120 detik
```

### Menguji tanpa nomor WhatsApp

`tools/mock_wa_gateway.py` meniru gateway (kontrak endpoint identik) untuk uji
otomatis tanpa scan QR:

```bash
python tools/mock_wa_gateway.py --port 3099                          # gateway tiruan
python tools/mock_wa_gateway.py --target http://127.0.0.1:8090/api/wa/webhook \
       --simulate "cek jaringan ke 8.8.8.8"                          # kirim pesan masuk
```

### Mode eksternal (opsional)

Kalau ingin memakai gateway yang dijalankan sendiri (mis. di server lain), hapus
atau ganti nama folder `wa-gateway/` — aplikasi otomatis kembali ke mode eksternal
dan memakai `NOC_WA_BASE_URL` seperti biasa.

---

## Keamanan

- **Target divalidasi ketat** sebelum menyentuh `exec.Command`: host/IP/host:port/URL saja.
  Ditolak: `8.8.8.8; calc`, `8.8.8.8 && whoami`, `$(whoami)`, `--help`, backtick, pipe, redirect.
  (Diuji di `internal/diag/diag_test.go` — `TestAllowed`.)
- **Tidak ada shell** — selalu `exec.Command(bin, args...)` dengan argumen terpisah.
- **API key tidak pernah dikirim ke browser** — `/api/status` hanya mengembalikan versi ter-mask.
- **Tool terbatas** — whitelist 9 tool; LLM tidak bisa meminta perintah shell bebas.
- **WhatsApp allowlist** — tanpa allowlist, siapa pun yang tahu nomor gateway bisa memicu
  diagnostik ke jaringan Anda. Isi `wa_allowlist` sebelum dipakai sungguhan.
  Allowlist **kosong berarti terbuka untuk semua nomor** — jangan dikosongkan saat produksi.
  Pengecualian: pesan dari **nomor gateway itu sendiri** selalu diproses, supaya operator
  bisa menguji bot dengan mengirim chat ke dirinya sendiri tanpa menambah nomornya ke daftar.
- **Kredensial sesi WhatsApp** ada di `wa-gateway/data/auth/` — setara login akun WhatsApp Anda.
  Sudah masuk `.gitignore`; **jangan pernah di-commit**.
- **Webhook WA tanpa autentikasi** — bind gateway dan ai-noc-go ke localhost, atau tambahkan
  header rahasia di reverse proxy. Jangan buka `/api/wa/webhook` ke internet.
- `/api/config` dan `/api/codex` **tanpa autentikasi** (untuk LAN tepercaya). Sebelum diekspos ke
  internet, tambahkan reverse proxy + auth, dan turunkan `codex_sandbox: danger-full-access`.

---

## Struktur

```
ai-noc-go/
├─ install.sh                   # installer semua kebutuhan (satu perintah)
├─ start.sh / start.bat         # jalankan aplikasi
├─ main.go                      # entrypoint, autostart gateway, graceful shutdown
├─ server.go                    # routing HTTP + SSE + proxy gateway + embed web
├─ web/index.html               # dashboard (di-embed ke binary)
├─ config.example.json
├─ MODEL_SETUP.md               # panduan setup model
├─ STANDARD.md                  # panduan standar konteks, sesi & cache
├─ wa-gateway/                  # WhatsApp Gateway (Node/Baileys) — embedded
│  ├─ app/{server,session,qr,webhook,health}.js
│  ├─ public/                   # UI bawaan gateway (tidak dipakai; kontrol ada di dashboard)
│  └─ package.json              # node_modules dibuat otomatis saat start pertama
├─ tools/mock_wa_gateway.py     # gateway WA tiruan untuk uji tanpa QR
└─ internal/
   ├─ config/config.go          # default → json → env → auth.json Codex
   ├─ llm/client.go             # klien OpenAI-compatible (SSE + function calling)
   ├─ diag/diag.go              # 9 probe jaringan + guard keamanan
   ├─ agent/agent.go            # loop OODA, parser verdict, eskalasi
   ├─ wa/gateway.go             # klien WhatsApp Gateway (kontrak ai-noc)
   ├─ wa/format.go              # format laporan untuk WhatsApp
   ├─ supervisor/supervisor.go  # jalankan & awasi proses Node gateway
   └─ codexbridge/codexbridge.go# eksekusi codex exec dari Go
```

---

## Test

```bash
go test ./...        # 90 unit test
go vet ./...         # bersih
```

Mencakup: guard injeksi target, paket RADIUS RFC 2865, parser SSE (konten & tool call bertahap),
parser `VERDICT/KEYAKINAN`, klasifikasi intent (chat/info/keluhan/tidak jelas), isolasi sesi
per nomor, cache (normalisasi + TTL + pemisahan per nomor), pembersihan banner Codex,
pemilihan `codex.exe` native di atas shim `.CMD`, allowlist WhatsApp (normalisasi nomor
`@s.whatsapp.net`/`@c.us`/`+62`), deteksi pesan grup, deteksi sesi tertaut, pemformatan laporan WA,
dan supervisor proses gateway (deteksi folder hilang, npm install, ring buffer log, Stop idempoten).

---

## Troubleshooting

| Gejala | Penyebab | Solusi |
|---|---|---|
| Hasil selalu `TIDAK DIKETAHUI` + `dibatalkan / timeout` | timeout gateway terlalu pendek | naikkan `timeout` di `wa-gateway/app/webhook.js` |
| `Invalid value: 'ultra'` | model `cx/*` dipakai sebagai model analisis | pakai `ag/*` atau `grip/*` — lihat [MODEL_SETUP.md](MODEL_SETUP.md) |
| `401 Invalid API key` (Codex) | `~/.codex/auth.json` masih mode OAuth | set `{"auth_mode":"apikey","OPENAI_API_KEY":"…","tokens":null}` |
| Agen tidak memanggil tool | model tidak mendukung function calling | ganti model — lihat [MODEL_SETUP.md](MODEL_SETUP.md) |
| Gateway minta QR padahal sudah tertaut | sesi lama bermasalah (`Stream Errored`) | start dibatalkan otomatis & kredensial diamankan; scan QR baru |
| Pesan grup tidak dibalas | filter grup aktif (bawaan) | aktifkan **Proses juga** di panel WhatsApp |
| `npm install` gagal saat start | jaringan / registry | jalankan `./install.sh` untuk melihat log lengkap |
| `Go tidak ditemukan` | Go belum terpasang | `./install.sh` (memasang via winget/brew/apt) |

---

## Pelajaran teknis (mahal ditemukan ulang)

**1. Codex + provider kustom + API key → `401 Invalid API key`.**
Codex CLI menyimpan OAuth ChatGPT di `~/.codex/auth.json` dan **tetap mengirim token itu** meski
`model_provider` diarahkan ke provider kustom. Perbaikan: ubah `auth.json` ke mode API key
(`{"auth_mode":"apikey","OPENAI_API_KEY":"…","tokens":null}`). Menghapus `auth.json` juga jalan.

**2. Prompt multi-baris rusak di Windows.**
`exec.LookPath("codex")` mengembalikan shim npm `codex.CMD` yang berjalan lewat `cmd.exe` dan
memotong argumen multi-baris — agen menjawab seolah formatnya tidak dikirim. Perbaikan: pilih
`codex.exe` native (`CODEX_CLI_PATH` atau pindai `%LOCALAPPDATA%\OpenAI\Codex\bin\*\codex.exe`)
**dan** kirim prompt lewat stdin (`codex exec … -`).

**3. Output Codex: banner di stderr, jawaban di stdout.**
`workdir:`, `model:`, `session id:`, gema prompt, dan `tokens used` ditulis ke **stderr**;
**stdout hanya berisi jawaban akhir agen**. Jangan gabungkan keduanya.

**4. `/v1/models` 200 tidak membuktikan kredensial valid.**
Selalu verifikasi dengan completion nyata — itulah alasan `/api/llm/ping` ada.

**5. Katalog model mengklaim dukungan tools, kenyataannya tidak.**
9Router mengiklankan **92 dari 92 model** mendukung tools; `cx/gpt-5.6-terra` gagal total di
`/v1/chat/completions` dengan `Invalid value: 'ultra'`. Selalu uji function calling sebelum
memilih model.

**6. Context request dibatalkan saat handler selesai.**
Mode async WA harus memakai `context.Background()` sendiri, kalau tidak LLM langsung gagal
`context canceled`.

**7. `registered: false` di `creds.json` Baileys 6.x bukan berarti belum tertaut.**
Flag itu tetap `false` walau sesi sudah CONNECTED. Penanda andal adalah field `me.id`.

---

## Integrasi NetLayer (langkah berikutnya)

`ai-noc-go` sengaja dibuat tanpa dependensi agar mudah ditempel ke stack NetLayer:

1. **RADIUS** — `tool=radius` sudah bisa cek liveness server. Tambahkan `secret` bersama supaya
   probe menghasilkan `Access-Accept`/`Access-Reject` yang sebenarnya.
2. **Database** — tambahkan tool `sql` (read-only, query ter-whitelist) untuk cek status pelanggan
   dari PostgreSQL: sesi PPPoE aktif, tagihan, status isolir.
3. **Mikrotik/OLT** — tambahkan tool REST/SSH terbatas untuk cek interface, PPPoE secret, level optik ONU.
4. **Dashboard** — halaman ini bisa disematkan sebagai iframe di panel admin NetLayer.
