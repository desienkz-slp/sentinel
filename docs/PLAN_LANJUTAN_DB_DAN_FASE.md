# Rencana lanjutan: fase tersisa + PostgreSQL + Redis

Lanjutan dari `PLAN_DUA_TIM_CS_NOC.md` (Fase 0–5 selesai, v0.2.22–v0.2.29).
Urutan dikerjakan satu per satu. Satu fase = satu rilis; rilis hanya bila `go vet`,
`go test ./...`, dan audit data pribadi hijau. Tiap fitur baru punya mode
`off|shadow|on` dan default `off`, jadi rollback = ubah satu flag.

## Fase yang belum selesai (dikerjakan lebih dulu)

| Fase | Isi | Syarat lulus |
|---|---|---|
| **A** (= Fase 6) | Agen NOC tool-first: tool read-only sesuai hak staf dipaparkan; klaim fakta tanpa tool call ditandai "belum diperiksa" oleh kode | model palsu yang mengarang angka disanitasi; hak peran tetap berlaku; flag `noc_toolfirst` |
| **B** (= Fase 7) | Penataan paket tanpa ubah perilaku: otorisasi satu paket, penyamaan nama | seluruh uji hijau tiap langkah; tidak ada perubahan perilaku |

Fase 5 (keparahan) selesai di kode. Ambang P1–P3 tetap diisi operator lewat
`severity_policy`; sampai itu terisi, hasilnya UNRATED (disengaja).

## Database

Server 133: Ubuntu 22.04, disk kosong 5,5 GB, RAM 4 GB. PostgreSQL 14 dan Redis 6
dari apt. Tidak ada yang dibuka ke jaringan (hanya `127.0.0.1`/socket).

| Fase | Isi | Syarat lulus |
|---|---|---|
| **C** | Skrip pasang satu perintah `deploy/install-datastores.sh` (idempoten, ada `--check`); pasang PostgreSQL + Redis di 133; buat role/DB; kredensial di berkas root-only, tidak di git/log | `--check` dua kali tanpa perubahan; `pg_isready`, `redis-cli ping` OK; panel Observabilitas menampilkan postgres/redis tersedia |
| **D** | Jalankan migrasi (`migrations/*.sql`) dengan pelacak versi; tambah tabel `handoffs` | migrasi berulang aman; skema sama di uji dan produksi |
| **E** | Penyimpanan kasus + handoff ke PostgreSQL; JSON jadi fallback bila DB mati; mode `store_pg=off|shadow|on` (shadow = tulis ke keduanya, baca dari JSON, catat selisih) | uji: DB mati → tetap jalan lewat JSON; tidak ada kehilangan data; selisih shadow nol |
| **F** | Impor sekali jalan data JSON lama ke PostgreSQL (idempoten, ada dry-run) | jumlah baris sama dengan sumber; ulang tidak menggandakan |
| **G** | Redis: cache sesi/dedupe/ratelimit dengan fallback ke cache lokal bila Redis mati | uji: Redis mati → perilaku tetap; kunci punya TTL |
| **H** | Dokumen, status panel, rilis akhir, ringkasan keputusan yang menunggu operator | semua hijau di produksi |

## Aturan keselamatan

- Pemasangan di produksi hanya setelah skrip lolos `--check` dan cadangan `data/` dibuat.
- Kata sandi dibuat acak oleh skrip, disimpan root-only (mode 600), tidak pernah dicetak.
- Migrasi tidak menghapus apa pun; JSON tidak dihapus sampai `store_pg=on` terbukti stabil.
- Pesan commit dan dokumen tidak memuat IP produksi, nomor, atau token.

## Status

| Fase | Status | Rilis |
|---|---|---|
| A | SELESAI di kode; `noc_toolfirst=shadow`; belum `on` | v0.2.30 |
| B | SELESAI (dievaluasi; tanpa perubahan kode) | - |
| C | SELESAI: PostgreSQL 14 + Redis 6 aktif di 133, hanya lokal; `--check` bersih 2x | c2c22cf |
| D | SELESAI: runner migrasi + checksum + 003_handoffs; teruji di PostgreSQL asli | v0.2.31 |
| E | SELESAI: handoff + kasus -> PostgreSQL (`store_pg=shadow`), JSON sumber kebenaran; PG mati tidak mengganggu | v0.2.31-32 |
| F | SELESAI di kode: `ai-noc-go -import-pg [-dry-run]`, idempoten, terverifikasi | v0.2.32 |
| G | SELESAI di kode: dedupe bersama Redis (`store_redis`), Redis mati jatuh ke lokal; teruji di Redis asli | v0.2.33 |
| H | SELESAI: dokumen + verifikasi produksi | v0.2.33 |

## Catatan Fase B (hasil evaluasi)

- Nama `GenieACS`: 48 pemakaian, satu ejaan. Tidak ada yang disamakan.
- Dua `Authorize` sengaja terpisah: `directory.Authorize` = siapa (jabatan, izin, PIN);
  `policy.ExecutionGuard.Authorize` = apakah aksi lolos kebijakan, risiko, persetujuan.
  Digabung = lapisan hilang. Tidak digabung.
- `internal/agent` kini 1.000 baris di `agent.go`; logika tim sudah berkas sendiri.
  Pemindahan antar-paket ditunda: risiko untuk produksi lebih besar dari manfaatnya.

## Keadaan produksi (133) saat rencana ini ditutup

| Komponen | Keadaan |
|---|---|
| Versi | v0.2.33 |
| PostgreSQL 14 | aktif, hanya lokal; migrasi 001-003 terterapkan; 2 kasus + 4 handoff diimpor dan terverifikasi |
| Redis 6 | aktif, hanya lokal, berkata sandi; dipakai dedupe |
| `store_pg` | shadow: JSON sumber kebenaran, PG menerima salinan |
| `store_redis` | shadow: keputusan dedupe tetap lokal, Redis dibandingkan |
| `routing`, `cs_scope` | on |
| `presenter`, `handoff`, `severity`, `noc_toolfirst` | shadow |

## Perintah operator

- Cek datastore tanpa mengubah: `deploy/install-datastores.sh --check`
- Impor ulang JSON ke PG: `ai-noc-go -import-pg -dry-run` lalu `ai-noc-go -import-pg`
- Naikkan mode lewat Pengaturan atau `POST /api/config` (`store_pg`, `store_redis`, ...)
- Cadangan otomatis `data-backup-*.tar.gz` dibuat tiap skrip pasang dijalankan.

## Belum dilakukan (butuh keputusan atau pengamatan)

1. **`store_pg=on`**: baru bermakna bila pembacaan dari PG ditambahkan. Saat ini `on` dan
   `shadow` sama (JSON tetap dibaca). Pembacaan dari PG ditunda sampai selisih terbukti
   nol selama masa pengamatan nyata.
2. **`store_redis=on`**: baru berguna bila ada lebih dari satu instance. Satu instance
   cukup dengan dedupe lokal.
3. **Ambang P1-P3** (`severity_policy`): belum diisi operator, hasil keparahan UNRATED.
4. **Uji WhatsApp nyata** untuk `presenter`, `handoff`, `noc_toolfirst` sebelum `on`.
5. Server 139 belum diverifikasi mati.
6. Pencadangan PostgreSQL terjadwal (`pg_dump`) belum dibuat.
