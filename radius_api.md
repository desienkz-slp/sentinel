# NETORA Radius UI — Dokumentasi API (NOC Agent)

> **File ini adalah referensi resmi API NETORA Radius UI untuk NOC Agent.**
> Semua endpoint diakses melalui HTTP dengan autentikasi Bearer Token.
>
> Dibuat: 2026-10-01 | Diperbarui: 2026-10-01 | Source: `radius-ui/server/index.js`

---

## Daftar Isi

1. [Autentikasi](#1-autentikasi)
2. [Role & Akses Endpoint](#2-role--akses-endpoint)
3. [Endpoint: Users PPP](#3-endpoint-users-ppp)
4. [Endpoint: Active Sessions](#4-endpoint-active-sessions)
5. [Endpoint: Auth & Accounting Logs](#5-endpoint-auth--accounting-logs)
6. [Endpoint: Traffic Harian](#6-endpoint-traffic-harian)
7. [Endpoint: NAS](#7-endpoint-nas)
8. [Endpoint: Profiles (Group)](#8-endpoint-profiles-group)
9. [Endpoint: FUP Policy](#9-endpoint-fup-policy)
10. [Endpoint: System Stats](#10-endpoint-system-stats)
11. [Endpoint: Tunnels WireGuard](#11-endpoint-tunnels-wireguard)
12. [Endpoint: VPN Config](#12-endpoint-vpn-config)
13. [Quick Reference NOC Workflow](#13-quick-reference-noc-workflow)
14. [Contoh Penggunaan curl](#14-contoh-penggunaan-curl)

---

## 1. Autentikasi

Semua endpoint `/api/*` (kecuali `/api/auth`) **wajib** menyertakan header:

```http
Authorization: Bearer <TOKEN>
```

### Dua Jenis Token

| Jenis Token | Sumber | Role | Keterangan |
|---|---|---|---|
| **Machine/Global Token** | `.env` -> `API_TOKEN` | `admin` | Token statis untuk machine-to-machine (NOC Agent) |
| **Session Token** | Login via `/api/auth` | `admin` / `noc` / `teknisi` | Token dinamis per sesi user web UI |

> **Rekomendasi NOC Agent:** Gunakan **Machine Token** (`API_TOKEN`) dari `.env` server.
> Token ini memiliki role `admin` sehingga bisa membaca **semua** endpoint data tanpa batasan.

### Cara Mendapatkan Machine Token

Token dikonfigurasi admin di halaman **Config -> Database -> API Token** pada Web UI,
atau langsung edit file `.env` server:

```env
API_TOKEN=your_secret_token_here
```

### Error Response Autentikasi

```json
// 401 Token tidak ada atau tidak valid
{ "error": "Unauthorized: Invalid or missing API Token" }

// 403 Role tidak diizinkan
{ "error": "Forbidden: NOC cannot access this module" }
```

---

## 2. Role & Akses Endpoint

| Endpoint / Modul | admin | noc | teknisi |
|---|:---:|:---:|:---:|
| GET `/api/users` | v | v | v |
| POST/PUT/DELETE `/api/users` | v | v | v (no DELETE) |
| GET `/api/sessions` | v | v | v |
| POST `/api/sessions/kick` | v | v | v |
| GET `/api/logs/auth` | v | v | x |
| GET `/api/logs/accounting` | v | v | x |
| GET `/api/profiles` | v | v | x |
| POST/PUT/DELETE `/api/profiles` | v | v | x |
| GET `/api/fup` | v | v | x |
| GET `/api/nas` | v | v (GET only) | v |
| POST/PUT/DELETE `/api/nas` | v | x | x |
| GET `/api/system/stats` | v | v | v |
| GET `/api/traffic/daily/:username` | v | v | v |
| GET `/api/tunnels` | v | x | x |
| GET `/api/vpn/config` | v | x | x |
| GET `/api/config/*` | v | x | x |

> NOC Agent disarankan menggunakan **Machine Token (role admin)** agar bisa membaca semua endpoint tanpa batasan.

---

## 3. Endpoint: Users PPP

### 3.1 Get All PPP Users

```http
GET /api/users
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "username": "pelanggan01",
    "password": "secret123",
    "profile": "paket-10mbps",
    "nas_ip": "192.168.1.1",
    "last_disconnect": "2026-10-01T08:30:00.000Z"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `username` | string | Username PPP |
| `password` | string | Password Cleartext |
| `profile` | string | Nama group/paket dari `radusergroup` |
| `nas_ip` | string atau null | IP NAS yang di-pin (null = semua NAS) |
| `last_disconnect` | string atau null | Timestamp terakhir sesi berakhir |

---

### 3.2 Create PPP User

```http
POST /api/users
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "username": "pelanggan01",
  "password": "secret123",
  "profile": "paket-10mbps",
  "nas_ip": "192.168.1.1"
}
```

| Field | Required | Keterangan |
|---|:---:|---|
| `username` | Ya | Username PPP (harus unik) |
| `password` | Ya | Password Cleartext |
| `profile` | Tidak | Nama group/paket |
| `nas_ip` | Tidak | Pin ke NAS tertentu; kosongkan untuk semua NAS |

**Response 200:**
```json
{ "message": "User created successfully" }
```

**Response 400:**
```json
{ "error": "Username already exists" }
```

---

### 3.3 Update PPP User

```http
PUT /api/users/:username
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "password": "newpassword",
  "profile": "paket-20mbps",
  "nas_ip": "192.168.1.2"
}
```

**Response 200:**
```json
{ "message": "User updated successfully" }
```

---

### 3.4 Delete PPP User

```http
DELETE /api/users/:username
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{ "message": "User deleted successfully" }
```

---

### 3.5 Disable User (Isolir)

Menonaktifkan user dengan mengubah `Auth-Type` menjadi `Reject`. Password asli di-backup otomatis.

```http
POST /api/users/:username/disable
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{ "message": "User disabled successfully" }
```

---

### 3.6 Enable User (Unisolir)

Memulihkan user dari status disabled. Password diambil dari backup otomatis jika tidak dikirimkan.

```http
POST /api/users/:username/enable
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body (opsional):**
```json
{
  "password": "restoredPassword",
  "profile": "paket-10mbps"
}
```

**Response 200:**
```json
{ "message": "User enabled successfully" }
```

---

### 3.7 Batch Create / Upsert Users

```http
POST /api/users/batch
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "users": [
    { "username": "user1", "password": "pass1", "profile": "paket-10mbps", "nas_ip": "" },
    { "username": "user2", "password": "pass2", "profile": "paket-20mbps" }
  ]
}
```

> Catatan: Endpoint ini bersifat **upsert** — jika username sudah ada, data lama dihapus dan diganti.

**Response 200:**
```json
{ "message": "2 users processed successfully" }
```

---

### 3.8 Batch Delete Users

```http
DELETE /api/users/batch
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "usernames": ["user1", "user2", "user3"]
}
```

**Response 200:**
```json
{ "message": "3 users deleted successfully" }
```

---

### 3.9 Batch Move NAS

Memindahkan sekelompok user ke NAS lain. Opsional langsung kick sesi aktif.

```http
PATCH /api/users/batch-move-nas
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "usernames": ["user1", "user2"],
  "target_nas_ip": "192.168.2.1",
  "kick_active": true
}
```

| Field | Required | Keterangan |
|---|:---:|---|
| `usernames` | Ya | Array username yang akan dipindah |
| `target_nas_ip` | Tidak | IP NAS tujuan; string kosong = hapus pin (All NAS) |
| `kick_active` | Tidak | true = kirim PoD ke NAS lama dan tutup sesi. Default: false |

**Response 200:**
```json
{ "message": "2 users moved and kicked successfully" }
```

---

## 4. Endpoint: Active Sessions

### 4.1 Get Active Sessions

Mengembalikan semua sesi PPP yang **sedang aktif** (`acctstoptime IS NULL`).

Secara otomatis endpoint ini menjalankan **dua operasi cleanup** sebelum return data:
1. **Auto-Recover Zombie Sessions**: Membuka kembali sesi yang tertutup paksa (misal akibat NAS-Reboot), namun MikroTik masih aktif mengirimkan `Interim-Update` dalam **15 menit terakhir** — ditandai dengan `acctupdatetime > acctstoptime`.
2. **Auto-Close Ghost Sessions**: Jika satu username punya lebih dari satu sesi aktif, sesi-sesi lama ditutup dan hanya sesi terbaru (MAX `radacctid`) yang dipertahankan.

```http
GET /api/sessions
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "radacctid": 12345,
    "acctsessionid": "ABC123DEF",
    "username": "pelanggan01",
    "nasipaddress": "192.168.1.1",
    "framedipaddress": "10.10.10.50",
    "acctstarttime": "2026-10-01T06:00:00.000Z",
    "acctsessiontime": 12600,
    "acctinputoctets": 104857600,
    "acctoutputoctets": 524288000,
    "callingstationid": "AA:BB:CC:DD:EE:FF",
    "nas_identifier": "NAS-Utama",
    "profile": "paket-10mbps"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `radacctid` | int | ID unik sesi di radacct |
| `acctsessionid` | string | Session ID dari NAS |
| `username` | string | Username PPP |
| `nasipaddress` | string | IP NAS tempat user terkoneksi |
| `framedipaddress` | string | IP yang diberikan ke user |
| `acctstarttime` | datetime | Waktu mulai sesi (UTC) |
| `acctsessiontime` | int | Durasi sesi dalam detik |
| `acctinputoctets` | int | Total upload dalam bytes (dari sisi NAS) |
| `acctoutputoctets` | int | Total download dalam bytes (dari sisi NAS) |
| `callingstationid` | string | MAC address perangkat user |
| `nas_identifier` | string | Nama pendek NAS (nas.shortname) |
| `profile` | string | Paket/group user |

---

### 4.2 Kick / Disconnect Session

Mengirim **Packet of Disconnect (PoD)** via `radclient` ke NAS dan menutup sesi di database.

```http
POST /api/sessions/kick
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "username": "pelanggan01"
}
```

**Response 200:**
```json
{ "success": true, "message": "Kick command sent for pelanggan01 and session cleared." }
```

**Response 404:**
```json
{ "error": "No active session found for this user." }
```

---

## 5. Endpoint: Auth & Accounting Logs

### 5.1 Authentication Logs

Riwayat percobaan login PPP dari tabel `radpostauth`. Maksimal **500 record terbaru**.

> Prasyarat: Modul `sql` untuk `post-auth` harus aktif di konfigurasi FreeRADIUS.

```http
GET /api/logs/auth
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "id": 9001,
    "username": "pelanggan01",
    "pass": "",
    "reply": "Access-Accept",
    "authdate": "2026-10-01T08:00:00.000Z"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `id` | int | ID record |
| `username` | string | Username yang mencoba login |
| `pass` | string | Password yang dicoba (biasanya dikosongkan) |
| `reply` | string | `Access-Accept` (sukses) atau `Access-Reject` (gagal) |
| `authdate` | datetime | Timestamp percobaan |

> Catatan: Field `class` sudah **dihapus** dari response sejak update 2026-10-01.

---

### 5.2 Accounting / Session History Logs

Riwayat semua sesi (aktif maupun sudah berhenti). Maksimal **500 record terbaru**.

```http
GET /api/logs/accounting
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "radacctid": 12345,
    "username": "pelanggan01",
    "nasipaddress": "192.168.1.1",
    "framedipaddress": "10.10.10.50",
    "acctstarttime": "2026-10-01T06:00:00.000Z",
    "acctstoptime": "2026-10-01T10:30:00.000Z",
    "acctsessiontime": 16200,
    "acctinputoctets": 104857600,
    "acctoutputoctets": 524288000,
    "acctterminatecause": "User-Request",
    "callingstationid": "AA:BB:CC:DD:EE:FF"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `acctstoptime` | datetime atau null | Null = sesi masih aktif |
| `acctsessiontime` | int | Durasi sesi dalam detik |
| `acctterminatecause` | string | Alasan sesi berakhir: `User-Request`, `Admin-Kick`, `NAS-Reboot`, dll. |

---

## 6. Endpoint: Traffic Harian

Data traffic per user per hari dari tabel `app_daily_traffic`.
Rentang data ditentukan oleh `RETENTION_DAYS` di `.env` (default: 30 hari).

```http
GET /api/traffic/daily/:username
Authorization: Bearer <TOKEN>
```

**Contoh Request:**
```http
GET /api/traffic/daily/pelanggan01
```

**Response 200:**
```json
[
  {
    "date": "2026-09-30",
    "upload": 52428800,
    "download": 262144000,
    "total": 314572800
  },
  {
    "date": "2026-10-01",
    "upload": 104857600,
    "download": 524288000,
    "total": 629145600
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `date` | string YYYY-MM-DD | Tanggal |
| `upload` | int | Total upload hari itu (bytes) |
| `download` | int | Total download hari itu (bytes) |
| `total` | int | Total traffic = upload + download (bytes) |

---

## 7. Endpoint: NAS

### 7.1 Get All NAS

```http
GET /api/nas
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "id": 1,
    "nasname": "192.168.1.1",
    "shortname": "NAS-Utama",
    "type": "other",
    "ports": 0,
    "secret": "radtest",
    "server": null,
    "community": null,
    "description": "Router utama gedung A"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `nasname` | string | IP Address NAS |
| `shortname` | string | Nama pendek / identifier |
| `type` | string | Tipe NAS: `other`, `cisco`, `computone`, dll. |
| `secret` | string | RADIUS shared secret |
| `description` | string | Deskripsi bebas |

---

### 7.2 Add NAS (admin only)

```http
POST /api/nas
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "nasname": "192.168.2.1",
  "shortname": "NAS-Cabang",
  "type": "other",
  "ports": 0,
  "secret": "radsecret",
  "description": "Router cabang timur"
}
```

**Response 200:**
```json
{ "id": 2, "message": "NAS added successfully" }
```

> FreeRADIUS akan di-restart otomatis setelah perubahan NAS.

---

### 7.3 Update NAS (admin only)

```http
PUT /api/nas/:id
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "nasname": "192.168.2.1",
  "shortname": "NAS-Cabang-Baru",
  "secret": "newsecret",
  "description": "Updated"
}
```

---

### 7.4 Delete NAS (admin only)

```http
DELETE /api/nas/:id
Authorization: Bearer <TOKEN>
```

---

### 7.5 Test Koneksi NAS / Ping (admin only)

```http
POST /api/nas/test
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{ "ip": "192.168.1.1" }
```

**Response 200:**
```json
{ "success": true,  "message": "Connected"         }
{ "success": false, "message": "Unreachable / RTO" }
```

---

## 8. Endpoint: Profiles (Group)

Profiles = konfigurasi paket internet (Mikrotik Rate-Limit, dsb.) yang disimpan di
tabel `radgroupreply` (atribut reply) dan `radgroupcheck` (atribut check).

### 8.1 Get All Profiles

```http
GET /api/profiles
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "id": 1,
    "groupname": "paket-10mbps",
    "attribute": "Mikrotik-Rate-Limit",
    "op": ":=",
    "value": "10M/10M",
    "type": "reply"
  },
  {
    "id": 2,
    "groupname": "paket-10mbps",
    "attribute": "Simultaneous-Use",
    "op": ":=",
    "value": "1",
    "type": "check"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `groupname` | string | Nama profile/paket |
| `attribute` | string | RADIUS attribute |
| `op` | string | Operator: `:=`, `=`, `+=`, dll. |
| `value` | string | Nilai atribut |
| `type` | string | `reply` dari radgroupreply, `check` dari radgroupcheck |

---

### 8.2 Create Profile Attribute

```http
POST /api/profiles
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "groupname": "paket-20mbps",
  "attribute": "Mikrotik-Rate-Limit",
  "op": ":=",
  "value": "20M/20M"
}
```

> Atribut yang otomatis masuk ke `radgroupcheck`: `NAS-IP-Address`, `Simultaneous-Use`.
> Sisanya masuk ke `radgroupreply`.

---

### 8.3 Bulk Save Profile (Atomic Replace)

Menghapus semua atribut group lama dan menggantinya sekaligus dalam satu transaksi.

```http
POST /api/profiles/bulk
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "groupname": "paket-10mbps",
  "attributes": [
    { "attribute": "Mikrotik-Rate-Limit", "op": ":=", "value": "10M/10M", "type": "reply" },
    { "attribute": "Simultaneous-Use",    "op": ":=", "value": "1",        "type": "check" }
  ]
}
```

**Response 200:**
```json
{ "message": "Profile saved successfully" }
```

---

### 8.4 Update Profile Attribute

```http
PUT /api/profiles/:id
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "groupname": "paket-10mbps",
  "attribute": "Mikrotik-Rate-Limit",
  "op": ":=",
  "value": "10M/10M",
  "type": "reply"
}
```

---

### 8.5 Delete Profile Attribute

```http
DELETE /api/profiles/:id?type=reply
Authorization: Bearer <TOKEN>
```

| Query Param | Value | Keterangan |
|---|---|---|
| `type` | `reply` | Hapus dari `radgroupreply` |
| `type` | `check` | Hapus dari `radgroupcheck` |

---

### 8.6 Delete Entire Profile Group

Menghapus semua atribut milik sebuah group (reply + check) sekaligus.

```http
DELETE /api/profiles/group/:groupname
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{ "message": "Profile group deleted successfully" }
```

---

## 9. Endpoint: FUP Policy

Kebijakan Fair Usage Policy per group. Tersimpan di tabel `app_fup_policies`.

### 9.1 Get All FUP Policies

```http
GET /api/fup
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "id": 1,
    "groupname": "paket-10mbps",
    "quota_bytes": 107374182400,
    "reset_period": "monthly",
    "address_list_name": "FUP-10mbps"
  }
]
```

| Field | Type | Keterangan |
|---|---|---|
| `groupname` | string | Nama group yang dikenai FUP |
| `quota_bytes` | int | Kuota dalam bytes. Contoh: 100 GB = 107374182400 |
| `reset_period` | string | `monthly`, `weekly`, atau `daily` |
| `address_list_name` | string | Nama address-list Mikrotik untuk throttle saat FUP aktif |

---

### 9.2 Create FUP Policy

```http
POST /api/fup
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "groupname": "paket-10mbps",
  "quota_bytes": 107374182400,
  "reset_period": "monthly",
  "address_list_name": "FUP-10mbps"
}
```

---

### 9.3 Update FUP Policy

```http
PUT /api/fup/:id
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

Request body sama seperti Create.

---

### 9.4 Delete FUP Policy

```http
DELETE /api/fup/:id
Authorization: Bearer <TOKEN>
```

---

## 10. Endpoint: System Stats

Statistik server real-time: CPU, RAM, Disk, ukuran database, dan statistik tabel RADIUS.

```http
GET /api/system/stats
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{
  "cpu": {
    "load": "12.5"
  },
  "memory": {
    "total": 8589934592,
    "used": 4294967296,
    "free": 4294967296,
    "usedPercent": "50.0"
  },
  "storage": {
    "total": 107374182400,
    "used": 53687091200,
    "free": 53687091200,
    "usedPercent": 50
  },
  "database": {
    "sizeMB": "128.45"
  },
  "radiusData": {
    "radacctCount": 15000,
    "radcheckCount": 500,
    "radpostauthCount": 25000
  }
}
```

| Field | Type | Keterangan |
|---|---|---|
| `cpu.load` | string | Persentase beban CPU (%) |
| `memory.total` | int | Total RAM dalam bytes |
| `memory.usedPercent` | string | Persentase penggunaan RAM |
| `storage.usedPercent` | number | Persentase penggunaan disk |
| `database.sizeMB` | string | Ukuran database RADIUS dalam MB |
| `radiusData.radacctCount` | int | Total record di tabel `radacct` |
| `radiusData.radcheckCount` | int | Total record di tabel `radcheck` (approx. jumlah user) |
| `radiusData.radpostauthCount` | int | Total record auth log |

---

## 11. Endpoint: Tunnels WireGuard

> Role: `admin` only

### 11.1 Get All Tunnels

```http
GET /api/tunnels
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
[
  {
    "id": 1,
    "name": "node-cabang-timur",
    "public_key": "AbCdEfGhIj...",
    "private_key": "XyZaBcDeFg...",
    "ip_address": "10.8.0.2"
  }
]
```

---

### 11.2 Get WireGuard Server Public Key

```http
GET /api/wireguard/info
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{ "public_key": "ServerPublicKeyBase64==" }
```

---

### 11.3 Add Tunnel (admin only)

```http
POST /api/tunnels
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{
  "name": "node-cabang-barat",
  "ip_address": "10.8.0.3"
}
```

Server akan men-generate keypair WireGuard secara otomatis.

> **Prasyarat:** Service WireGuard (`wg-quick@wg0`) harus dalam status **active**.
> Jika tidak aktif, akan return `400` dengan pesan:
> `"WireGuard service is offline. Silakan nyalakan di menu VPN Configuration terlebih dahulu."`

---

### 11.4 Delete Tunnel (admin only)

```http
DELETE /api/tunnels/:id
Authorization: Bearer <TOKEN>
```

> **Prasyarat:** Service WireGuard (`wg-quick@wg0`) harus dalam status **active**.
> Jika tidak aktif, akan return `400` sebelum menghapus peer dari konfigurasi.

---

## 12. Endpoint: VPN Config

> Role: `admin` only

### 12.1 Get VPN Config

```http
GET /api/vpn/config
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{
  "wireguard_ip": "10.8.0.1/24",
  "wireguard_port": "51820",
  "l2tp_ip": "10.9.0.1",
  "l2tp_secret": "rahasia"
}
```

---

### 12.2 Cek Status VPN Service

```http
GET /api/vpn/service/wireguard/status
GET /api/vpn/service/l2tp/status
Authorization: Bearer <TOKEN>
```

**Response 200:**
```json
{ "active": true }
```

---

### 12.3 Toggle VPN Service (admin only)

```http
POST /api/vpn/service/wireguard/toggle
POST /api/vpn/service/l2tp/toggle
Authorization: Bearer <TOKEN>
Content-Type: application/json
```

**Request Body:**
```json
{ "action": "start" }
```

| `action` | Keterangan |
|---|---|
| `start` | Menghidupkan service — menggunakan `systemctl enable --now` (aktif sekarang + persisten setelah reboot) |
| `stop` | Mematikan service — menggunakan `systemctl disable --now` (mati sekarang + tidak auto-start setelah reboot) |

---

## 13. Quick Reference NOC Workflow

### Monitoring Rutin

Urutan endpoint untuk **monitoring rutin** oleh NOC Agent:

```
1. GET /api/system/stats            Cek kesehatan server (CPU/RAM/Disk)
2. GET /api/sessions                Daftar user yang sedang online
3. GET /api/users                   Daftar semua user PPP + profile + NAS
4. GET /api/nas                     Daftar NAS yang terdaftar
5. GET /api/logs/auth               Log auth 500 terbaru (sukses/gagal)
6. GET /api/logs/accounting         Riwayat sesi 500 terbaru
7. GET /api/traffic/daily/:user     Traffic grafik per user
8. GET /api/profiles                Daftar profile/paket
9. GET /api/fup                     Daftar kebijakan FUP
```

### Alur: Deteksi User Disconnect Berulang

```
1. GET /api/logs/auth
   Filter reply = "Access-Reject" -> temukan username dengan reject tinggi

2. GET /api/users
   Cek profile dan nas_ip user bersangkutan

3. GET /api/traffic/daily/:username
   Cek lonjakan traffic (kemungkinan FUP tercapai)

4. POST /api/sessions/kick   (jika ada ghost session)
   Body: { "username": "targetUser" }
```

### Alur: Isolir Pelanggan Menunggak

```
1. POST /api/users/:username/disable
   Set Auth-Type Reject + backup password

2. GET /api/sessions
   Verifikasi: username tidak boleh muncul di sesi aktif
```

---

## 14. Contoh Penggunaan curl

### Setup Variable

```bash
TOKEN="your_api_token_here"
HOST="http://192.168.1.100:3001"
```

### Get Active Sessions

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$HOST/api/sessions" | jq .
```

### Hitung User Online

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$HOST/api/sessions" | jq length
```

### Get All Users

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$HOST/api/users" | jq .
```

### Cek System Stats (Ringkas)

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$HOST/api/system/stats" | \
  jq '{ cpu: .cpu.load, ram: .memory.usedPercent, disk: .storage.usedPercent, total_users: .radiusData.radcheckCount }'
```

### Disable User (Isolir)

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  "$HOST/api/users/pelanggan01/disable" | jq .
```

### Enable User (Unisolir)

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"profile":"paket-10mbps"}' \
  "$HOST/api/users/pelanggan01/enable" | jq .
```

### Kick Session

```bash
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"username":"pelanggan01"}' \
  "$HOST/api/sessions/kick" | jq .
```

### Auth Log: Filter yang Reject

```bash
curl -s -H "Authorization: Bearer $TOKEN" "$HOST/api/logs/auth" | \
  jq '[.[] | select(.reply == "Access-Reject")]'
```

### Traffic Harian User

```bash
curl -s -H "Authorization: Bearer $TOKEN" \
  "$HOST/api/traffic/daily/pelanggan01" | jq .
```

### Batch Create Users dari File JSON

```bash
# Buat users.json dulu:
# { "users": [{"username":"u1","password":"p1","profile":"paket-10mbps"},...] }
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d @users.json \
  "$HOST/api/users/batch" | jq .
```

---

*Dibuat: 2026-10-01 | Diperbarui: 2026-10-01 — NETORA Radius UI (upluk-upluk_dev)*
