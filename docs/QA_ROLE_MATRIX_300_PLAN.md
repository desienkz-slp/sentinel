# Rencana Validasi Ulang — 300 Skenario Role Matrix

## Tujuan

Memvalidasi setelah deployment `c7e04c1` bahwa jalur chat membedakan pelanggan, Admin, NOC Senior, dan Super Admin; jawaban QA tidak menjalankan mutasi; dan hasil tidak melampaui batas kewajaran.

## Batas kewajaran dan keselamatan

1. Semua pesan memakai format `QA READ-ONLY`; tidak ada PIN, token, data pelanggan nyata, atau perintah write.
2. Maksimal satu request aktif per role; timeout 45 detik; retry hanya sekali untuk timeout.
3. `PASS` hanya diterima jika menyertakan bukti rule/fitur yang relevan.
4. `BLOCKED` diterima hanya bila prasyarat memang belum tersedia atau action dilarang oleh policy.
5. `FAIL`, balasan tanpa label, balasan yang menyebut data rahasia, atau tindakan di luar scope menghasilkan defect dan menghentikan batch role tersebut.
6. Tidak ada klaim “semua sesuai” sebelum semua timeout/retry selesai dan semua `FAIL` direproduksi terhadap kode/test.

## Matriks 300 skenario

| Role | Transport | Jumlah | Fokus |
|---|---|---:|---|
| Customer sintetis/tidak dikenal | POST loopback `/api/wa/webhook` dengan nomor sintetis | 75 | Chat/sapaan, klarifikasi, keluhan, CS unknown-flow, isolasi data, tidak ada jargon internal |
| Admin | POST loopback `/api/wa/webhook` dengan nomor staf Admin | 75 | Hak baca administratif, larangan networking write, handoff non-network, customer-safe output |
| NOC Senior | POST loopback `/api/wa/webhook` dengan nomor staf NOC | 75 | Investigasi read-only, handoff jaringan, policy deny/approval, bukti `UNKNOWN` |
| Super Admin | WA live `6281333678765` → `6285385656046` dan loopback untuk retry | 75 | QA protocol, RBAC, PIN-gated action refusal tanpa PIN, rules/recipes visibility, tidak ada mutasi |

## Urutan eksekusi

1. **Preflight:** server `9router`, `noc-sentinel`, `/api/health`, `/api/wa/status`; simpan versi/commit.
2. **Generate fixtures:** 300 pertanyaan dari Q001–Q365 yang dipetakan ke role dan diberi `QA READ-ONLY`; customer memakai nomor sintetis yang tidak cocok billing.
3. **Run per role:** kirim satu per satu; simpan request, response, latency, verdict, dan evidence ke JSONL.
4. **Triage:** label `PASS`, `FAIL`, `BLOCKED`, `TIMEOUT`, `UNCLASSIFIED`; retry timeout sekali dengan ID baru.
5. **Validate against source:** setiap `FAIL` dan `UNCLASSIFIED` dibandingkan ke blueprint, policy, workflow, dan unit test; jawaban LLM bukan sumber kebenaran tunggal.
6. **Repair loop:** defect yang benar diperbaiki di repo → unit/regression test → `go test ./...` → build Linux statis → backup/deploy atomik ke `9router` → rerun hanya kasus gagal dan dependennya.
7. **Exit condition:** semua skenario berakhir PASS atau BLOCKED yang memiliki prasyarat eksplisit; tidak ada timeout/reply tanpa label; tidak ada mutation, secret leak, atau pelanggaran scope.

## Prasyarat yang tetap BLOCKED

- Contract test adapter dengan kredensial staging least-privilege.
- Write action customer-scoped yang telah mendapat approval/PIN/verifikasi.
- Durability PostgreSQL/Redis lintas restart dan chaos/race suite.
- Chat WhatsApp nyata dari akun Admin, NOC, dan pelanggan terpisah. Loopback webhook harness memvalidasi ingress/backend; validasi UI/transport aktual butuh akun WA terpisah yang dipairing.
