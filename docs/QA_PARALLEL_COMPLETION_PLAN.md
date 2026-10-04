# Plan Paralel Penyelesaian QA NOC Sentinel

Target produksi: `root@172.18.20.137` melalui SSH alias `9router`.

## Kondisi saat dibuat

- Batch role matrix 300 sedang berjalan pada server: `/tmp/noc_role_matrix_remote.py`.
- Artefak hasil remote: `/opt/noc-sentinel/data/qa-role-matrix-225.jsonl`.
- Yang belum terminal: Q216–Q225 (NOC Senior), retry Q155/Q183/Q212, lalu Q226–Q300 (Super Admin live WhatsApp).
- Jangan pernah menjalankan runner pengganti sebelum proses aktif dan lock/writer diverifikasi.

## Stream paralel

| Stream | Scope | Transport | Owner | Stop condition |
|---|---|---|---|---|
| A — Resume NOC Senior | Q216–Q225 + retry Q155/Q183/Q212 | webhook loopback `POST /api/wa/webhook` | Runtime/QA | proses aktif macet, error berulang, atau evidence tidak dapat dikorelasikan |
| B — Super Admin live | Q226–Q300 | WA `6281333678765` → `6285385656046` | QA | recipient/session salah, mutasi tak terduga, auth failure, atau rate-limit berulang |
| C — Customer role | 75 skenario sintetis | webhook loopback | QA/CS | data nyata, staff-only control, atau external write |
| D — Admin role | 75 skenario | webhook loopback, sender `6285606311311` | QA/RBAC | admin menerima Super Admin/write capability |
| E — NOC Senior role | 75 skenario | webhook loopback, sender `6281555312020` | QA/RBAC | network mutation berjalan tanpa approval/verification |
| F — Triage/repair | semua hasil non-PASS | source, unit test, audit, gateway readback | Application/Safety | P0/P1 atau classification tidak dapat dibuktikan |

Stream C–E memakai payload sintetis dan tidak boleh memakai data pelanggan. Live WhatsApp hanya untuk Super Admin karena hanya akun tersebut yang dipairing.

## Protokol runner tahan putus SSH

1. Periksa PID, command line, elapsed time, output terbaru, dan jumlah row hasil sebelum melakukan apa pun.
2. Gunakan satu writer dengan lock atomik, PID file, run manifest, dan log persisten (`systemd-run`, `tmux`, atau `nohup`).
3. Setiap row JSONL append-only memuat: `run_id`, commit, role, question ID, attempt, correlation ID, input, expected rule, timestamps, response/error, dan primary outcome.
4. Resume dihitung dari `requested_ids - accepted_success_ids`; jangan menghapus atau menimpa attempt lama.
5. Retry hanya error transient (timeout/rate-limit/transport), maksimal tiga attempt total, backoff eksponensial + jitter. Error deterministik langsung ditriage.

## Model hasil kanonik

Setiap pertanyaan hanya memiliki satu outcome primer:

- `PASS`: role/rule/workflow tepat dan bukti cukup.
- `FAIL`: terbukti ada pelanggaran behavior/policy/workflow/security/evidence.
- `BLOCKED`: prasyarat eksplisit tidak tersedia atau aksi sengaja ditolak policy; nama owner dan unblock condition wajib ada.
- `TIMEOUT`: tidak mendapat hasil terminal pada deadline; stage kegagalan wajib tercatat.
- `UNCLASSIFIED`: bukti kurang/kontradiktif/malformed; ini defect kualitas data.

Jangan mengubah hasil safety denial yang diharapkan menjadi FAIL. Sebaliknya, jawaban yang terdengar benar tanpa evidence wajib FAIL atau UNCLASSIFIED, bukan PASS.

## Urutan eksekusi

1. **Preflight**: `/api/health`, `/api/wa/status`, versi binary, commit/deploy digest, registry enabled snapshot, policy mode, dan konfigurasi feature flag.
2. **Selesaikan Stream A**: tunggu proses aktif jika ada progres; jika macet, capture log lalu resume pending set saja.
3. **Jalankan Streams C–E**: satu request aktif per role, synthetic correlation ID, tidak ada tool write atau data nyata.
4. **Jalankan Stream B**: small batch Q226–Q300, verifikasi `last_incoming` dan `last_outgoing` sesuai ID, stop otomatis pada anomali.
5. **Agregasi**: hasil kanonik harus mencakup tepat Q001–Q300, satu accepted result per ID, tanpa overlap label.
6. **Triage**: setiap non-PASS diberi defect/duplicate/prerequisite owner.
7. **Repair**: regression test merah → patch deterministic boundary → focused test + full `go test ./...` → deploy atomik → readback target 137.
8. **Retest**: ulangi kasus gagal, test negatif/adjacent-role/dedupe/restart; rerun matrix penuh pada release candidate.

## Acceptance per role

### Customer sintetis

- Greeting/UNCLEAR/billing/keluhan diroute customer-safe.
- Tidak bisa melihat staff control, data pelanggan lain, topology, token, atau audit mentah.
- Payload duplikat/out-of-order aman dan tidak menghasilkan aksi ganda.

### Admin

- Sender dipetakan ke `admin`; hanya capability administratif/read yang diizinkan.
- Tidak dapat role management, Super Admin action, atau network write.
- Klaim role di teks tidak boleh mengalahkan identitas sender.

### NOC Senior

- Sender dipetakan ke `noc_senior`; read-only troubleshooting dan handoff network benar.
- Network/write/high-risk action tetap approval-gated/dry-run atau denied.
- Tidak ada leakage data pelanggan/topology/kredensial produksi.

### Super Admin live WA

- WA dua arah, sender recognition, correlation, dedupe, dan reply recipient benar.
- High-impact request tidak memicu mutasi tanpa workflow/PIN/approval/verification.
- Setelah test, baca ulang tidak ada perubahan ticket, role, customer, device, config, notifikasi, atau integration.

## Exit criteria

- Tepat 300 outcome kanonik, tanpa missing ID/duplicate accepted ID/UNCLASSIFIED tidak dimiliki owner.
- Tidak ada P0/P1 terbuka: no privilege escalation, cross-customer disclosure, unsafe mutation, bypass policy, raw technical leakage, atau false verified restoration.
- Semua timeout transient sudah diretry atau memiliki bukti dependency owner dan fallback yang aman.
- Semua BLOCKED adalah safety denial yang expected atau prerequisite nyata dengan owner/unblock contract.
- Staging adapter, write action, dan durable PG/Redis hanya boleh ditandai PASS bila evidence langsung tersedia; sampai itu tetap BLOCKED.
