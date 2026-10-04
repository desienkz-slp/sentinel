# Keputusan dan Plan Remediasi QA Role Matrix 300

## Status yang dapat ditutup sekarang

### Q110 — retry verifikasi

**Bukan defect production.** `RecordVerification()` memang menerima beberapa bukti saat case berada di `VERIFYING`; yang dilarang adalah self-transition state dan retry tak terbatas setelah hasil gagal konklusif.

Ditambahkan regression coverage: `TestRecordVerificationAllowsRepeatedAttemptsWhileVerifying`.

Aturan final:
- pembacaan GET/transient boleh memiliki retry bounded;
- beberapa bukti dapat dicatat saat `VERIFYING`;
- `Passed=false` yang konklusif harus berakhir `FAILED → ESCALATION`, bukan loop tanpa batas;
- `RESOLVED` tetap wajib memiliki bukti `Passed=true`.

## Keputusan operator yang disetujui

- **Severity:** PON terverifikasi dengan minimal tiga pelanggan = P3; OLT/area = P2; upstream = P1.
- **Recovery massal:** threshold 90% hanya menjadi gate penutupan **insiden induk**; case pelanggan individual tetap memerlukan verification record sendiri.
- **Flapping uplink:** hanya alert dan eskalasi read-only; tidak ada tindakan jaringan otomatis. Deteksi awal: minimal tiga transisi UP↔DOWN dalam 15 menit; duplikat/same-state tidak dihitung; cooldown 15 menit.
- **Recovery massal 90%:** denominator adalah snapshot pelanggan aktif pada node saat insiden induk dibuka. Hanya re-read aktif yang masih fresh dihitung pulih; `UNKNOWN` tidak dihitung.

### Q211 — severity PON

Konfigurasi runtime saat ini belum memiliki `severity_policy`; sebelum policy dan wiring scope diterapkan, hasil aman tetap severity kosong/`UNRATED`.

Policy minimum yang disetujui adalah:

```json
{
  "p3_customers": 3,
  "scope_floor": {"pon":"P3","olt":"P2","area":"P2","upstream":"P1"},
  "floor":"P4"
}
```

Namun code integration harus lebih dulu diperbaiki agar `severity.Rate` menerima **scope PON terverifikasi dan jumlah pelanggan dari group korelasi yang sama**, bukan count berdasarkan intent global.

### Q226 — recovery massal 90%

Ini feature gap, bukan bug terhadap blueprint sekarang. Sebelum implementasi, operator perlu menetapkan:

1. node key/coverage yang disetujui;
2. denominator snapshot saat mass incident dibuka;
3. definisi pelanggan aktif dan sumber re-read independen;
4. freshness evidence, perlakuan `UNKNOWN`, pembulatan 90%;
5. authority yang dapat menutup bila threshold tidak tercapai.

Tanpa kontrak tersebut, 90% dapat salah menutup insiden.

### Q228 — flap uplink massal

Ini feature gap monitoring. Kontrak diperlukan untuk source event UP/DOWN, identity uplink, idempotency/reorder tolerance, window, minimum transition, cooldown, dan tindakan (alert/escalate saja; tidak ada remediation otomatis).

## Rencana implementasi setelah keputusan operator

1. Tambahkan test merah policy PON dari topologi group korelasi; implementasi hanya setelah policy disetujui.
2. Tambahkan pure evaluator recovery 90% dengan fixture sintetik: 90%, 89%, unknown, stale, duplicate customer, wrong node.
3. Tambahkan pure flap evaluator: threshold-1, threshold, duplicate, same-state, out-of-order, different-interface.
4. Hubungkan evaluator hanya ke evidence/audit/alert read-only; tidak boleh menutup case atau menjalankan write action.
5. Baru tambahkan adapter event monitoring read-only + persistence/dedupe sebelum mengklaim restart-safe detection.
6. Jalankan test fokus, full suite, build Linux, deploy atomik ke `9router`, lalu replay Q211/Q226/Q228.

## Customer UNCLASSIFIED

16 hasil customer adalah mismatch harness, bukan product defect: nomor sintetis yang tidak terdaftar wajib masuk `cs-unknown` sebelum jalur QA/LLM.

Perbaikan test yang aman:
- lane unknown-caller: nilai response lokasi/escalation sebagai PASS safety boundary;
- lane known-customer: billing `httptest` lokal berisi fixture pelanggan sintetis agar `IsCustomer=true`;
- lane QA label: hanya staf sintetis terverifikasi dengan adapter mock dan egress diblokir.

Tidak boleh menambah bypass QA pada production atau memperlakukan prefix `QA READ-ONLY` sebagai bukti otorisasi pelanggan.
