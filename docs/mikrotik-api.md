# MikroTik / RouterOS — Referensi API & API-SSL

> Acuan implementasi MikrotikAdapter di NOC Sentinel.
> Sumber: dokumentasi resmi MikroTik (help.mikrotik.com / manual.mikrotik.com).

## Ringkasan: ada DUA "API" yang berbeda

| | **API (classic / binary)** | **REST API** |
|---|---|---|
| Muncul sejak | RouterOS v3 (semua versi, termasuk v6) | RouterOS **v7.1+** |
| Transport | TCP mentah, protokol biner proprietary | HTTP/HTTPS (JSON) |
| Port default | **8728** (plaintext) / **8729** (TLS) | 80/443 (service `www` / `www-ssl`) |
| Auth | Kalimat `/login` (sentence) | **HTTP Basic Auth** (`user:password`) |
| Bentuk | "sentence": word ber-prefix panjang, ditutup null byte | URL resource + HTTP method |
| Contoh | `/ip/address/print` | `GET /rest/ip/address` |

**Keputusan NOC Sentinel:** pakai **REST API (v7+) via HTTPS**, karena adapter
kita berbasis HTTP (retry/backoff/health) dan single-binary. API biner 8728/8729
butuh implementasi protokol biner sendiri — tidak dipakai dulu.

## API vs API-SSL (protokol biner yang sama, transport beda)

- **API (port 8728) — plaintext.** Tanpa enkripsi. Username+password dikirim
  teks polos di jaringan, bisa di-sniff. Hanya aman di LAN/VPN terpercaya.
- **API-SSL (port 8729) — TLS.** Lapisan TLS membungkus protokol yang sama.
  Dua mode:
  1. **Dengan sertifikat** (di-set di `/ip service` → `certificate`) → client
     bisa verifikasi identitas router via TLS normal (disarankan).
  2. **Tanpa sertifikat** → client pakai *anonymous Diffie-Hellman cipher*
     (terenkripsi, tapi **tanpa autentikasi server** → rentan MITM).

> **Catatan penting:** API-SSL tetap mengirim password plaintext di level
> protokolnya. Yang melindungi adalah *terowongan TLS*-nya, bukan protokol API.
> Untuk produksi: pakai sertifikat, bukan mode anonymous DH.

## Metode login (dua era)

- **Pra-6.43**: challenge-response MD5 (`=ret=` challenge → client balas
  `=response=00<md5>`) — password tidak dikirim polos.
- **6.43+**: plaintext login (`/login =name=admin =password=...` → `!done`).
  Password polos, jadi **wajib SSL** di jaringan tidak terpercaya.

## REST API (yang kita pakai)

- Enable: `/ip service enable www` (HTTP, v7.9+) atau `/ip service enable www-ssl`
  (HTTPS). Base path: `http(s)://<router>/rest`.
- Auth: HTTP Basic Auth (user+password user router; default `admin` tanpa pass).
- **Semua nilai JSON dikode sebagai string** (angka & boolean pun string).
- Method: `GET`=print, `PATCH`=set, `PUT`=add, `DELETE`=remove, `POST`=perintah
  konsol arbitrer.
- Sertifikat default MikroTik = **self-signed** → client perlu `curl -k`
  (di adapter kita: `InsecureTLS`).

### Endpoint yang dipakai NOC Sentinel (read-only)

| Endpoint | Data | Dipakai untuk |
|---|---|---|
| `GET /rest/system/resource` | version, board-name, uptime, cpu-load | Health/ping |
| `GET /rest/ppp/active` | name, service, caller-id, address, uptime | `mikrotik.get_pppoe_status` |

Contoh respons `ppp/active`:
```json
[
  {"name":"628123456789@netlayer","service":"pppoe","caller-id":"AA:BB","address":"10.0.0.5","uptime":"1h2m"}
]
```

## Catatan keamanan resmi (kutipan)

> "We do not advise enabling HTTP access (www service). The main risk is that
> authentication credentials can be read with passive eavesdropping."

Untuk produksi: REST via **HTTPS (`www-ssl`) + sertifikat**, bukan HTTP `www`.
HTTP `www` hanya untuk tes di jaringan yang dijamin aman.

## Kaitannya dengan konfigurasi di dashboard

Konfigurasi MikroTik di `⚙ Pengaturan` memakai field: `host`, `port`,
`username`, `password`, `tls` (HTTPS/HTTP). Base URL dibangun otomatis:
`http(s)://host[:port]/rest`. Env yang menang: `NOC_MIKROTIK_HOST/PORT/USER/PASS/TLS`.

## API biner 8728/8729 (referensi, TIDAK dipakai dulu)

- Login sentence: `["/login", "=name=admin", "=password=..."]` → `!done`.
- Daftar library Go: `github.com/cloudwatt/terraform-provider-routeros` (tidak
  relevan), `github.com/ddelnano/mikrotik` (client REST), atau implementasi
  protokol biner sendiri (lihat blog "Implementing MikroTik's Binary API
  Protocol").
- Hanya perlu bila target router masih RouterOS v6 (tanpa REST).
