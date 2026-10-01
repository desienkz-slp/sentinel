# NETORA — NOC Agent API Guide

> **Target Pembaca:** NOC Engineer / Automated NOC Agent / Script monitoring jaringan.
> **Base Path:** `/api/v1`
> **Autentikasi:** Laravel Sanctum Bearer Token (sama seperti Mobile App).
> **Sumber kode:** Diinvestigasi langsung dari `routes/api.php` + `MonitoringController`, `RadiusController`, `CustomerController`, `SettingsController`.

---

## Daftar Isi

1. [Auth & Token](#1-auth--token)
2. [Overview Semua Router (NOC Dashboard)](#2-overview-semua-router-noc-dashboard)
3. [Detail Router (PPPoE Secrets + Active Sessions)](#3-detail-router-pppoe-secrets--active-sessions)
4. [PPPoE Secrets Langsung dari MikroTik](#4-pppoe-secrets-langsung-dari-mikrotik)
5. [Koneksi Aktif (Live Sessions)](#5-koneksi-aktif-live-sessions)
6. [Resource Sistem MikroTik](#6-resource-sistem-mikrotik)
7. [Sync PPPoE Secrets ke Database](#7-sync-pppoe-secrets-ke-database)
8. [Manajemen PPPoE User (CRUD)](#8-manajemen-pppoe-user-crud)
9. [Isolir & Unisolir Pelanggan via Router](#9-isolir--unisolir-pelanggan-via-router)
10. [Isolir & Unisolir via Billing (tanpa router)](#10-isolir--unisolir-via-billing-tanpa-router)
11. [FreeRADIUS — Sesi Aktif](#11-freeradius--sesi-aktif)
12. [FreeRADIUS — Manajemen User](#12-freeradius--manajemen-user)
13. [FreeRADIUS — Manajemen Profile](#13-freeradius--manajemen-profile)
14. [Paket Internet (Package)](#14-paket-internet-package)
15. [Status Berlangganan Pelanggan](#15-status-berlangganan-pelanggan)
16. [Pembayaran & Tagihan](#16-pembayaran--tagihan)
17. [Statistik & Dashboard NOC](#17-statistik--dashboard-noc)
18. [Referensi Nilai Enum](#18-referensi-nilai-enum)
19. [Konfigurasi URL API NOC Agent](#19-konfigurasi-url-api-noc-agent)
20. [NOC Agent API Key — Generate & Revoke](#20-noc-agent-api-key--generate--revoke)
21. [NOC Agent — Data Pelanggan (Read Only)](#21-noc-agent--data-pelanggan-read-only)

---

## 1. Auth & Token

### `POST /api/v1/login`

> **Tidak membutuhkan Bearer Token.** Endpoint ini terbuka.

**Request:**
```json
{
  "email": "noc@example.com",
  "password": "password123"
}
```

**Response 200:**
```json
{
  "success": true,
  "data": {
    "token": "1|AbCdEfGh...",
    "user": {
      "id": 5,
      "name": "NOC Agent",
      "email": "noc@example.com",
      "role": { "name": "NOC", "can_access_mobile": true },
      "tenant_id": 1
    }
  }
}
```

> Simpan `token` dan gunakan sebagai `Authorization: Bearer {token}` di semua request berikutnya.
> Token berlaku hingga di-revoke. NOC agent sebaiknya menyimpan token di environment variable.

---

## 2. Overview Semua Router (NOC Dashboard)

### `GET /api/v1/monitoring`

Mengambil status semua router aktif milik tenant, termasuk jumlah user online live dari MikroTik, dan daftar semua PPPoE user dari semua router dalam satu response.

> Endpoint ini melakukan koneksi live ke **setiap** router. Waktu respons bergantung pada jumlah router dan latensi jaringan.

**Response 200:**
```json
{
  "success": true,
  "data": {
    "summary": {
      "total_routers": 3,
      "total_online": 87,
      "total_secrets": 150,
      "total_profiles": 8,
      "total_customers": 145,
      "mapped_customers": 138,
      "healthy_routers": 3,
      "error_routers": 0
    },
    "routers": [
      {
        "id": 1,
        "name": "Router-Utama",
        "ip_address": "192.168.1.1",
        "port": 8728,
        "description": "Kantor Pusat",
        "is_active": true,
        "customers_count": 50,
        "pppoe_secrets_count": 55,
        "pppoe_profiles_count": 4,
        "disabled_secrets_count": 3,
        "mapped_customers": 48,
        "online_count": 42,
        "live_status": "ok",
        "last_sync": "2026-10-01T02:00:00.000000Z"
      }
    ],
    "pppoeUsers": [
      {
        "name": "user01",
        "router_id": 1,
        "router_name": "Router-Utama",
        "profile": "10Mbps",
        "service": "pppoe",
        "disabled": false,
        "is_online": true,
        "ip": "10.0.0.5",
        "caller_id": "ether1",
        "uptime": "2d3h14m",
        "customer_name": "Budi Santoso",
        "last_logout": "2026-09-28 10:00:00"
      }
    ]
  }
}
```

**Field `live_status`:**
| Nilai | Arti |
| :--- | :--- |
| `ok` | Router bisa dihubungi, data live |
| `error` | Router tidak bisa dihubungi (timeout/error koneksi) |

---

## 3. Detail Router (PPPoE Secrets + Active Sessions)

### `GET /api/v1/monitoring/{router}/detail`

Detail lengkap satu router: daftar secret, sesi aktif, profil, dan resource sistem MikroTik.

**Response 200:**
```json
{
  "success": true,
  "data": {
    "router": { "id": 1, "name": "Router-Utama", "ip_address": "192.168.1.1" },
    "enrichedSecrets": [
      {
        "name": "user01",
        "password": "pass123",
        "profile": "10Mbps",
        "service": "pppoe",
        "disabled": false,
        "comment": "",
        "is_online": true,
        "customer_name": "Budi Santoso",
        "customer_id": 42
      }
    ],
    "activeDetails": [
      {
        "name": "user01",
        "service": "pppoe",
        "caller_id": "00:11:22:33:44:55",
        "address": "10.0.0.5",
        "uptime": "2d3h14m",
        "encoding": "MPPE256",
        "customer_name": "Budi Santoso"
      }
    ],
    "profiles": [ { "name": "10Mbps", "rate-limit": "10M/10M" } ],
    "resources": {
      "uptime": "10d2h",
      "cpu-load": "5",
      "free-memory": "128000000",
      "total-memory": "512000000",
      "version": "7.14",
      "board-name": "hAP ax3"
    }
  }
}
```

---

## 4. PPPoE Secrets Langsung dari MikroTik

### `GET /api/v1/monitoring/{router}/secrets`

PPPoE secrets dari MikroTik secara live, diperkaya data customer dari database NETORA.

**Response 200:**
```json
{
  "success": true,
  "data": {
    "secrets": [
      {
        "name": "user01",
        "password": "pass123",
        "profile": "10Mbps",
        "service": "pppoe",
        "disabled": false,
        "is_online": true,
        "customer_name": "Budi Santoso",
        "customer_id": 42,
        "customer_status": "active",
        "customer_isolated": false,
        "uptime": "2d3h14m",
        "last_logout": "2026-09-28 10:00:00"
      }
    ],
    "profiles": [
      { "name": "10Mbps", "rate-limit": "10M/10M", "local-address": "192.168.10.1" }
    ]
  }
}
```

> Field `customer_status` dan `customer_isolated` berguna untuk mendeteksi user yang seharusnya terisolir tapi masih online di router (inkonsistensi state).

---

## 5. Koneksi Aktif (Live Sessions)

### `GET /api/v1/monitoring/{router}/connections`

Daftar PPPoE active connections saat ini dari MikroTik (tanpa data customer NETORA).

**Response 200:**
```json
{
  "success": true,
  "count": 42,
  "data": [
    {
      "name": "user01",
      "service": "pppoe",
      "caller-id": "00:11:22:33:44:55",
      "address": "10.0.0.5",
      "uptime": "2d3h14m",
      "encoding": "MPPE256"
    }
  ]
}
```

---

## 6. Resource Sistem MikroTik

### `GET /api/v1/monitoring/{router}/resources`

CPU, RAM, uptime, versi firmware MikroTik.

**Response 200:**
```json
{
  "success": true,
  "data": {
    "uptime": "10d2h30m",
    "cpu-load": "12",
    "free-memory": "128000000",
    "total-memory": "512000000",
    "free-hdd-space": "52000000",
    "total-hdd-space": "128000000",
    "version": "7.14 (stable)",
    "board-name": "hAP ax3",
    "architecture-name": "arm64"
  }
}
```

---

## 7. Sync PPPoE Secrets ke Database

### `POST /api/v1/monitoring/{router}/sync`

Sinkronkan PPPoE secrets & profiles dari MikroTik ke database NETORA.

**Body:** *(kosong)*

**Response 200:**
```json
{
  "success": true,
  "message": "Sync selesai: 4 profile, 55 secret."
}
```

---

## 8. Manajemen PPPoE User (CRUD)

### `POST /api/v1/monitoring/{router}/secrets` — Tambah User

```json
{
  "username": "user-baru",
  "password": "passw0rd",
  "profile": "10Mbps"
}
```

### `PUT /api/v1/monitoring/{router}/secrets/profile` — Ganti Profile Batch

```json
{
  "users": ["user01", "user02"],
  "profile": "20Mbps"
}
```

### `DELETE /api/v1/monitoring/{router}/secrets` — Hapus User

```json
{ "username": "user-lama" }
```

---

## 9. Isolir & Unisolir Pelanggan via Router

Disable/enable PPPoE secret di MikroTik **dan** update `is_isolated` di database NETORA. Terekam di `IsolirLog` + `AuditLog`.

### `POST /api/v1/monitoring/{router}/customers/{customer}/isolir`

| Parameter | Tipe | Keterangan |
| :--- | :--- | :--- |
| `router` | integer | ID router MikroTik |
| `customer` | integer | ID pelanggan NETORA |

**Response 200:**
```json
{ "success": true, "message": "Budi Santoso berhasil di-isolir." }
```

### `POST /api/v1/monitoring/{router}/customers/{customer}/unisolir`

```json
{ "success": true, "message": "Budi Santoso berhasil di-unisolir." }
```

---

## 10. Isolir & Unisolir via Billing (tanpa router)

Endpoint alternatif via `IsolirController` — tidak perlu spesifik router ID.

| Method | Endpoint | Keterangan |
| :--- | :--- | :--- |
| `POST` | `/api/v1/isolir/{customer}` | Isolir pelanggan |
| `POST` | `/api/v1/isolir/{customer}/release` | Lepas isolir |
| `POST` | `/api/v1/cuti/{customer}/store` | Aktifkan cuti |
| `POST` | `/api/v1/cuti/{customer}/restore` | Restore dari cuti |

---

## 11. FreeRADIUS — Sesi Aktif

### `GET /api/v1/radius/servers/{server}/sessions`

Sesi aktif dari FreeRADIUS (via `radacct`).

```json
{
  "success": true,
  "data": [
    {
      "username": "user01",
      "nasipaddress": "192.168.1.1",
      "framedipaddress": "10.0.1.5",
      "acctstarttime": "2026-10-01 07:00:00",
      "acctsessiontime": 3600,
      "acctinputoctets": 102400,
      "acctoutputoctets": 512000
    }
  ]
}
```

---

## 12. FreeRADIUS — Manajemen User

| Method | Endpoint | Body |
| :--- | :--- | :--- |
| `GET` | `/radius/servers/{server}/users` | — |
| `POST` | `/radius/servers/{server}/users` | `{username, password, profile, nas_ip}` |
| `PUT` | `/radius/servers/{server}/users/{username}` | `{password?, profile?}` |
| `POST` | `/radius/servers/{server}/users/{username}/disable` | — |
| `POST` | `/radius/servers/{server}/users/{username}/enable` | — |
| `POST` | `/radius/servers/{server}/users/batch-delete` | `{usernames: []}` |

---

## 13. FreeRADIUS — Manajemen Profile

| Method | Endpoint | Body |
| :--- | :--- | :--- |
| `GET` | `/radius/servers/{server}/profiles` | — |
| `POST` | `/radius/servers/{server}/profiles` | `{name, rate_limit}` |
| `PUT` | `/radius/servers/{server}/profiles/{id}` | `{name?, rate_limit?}` |
| `DELETE` | `/radius/servers/{server}/profiles/{id}` | — |

---

## 14. Paket Internet (Package)

### `GET /api/v1/settings/packages`

```json
{
  "success": true,
  "data": [
    {
      "id": 1,
      "name": "Paket 10Mbps",
      "price": 150000,
      "speed_up": 10,
      "speed_down": 10,
      "speed_unit": "Mbps",
      "sort_order": 1,
      "is_active": true,
      "pppoe_profile": "10Mbps",
      "radius_profile": "10Mbps"
    }
  ]
}
```

| Field | Keterangan NOC |
| :--- | :--- |
| `pppoe_profile` | Nama profile PPPoE di MikroTik |
| `radius_profile` | Nama group di FreeRADIUS |

### `GET /api/v1/settings/routers`

Daftar router aktif — gunakan untuk mendapatkan `{router}` ID.

### `GET /api/v1/settings/servers`

Daftar server RADIUS aktif — gunakan untuk mendapatkan `{server}` ID.

---

## 15. Status Berlangganan Pelanggan

### `GET /api/v1/customers?status={status}`

| `status` | Keterangan |
| :--- | :--- |
| `active` | Aktif berlangganan (default) |
| `inactive` | Tidak aktif |
| `pending` | Menunggu aktivasi |

**Field status kritis di response:**
```json
{
  "id": 42,
  "name": "Budi Santoso",
  "username": "user01",
  "status": "active",
  "is_isolated": false,
  "isolated_since": null,
  "due_date": "2026-10-25",
  "router_id": 1,
  "package": { "pppoe_profile": "10Mbps" }
}
```

**Penggunaan NOC:**
```
GET /api/v1/customers?status=active&per_page=all   → semua pelanggan aktif
GET /api/v1/customers?status=inactive              → pelanggan tidak aktif
GET /api/v1/customers/bill                          → pelanggan jatuh tempo
```

---

## 16. Pembayaran & Tagihan

### `GET /api/v1/incomes` — Riwayat Pembayaran

Query: `month`, `year`, `search`, `per_page`

### `POST /api/v1/payments` — Catat Pembayaran

```json
{
  "customer_id": 42,
  "amount": 150000,
  "payment_month": 10,
  "payment_year": 2026,
  "payment_method": "cash",
  "notes": "Bayar langsung kantor"
}
```

**Response:**
```json
{ "success": true, "data": { "payment_id": 101, "message": "Pembayaran berhasil dicatat." } }
```

### `GET /api/v1/payments/{payment}/print-data` — Data Struk

### `POST /api/v1/payments/{payment}/cancel` — Batalkan Pembayaran *(butuh `can_delete_finance`)*

---

## 17. Statistik & Dashboard NOC

### `GET /api/v1/reports/total`

```json
{
  "success": true,
  "data": {
    "total_income_this_month": 45000000,
    "total_customers": 150,
    "active_customers": 138,
    "isolated_customers": 12,
    "unpaid_customers": 25
  }
}
```

### `GET /api/v1/statistics/data?action={action}`

| `action` | Keterangan |
| :--- | :--- |
| `monthly_income` | Pendapatan 12 bulan terakhir |
| `customer_growth` | Pertumbuhan pelanggan baru |
| `payment_method` | Distribusi metode pembayaran |
| `package_distribution` | Distribusi paket |
| `area_distribution` | Distribusi per area |
| `isolir_stats` | Statistik isolir per bulan |

---

## 18. Referensi Nilai Enum

| Field | Nilai | Keterangan |
| :--- | :--- | :--- |
| `customer.status` | `active` | Aktif berlangganan |
| `customer.status` | `inactive` | Tidak aktif |
| `customer.status` | `pending` | Pending aktivasi |
| `customer.is_isolated` | `false` | Normal, PPPoE enabled |
| `customer.is_isolated` | `true` | Terisolir, PPPoE disabled |
| `router.live_status` | `ok` | Koneksi router berhasil |
| `router.live_status` | `error` | Gagal konek router |
| `payment_method` | `cash` | Tunai |
| `payment_method` | `transfer` | Transfer bank |
| `payment_method` | `qris` | QRIS |
| `server.type` | `freeradius` | FreeRADIUS standar |
| `server.type` | `upluk_upluk_api` | NETORA Radius API |

---

## Quick Reference — NOC Agent Workflow

```
1. LOGIN
   POST /api/v1/login  →  simpan token

2. AMBIL REFERENSI ID
   GET /api/v1/settings/routers   →  catat router IDs
   GET /api/v1/settings/servers   →  catat RADIUS server IDs
   GET /api/v1/settings/packages  →  catat package & profile names

3. NOC OVERVIEW (polling periodik)
   GET /api/v1/monitoring  →  summary + semua pppoe users semua router

4. MONITORING DETAIL SATU ROUTER
   GET /api/v1/monitoring/{router}/connections  →  live sessions
   GET /api/v1/monitoring/{router}/resources    →  CPU/RAM
   GET /api/v1/monitoring/{router}/secrets      →  secrets + customer status

5. DETEKSI INKONSISTENSI STATE
   Cari: is_online=true AND customer_isolated=true
   → user masih online padahal harusnya terisolir
   → Tindakan: POST /api/v1/monitoring/{router}/customers/{customer}/isolir

6. JATUH TEMPO & ISOLIR OTOMATIS
   GET /api/v1/customers/bill
   POST /api/v1/isolir/{customer}

7. PELUNASAN & UNISOLIR
   POST /api/v1/payments
   POST /api/v1/isolir/{customer}/release

8. SYNC SETELAH PERUBAHAN MANUAL DI MIKROTIK
   POST /api/v1/monitoring/{router}/sync
```

---

## 19. Konfigurasi URL API NOC Agent

Endpoint untuk menyimpan dan mengambil URL base dari server NOC Agent (misalnya: `http://192.168.1.10:3000`). Konfigurasi ini disimpan per-tenant di tabel `billing_configs` dengan key `api_agent_noc`.

---

### `GET /api/v1/configs/api_agent_noc`

Mengambil URL API NOC Agent yang tersimpan untuk tenant saat ini.

**Response 200:**
```json
{
  "status": "success",
  "data": {
    "api_url": "http://192.168.1.10:3000"
  }
}
```
> Jika belum pernah dikonfigurasi, `api_url` akan bernilai `null`.

---

### `POST /api/v1/configs/api_agent_noc`

Menyimpan atau memperbarui URL API NOC Agent. Membutuhkan permission `can_view_dashboard_config` atau `is_system_admin`.

**Request Body:**
```json
{
  "api_url": "http://192.168.1.10:3000"
}
```

| Field     | Tipe   | Wajib | Keterangan                        |
|-----------|--------|-------|-----------------------------------|
| `api_url` | string | Tidak | URL valid (format URL), maks 500 karakter. Kirim `null` atau string kosong untuk mengosongkan. |

**Response 200 (sukses):**
```json
{
  "status": "success",
  "message": "URL API NOC Agent berhasil diperbarui."
}
```

**Response 403 (tidak ada akses):**
```json
{
  "status": "error",
  "message": "Unauthorized. Anda tidak memiliki akses."
}
```

---

## 20. NOC Agent API Key — Generate & Revoke

API key digunakan oleh NOC Agent eksternal untuk mengakses endpoint `/api/noc/v1/*`.
Setiap tenant memiliki key unik. Key dikirim via header `X-NOC-API-Key`.

> **Semua endpoint di bawah** menggunakan autentikasi Sanctum Bearer Token (akun user biasa).
> Butuh permission: `can_view_dashboard_config` atau `is_system_admin`.

---

### `GET /api/v1/configs/noc_agent_api_key`

Mengecek apakah API key sudah ada. Key ditampilkan dalam format masked.

**Response 200:**
```json
{
  "status": "success",
  "data": {
    "has_key": true,
    "key_preview": "••••••••a1b2c3d4"
  }
}
```

---

### `POST /api/v1/configs/noc_agent_api_key/generate`

Generate API key baru (64-char hex). Jika sudah ada key lama, key lama langsung diganti.

> ⚠️ **Simpan key ini segera!** Key hanya ditampilkan sekali penuh saat generate.

**Response 200:**
```json
{
  "status": "success",
  "message": "API key NOC Agent berhasil di-generate. Simpan key ini — tidak akan ditampilkan ulang secara penuh.",
  "data": {
    "api_key": "a3f2e1...c9d8b7"
  }
}
```

---

### `DELETE /api/v1/configs/noc_agent_api_key`

Cabut (revoke) API key. Setelah ini NOC Agent tidak bisa mengakses data sampai key baru di-generate.

**Response 200:**
```json
{
  "status": "success",
  "message": "API key NOC Agent berhasil dicabut. Akses NOC Agent langsung dinonaktifkan."
}
```

---

## 21. NOC Agent — Data Pelanggan (Read Only)

> **Base Path berbeda:** `/api/noc/v1` (bukan `/api/v1`)
> **Autentikasi:** Header `X-NOC-API-Key: <key>` — **TANPA** Bearer Token Sanctum.
> Data difilter otomatis per-tenant berdasarkan API key yang digunakan.

### `GET /api/noc/v1/customers`

List semua pelanggan milik tenant dengan relasi lengkap.

**Query Parameters:**

| Parameter    | Tipe    | Keterangan |
|--------------|---------|------------|
| `status`     | string  | `active`, `inactive`, dll. Default: semua |
| `is_isolated`| bool    | `0` atau `1` |
| `router_id`  | integer | Filter by router |
| `area_id`    | integer | Filter by area |
| `search`     | string  | Cari by nama / username PPPoE / phone |
| `per_page`   | integer | Default: 50, maksimum: 200 |
| `page`       | integer | Halaman (default: 1) |

**Contoh Request:**
```bash
curl -H "X-NOC-API-Key: a3f2e1...c9d8b7" \
  "http://your-server/api/noc/v1/customers?status=active&is_isolated=0&per_page=100"
```

**Response 200:**
```json
{
  "status": "success",
  "data": [
    {
      "id": 12,
      "uuid": "550e8400-e29b-41d4-a716-446655440000",
      "customer_id_display": "PLG-0012",
      "name": "Budi Santoso",
      "phone": "08123456789",
      "email": "budi@email.com",
      "address": "Jl. Merdeka No. 5, Bandung",
      "nik": "3273012345678901",
      "username": "budi.pppoe",
      "odp_port": "1",
      "registration_date": "2024-01-15",
      "billing_date": 1,
      "tgl_isolir": 10,
      "custom_price": null,
      "diskon": null,
      "jenis_bayar": "prabayar",
      "max_tunggakan": 1,
      "auto_isolir": true,
      "pakai_ppn": false,
      "pakai_bhp": false,
      "auto_wa_tagihan": true,
      "tambahan_layanan": null,
      "deskripsi_layanan": null,
      "status": "active",
      "is_isolated": false,
      "isolated_since": null,
      "is_on_leave": false,
      "leave_start": null,
      "leave_end": null,
      "notes": null,
      "area":    { "id": 2, "name": "Kec. Cimahi" },
      "package": { "id": 5, "name": "Paket 10Mbps", "price": 150000 },
      "router":  { "id": 1, "name": "MKT-Utama", "host": "192.168.1.1" },
      "radius":  { "id": 1, "name": "RADIUS-01" },
      "odp":     { "id": 3, "name": "ODP-A01" },
      "sales":   { "id": 7, "name": "Andi" },
      "coordinate": { "latitude": -6.914744, "longitude": 107.603012 },
      "created_at": "2024-01-15 08:00:00",
      "updated_at": "2026-09-01 10:30:00"
    }
  ],
  "meta": {
    "current_page": 1,
    "per_page": 50,
    "total": 243,
    "last_page": 5
  }
}
```

**Response 401 (key salah/tidak ada):**
```json
{
  "status": "error",
  "message": "Header X-NOC-API-Key wajib disertakan."
}
```

---

*Dibuat: 2026-10-01 | Diperbarui: 2026-10-01 | Investigasi: `routes/api.php`, `MonitoringController`, `RadiusController`, `CustomerController`, `SettingsController`, `ConfigApiController`, `NocAgentController`*
