# Frontend Documentation & Handoff untuk Redesign Ulang

Dokumen ini dibuat agar bisa langsung diberikan ke model/engine AI lain untuk menyusun **plan redesign frontend** NOC Sentinel tanpa merusak fungsi operasional.

> **Snapshot Repository**
> - Branch: `main`
> - Commit acuan: `4e97811`
> - Frontend stack: **Server-rendered static HTML + Vanilla JS + Shared CSS**
> - Halaman utama: `web/index.html`, `web/settings.html`, `web/login.html`
> - Shared styles: `web/noc-ui.css`

---

## 1. Konteks Produk & Fungsi Dashboard

NOC Sentinel adalah dashboard operasional NOC (Network Operations Center) untuk ISP:
- **Observability:** Health status sistem (LLM, WA Gateway, PostgreSQL, Redis, Mikrotik, Radius, Billing, GenieACS).
- **Diagnosis Cepat:** Investigasi keluhan pelanggan lewat AI agent / rule deterministic.
- **Monitoring Case Aktif:** Pelacakan lifecycle tiket gangguan (`NEW` → `VERIFYING` → `OBSERVATION` → `FIELD_VISIT` → `RESOLVED_PENDING_CONFIRMATION` → `RESOLVED_VERIFIED` → `FAILED` → `ESCALATION`).
- **Monitoring Insiden Massal & Alert:** Visualisasi mass outage dan flapping uplink (MikroTik poll-derived).
- **Settings & Rule Engine:** Manajemen SOP deterministic, direktori staf + RBAC/PIN, konfigurasi multi-router MikroTik, model LLM & Codex bridge.
- **WhatsApp Gateway:** Kontrol daemon Baileys, pairing QR, trigger webhook, pengiriman pesan manual.

Frontend saat ini **langsung terhubung ke endpoint internal** (`/api/...`) pada origin yang sama tanpa framework (SPA/bundler/npm).

---

## 2. Arsitektur Frontend Saat Ini

### 2.1 Teknologi
- HTML statis + inline script (Vanilla JavaScript ES6).
- CSS terpusat pada `web/noc-ui.css` dengan CSS Custom Properties (Theme tokens).
- Polling asynchronous menggunakan native `fetch()` dan `setInterval()`.
- Tidak ada dependency Node.js/npm untuk frontend (semua disajikan langsung oleh binary Go).

### 2.2 Struktur File
- `web/index.html` → Dashboard operasi cockpit real-time (case queue, diagnosis, alerts, observability, drawer WA admin).
- `web/settings.html` → Konfigurasi sistem, model AI, policy, rules SOP, direktori staf, integrasi eksternal, developer mode greeting.
- `web/login.html` → Form login HMAC-SHA256 authenticated dashboard.
- `web/noc-ui.css` → Shared stylesheet untuk tema gelap (dark dense cockpit), layout responsif, dan aksesibilitas.

---

## 3. Kontrak Fungsional yang WAJIB Dipertahankan

Bila merombak desain (HTML/CSS), aturan ini **TIDAK BOLEH DILANGGAR**:

1. **Endpoint API yang sudah dipakai JS** harus tetap dipanggil secara tepat dengan method dan payload yang sama.
2. **ID DOM yang diakses JS** (`document.getElementById(...)` atau `$(...)`) harus tetap ada atau dimigrasikan secara presisi.
3. **Aksi operasional existing tidak boleh hilang:** tombol diagnosis, case viewer, WA control, simpan settings, aturan SOP, direktori staf.
4. **DILARANG menambah aksi fiktif** yang belum didukung backend (misal: tombol "Acknowledge" atau "Manual Close" tanpa API backend).
5. **Keamanan & Guardrails:** Kredensial password/token eksternal hanya ditampilkan sebagai `••••` (masked); tidak ada secret yang di-render di DOM.
6. **Greeting First Message Mode (Developer Mode):** Fitur toggle ON/OFF dan custom template sapaan pelanggan harus tetap ada di Settings dan berfungsi.

---

## 4. Inventory Halaman, DOM ID, dan API Endpoint

### 4.1 Dashboard (`web/index.html`)

#### Endpoint API yang Dipanggil:
- `GET /api/health` — Status konektivitas dependency (LLM, WA, DB, Cache).
- `GET /api/cases/active` — Daftar case gangguan yang sedang aktif.
- `GET /api/incidents/mass` — Proyeksi status insiden massal.
- `GET /api/alerts` — Alert monitoring (termasuk deteksi flap uplink).
- `GET /api/observability/summary` — Metrik total pesan, intent, eskalasi, dan case.
- `GET /api/logs?limit=80` — Riwayat sesi & kejadian insiden.
- `POST /api/wa/run` — Eksekusi diagnosis AI / SOP manual.
- Endpoint WA Gateway: `/api/wa/status`, `/api/wa/check`, `/api/wa/qr`, `/api/wa/connect`, `/api/wa/reconnect`, `/api/wa/logout`, `/api/wa/restart-daemon`, `/api/wa/send`, `/api/wa/settings`, `/api/wa/history`.

#### Polling Interval:
- Health check: 20 detik
- Observability summary: 12 detik
- Active cases: 8 detik
- Mass incidents: 12 detik
- Alerts: 10 detik
- Logs/history: 10 detik

#### Fungsi JavaScript Kunci:
- `checkHealth()`, `refreshKpis(obs, cases)`, `refreshCases()`, `refreshAlerts()`, `refreshIncidents()`, `refreshObservability()`, `refreshLogs()`
- `runDiag()` — Handler submit form diagnosis cepat.
- `openAdminDrawer()` — Membuka modal administrasi integrasi WA.
- `saveWaSettings()` — Menyimpan parameter konfigurasi WA lokal.

#### Daftar DOM ID Penting:
`activeCaseCount`, `adminDrawer`, `alertCount`, `caseTableWrap`, `caseTbody`, `elapsedMs`, `healthBadge`, `kpiActive`, `kpiEsc`, `kpiHealth`, `kpiResolved`, `nowText`, `obsCases`, `obsEscalations`, `obsIntl`, `obsMass`, `obsTotal`, `phaseLegend`, `qrAge`, `setMsg`, `sessState`, `steps`, `stCfgReply`, `stCfgSender`, `stCfgTimeout`, `stConnected`, `stHealth`, `stLastErr`, `stMsgState`, `stPid`, `stProvider`, `stReason`, `stSender`, `stStore`, `stWAEnabled`, `stWAAuto`, `stWATimeout`, `statusLine`, `tblAlerts`, `tblIncidents`, `tbodyAlerts`, `tbodyIncidents`, `txt`, `waQRBox`, `waQrModal`, `waSetAutoReply`, `waSetSender`, `waSetTimeout`, `whSender`, `whText`

---

### 4.2 Pengaturan (`web/settings.html`)

#### Endpoint API yang Dipanggil:
- `GET /api/config` & `POST /api/config` — Pengambilan dan penyimpanan konfigurasi global aplikasi.
- `GET /api/models` — Daftar model AI yang tersedia dari endpoint LLM.
- `GET /api/rules/list`, `POST /api/rules/upsert`, `POST /api/rules/order` — Manajemen SOP deterministic.
- `GET /api/staff/list`, `POST /api/staff/list` — Direktori staf internal & otorisasi hak akses.

#### Fungsi JavaScript Kunci:
- `loadAll()` — Memuat seluruh data konfigurasi saat startup.
- `saveSettings()` — Mengirim payload pembaruan ke `/api/config`.
- Rule handlers: `addRule()`, `delRule()`, `upRule()`, `saveRuleOrder()`.
- Staff directory handlers: `addStaffMember()`, `delStaffMember()`, `verifyPin()`, `setPinMode()`.
- Dynamic list handlers: `addMk()`, `delMk()`, `addNocSenior()`, `delNocSenior()`, `addAllowedTool()`, `delAllowedTool()`, `addForceNoProbeIntent()`, `delForceNoProbeIntent()`.

#### Fitur Developer Mode: Greeting First Message
- Toggle: `id="greetingFirstMessage"` (Checkbox boolean)
- Template Editor: `id="greetingTemplate"` (Textarea string)
- Config JSON fields: `"greeting_first_message"` (bool) & `"greeting_template"` (string)
- Placeholder aman yang didukung: `{{name}}`, `{{org}}`, `{{role}}`.

#### Daftar DOM ID Penting:
`apiHint`, `apiHintBox`, `backupCurrentNode`, `backupOpen`, `backupParentId`, `backupReason`, `billingKey`, `billingUrl`, `codexApi`, `codexHint`, `codexProvider`, `codexProviderHint`, `forceNoProbeIntents`, `gApiHint`, `gApiHintBox`, `gProvHint`, `gProvHintBox`, `genieKey`, `genieUrl`, `greetingFirstMessage`, `greetingTemplate`, `mkCount`, `mkList`, `nocSeniorList`, `orgName`, `radiusKey`, `radiusUrl`, `recipeAlias`, `recipeDiagIntents`, `rulesBody`, `sBase`, `sCodex`, `sCodexBase`, `sGate`, `sMaxCost`, `sMaxCostMinConf`, `sMinCostConf`, `sModel`, `sModelHint`, `sRulesOff`, `sSteps`, `sSys`, `sTemp`, `sTimeout`, `sTools`, `sWire`, `setMsg`, `staffApiHint`, `staffApiHintBox`, `staffDirectoryBody`, `staffPin`, `staffPinMode`, `staffPinRow`, `staffRole`, `staffRoleHint`, `staffSearch`, `staffStatus`, `staffTitle`, `staffName`, `staffNumber`, `stRules`, `stRulesStatus`, `toolAllowlist`

---

### 4.3 Autentikasi (`web/login.html`)
- Form POST langsung ke handler native `/login`.
- Input elements: `id="user"` (`name="username"`), `id="pass"` (`name="password"`), `id="submitBtn"`.
- Feedback elements: `id="err"`, `id="ver"`.

---

## 5. Baseline Responsive & Accessibility

Di stylesheet bersama (`web/noc-ui.css`), standar berikut wajib dipertahankan:
- **Breakpoint multi-device:**
  - `Full Desktop`: `min-width: 1440px` (layout 3 zona padat / wide cockpit).
  - `Portrait Desktop / Tablet`: `768px - 1439px` (reflow 2 kolom / 1 kolom tanpa nested scroll trap).
  - `Mobile`: `< 768px` (single column, touch target minimum 44px, horizontal table wrapper).
- **Viewport Constraints:**
  - Komponen tabel aktif (seperti Active Cases) wajib dibatasi ketinggiannya (`max-height: min(460px, 52vh)`) dengan scroll vertikal mandiri (`overflow: auto`) agar tidak mendorong konten lain ke bawah secara tak terbatas.
- **Accessibility:**
  - Focus state jelas (`:focus-visible`).
  - Dukungan `@media (prefers-reduced-motion: reduce)`.

---

## 6. Prompt Siap Pakai untuk Diarahkan ke Model Lain (Copy)

Gunakan prompt di bawah ini ketika meminta model AI lain membuat plan redesain:

```text
Halo, saya ingin Anda menyusun PLAN redesain frontend untuk aplikasi NOC Sentinel (bukan langsung menulis kode utuh, melainkan rencana teknis dan arsitektur visual).

Konteks Sistem:
1. Stack: HTML statis murni + Vanilla JavaScript (ES6) + Shared CSS (noc-ui.css) yang di-embed langsung ke dalam binary Go. Tidak menggunakan React, Vue, Tailwind CLI, atau npm bundler.
2. Tiga halaman utama:
   - web/index.html (Dashboard operasi cockpit NOC)
   - web/settings.html (Pengaturan model, policy, rules SOP, direktori staf, integrasi eksternal, greeting dev mode)
   - web/login.html (Halaman masuk operator)
3. Kompatibilitas Wajib:
   - Seluruh DOM ID yang ada saat ini harus tetap dipertahankan atau dipetakan 100% tanpa merusak script JS dan polling interval fetch().
   - Tidak boleh mengarang action/tombol baru yang belum ada endpoint backend-nya.
   - Fitur "Greeting First Message (Mode Developer)" dengan toggle dan template custom di Settings wajib tetap dipertahankan.
   - Multi-device support: Full Desktop (>=1440px), Portrait Desktop (768-1439px), dan Mobile (<768px, touch target >=44px).

Tugas Anda:
Buatlah Plan Redesain Frontend yang komprehensif, mencakup:
1. Analisis masalah visual & hierarki informasi pada UI saat ini.
2. Usulan Information Architecture (IA) dan Wireframe Layout baru untuk Desktop, Portrait, dan Mobile.
3. Desain Komponen Cockpit Operasi:
   - Case Active Queue & Timeline Lifecycle chip.
   - Quick Diagnosis & AI telemetry panel.
   - Observability & Mass Incident alert banners.
   - Drawer Administrasi WhatsApp & status integrasi.
4. Rencana refactoring CSS (token warna, typography, layout grid/flexbox, accessibility).
5. Strategi migrasi bertahap (Zero-regression implementation plan) agar semua hook JS dan ID DOM tetap aman.
6. Acceptance & QA checklist untuk pengujian visual dan fungsional.
```

---

## 7. Validasi Pengujian Setelah Redesain

Sebelum perubahan diterapkan ke lingkungan produksi, pengujian wajib:
```bash
# 1. Pastikan syntax dan integrasi Go tetap valid
go test ./... -count=1
go vet ./...

# 2. Cek integritas penyajian file statis
go test -run 'TestWebContainsCoreIDsAndApiPaths|TestSettingsContainsCoreFieldsAndPaths'
```
Pemeriksaan visual manual wajib memvalidasi alur diagnosis, buka drawer WA, simpan pengaturan, dan layout pada browser desktop, portrait, dan mobile.
