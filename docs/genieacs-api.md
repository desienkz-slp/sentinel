# GenieACS — Referensi API (NBI / Northbound Interface)

> Acuan implementasi GenieACSAdapter di NOC Sentinel.
> Sumber: dokumentasi resmi GenieACS (docs.genieacs.com, stable 1.2.x).

## Arsitektur (4 service)

| Service | Port default | Fungsi |
|---|---|---|
| `genieacs-cwmp` | 7547 | ACS yang dihubungi CPE (ONT/modem) via TR-069 |
| `genieacs-nbi` | 7557 | **Northbound Interface** — REST API (yang dipakai NOC) |
| `genieacs-fs` | — | File server (firmware/image) |
| `genieacs-ui` | — | Web GUI (frontend) |

## NBI REST API

Base URL: `http(s)://<host>:7557`

### Baca data (read-only)

| Endpoint | Fungsi |
|---|---|
| `GET /devices/?query={...}` | Cari perangkat (by `_id`, MAC, `_lastInform`, dll.) |
| `GET /devices?query={...}&projection=A,B,C` | Ambil parameter spesifik perangkat |
| `GET /faults` | Daftar fault (gangguan) perangkat |
| `GET /tasks/?query={...}` | Daftar task (pending/riwayat) |
| `GET /presets/` | Daftar preset |
| `GET /provisions/` | Daftar provision |

### Tulis (WRITE — deny-by-default di NOC)

| Endpoint | Fungsi |
|---|---|
| `POST /devices/<id>/tasks?connection_request` | Enqueue task (getParameterValues, refreshObject, setParameterValues, reboot, factoryReset, download) |
| `POST /tasks/<id>/retry` | Retry task gagal |
| `DELETE /devices/<id>` | Hapus device dari DB |
| `PUT /presets/<name>`, `PUT /files/<name>`, `PUT /provisions/<name>` | Kelola preset/file/provision |

## Kunci penting

1. **Query pakai bahasa MongoDB**, bukan param `search=`. Contoh:
   ```
   query={"_id":"202BC1-BM632w-000000"}
   query={"InternetGatewayDevice.WANDevice.1.WANConnectionDevice.1.WANIPConnection.1.MACAddress":"20:2B:C1:E0:06:65"}
   query={"_lastInform":{"$lt":"2017-12-11 13:16:23 +0000"}}
   ```
   Query WAJIB di-URL-encode (percent-encoding), terutama `{`, `}`, `"`, `$`.

2. **Field kunci per device:**
   - `_id` = serial number / device ID (mis. `202BC1-BM632w-000000`)
   - `_lastInform` = waktu inform terakhir → penentu **online/offline**
   - `_tags` = tag yang dipasang via provision
   - Parameter TR-069: `InternetGatewayDevice.*` / `Device.*` (model, manufacturer,
     MAC address, sinyal optik, dsb.)

3. **Autentikasi NBI** — HATI-HATI:
   - NBI bawaan **TIDAK punya auth token header** standar (beda dari Billing/RADIUS).
   - Keamanan umumnya lewat **jaringan internal/VPN** + HTTPS + Roles/Permissions.
   - Ada **CVE-2025-56015** (unauthenticated access di NBI 1.2.13) — pastikan
     GenieACS Anda versi patched atau dibatasi network.

4. **Port** NBI default 7557 (bisa beda per deployment — cek config/config.json).

## Kontrak aman adapter NOC Sentinel

`genieacs.get_device_state` hanya menerima `device_id`/serial GenieACS yang
tepat. Adapter menjalankan satu query server-side `_id` dengan `limit=1` dan
projection tetap untuk `_lastInform`, vendor, model, serta RX power. Bila NBI
mengembalikan lebih dari satu device, adapter menolak respons sebagai tidak
terbatas. Output dibatasi ke state terproyeksi dan tidak menyertakan raw device
ID atau field CPE lain.

`genieacs.get_devices` sengaja tidak tersedia. GenieACS NBI tidak menyediakan
kontrak paginasi dan agregasi yang dapat dipaksakan adapter tanpa menampilkan
raw device IDs; daftar bulk harus tetap ditolak.

Health check hanya membaca `_lastInform` dan mengembalikan hitungan agregat.

## Deteksi online/offline (untuk get_device_state)

Device dianggap **offline** bila `_lastInform` lebih lama dari ambang batas
(mis. 7 hari contoh di dokumentasi). Tidak ada field boolean "online" — harus
dihitung dari `_lastInform`.

## Kaitannya dengan konfigurasi dashboard

Config GenieACS: `genieacs_url` (base NBI, mis. `http://172.18.20.141:7557`).
Token opsional — kebanyakan deployment NBI internal tanpa token header.
