# Gateway WhatsApp lokal (simulasi) untuk uji komunikasi

Menggantikan gateway Baileys HANYA untuk uji di PC: tidak memakai jaringan WhatsApp,
tidak membaca `wa-gateway/data/auth`, tidak menyentuh server produksi. Kontrak sama
(webhook masuk, `/api/whatsapp/send`, pemecahan <=500 karakter lewat `app/split.js`).

Isolasi: Sentinel uji di `127.0.0.1:8190`, gateway di `127.0.0.1:3011`, data di
`.localtest/` (di-gitignore). Billing/RADIUS/MikroTik/GenieACS sengaja TIDAK dikonfigurasi.
Nomor staf dan pelanggan semuanya sintetis (62811100000x, 628999000001).

Jalankan:
1. `go build -o .localtest/ai-noc-go.exe .` lalu `cd .localtest && ./ai-noc-go.exe -config config.json`
2. `cd wa-gateway && PORT=3011 NOC_WEBHOOK_URL=http://127.0.0.1:8190/api/wa/webhook node sim/sim-gateway.mjs`
3. `python3 wa-gateway/sim/e2e.py`  (23 pemeriksaan)

Manual: `curl -X POST localhost:3011/sim/inbound -H 'Content-Type: application/json' -d '{"from":"628111000002","text":"..."}'`
lalu `curl localhost:3011/sim/outbox`.

## Ngobrol dengan agent lewat gateway lokal
    python3 wa-gateway/sim/chat.py noc "cek billing <username>"
    python3 wa-gateway/sim/chat.py admin "daftar pelanggan isolir"
    python3 wa-gateway/sim/chat.py 628xxxxxxxxxx "tagihan saya berapa?"   # nomor pelanggan asli = dikenali dari billing
Peran uji: super=628111000001, noc=628111000002, admin=628111000003. Billing memakai data ASLI
(read-only); balasan hanya masuk ke kotak keluar simulasi, tidak dikirim ke WhatsApp pelanggan.
