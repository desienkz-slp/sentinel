# Plan Pelaksanaan: Dua Tim CS dan NOC

Status: PLAN untuk disetujui. Belum ada kode yang diubah.
Pendamping: `docs/DESAIN_DUA_TIM_CS_NOC.md` (apa dan kenapa). Berkas ini = bagaimana,
urutan, dan cara membuktikan setiap langkah.
Selaras dengan `docs/MASTER_SPEC_DELIVERY_PLAN.md` (mode COPILOT, deny-by-default,
tanpa tulis eksternal sebelum gerbang kebijakan teruji).

## 0. Koreksi atas klaim saya sebelumnya

Di jawaban sebelumnya saya menulis bahwa aturan keparahan "belum ada, hanya kolom
teks". Itu kurang tepat. Yang saya baca sekarang: `internal/incident/correlation.go`
sudah punya korelasi insiden massal (`MassThreshold`, bawaan **3 pelanggan**) atas
topologi area/router/OLT/PON. Yang belum ada adalah pemetaan ke level P1..P4 dan
penggunaannya untuk menentukan siapa dihubungi. Fase 5 membangun di atas korelasi
yang sudah ada, bukan dari nol. (Jendela waktu korelasi belum saya baca; dicek di Fase 5.)

## Status pelaksanaan

| Fase | Status | Rilis | Catatan |
|---|---|---|---|
| 0 Fondasi | SELESAI | v0.2.22 | overlay registry menggabung; flag; metrik; korpus + golden |
| 1 Router intent | SELESAI (mode `shadow` menyala di produksi) | v0.2.23 | nol selisih di korpus; menunggu pengamatan produksi sebelum `on` |
| 2 Profil tim + pembatas | SELESAI di kode; `cs_scope=shadow` menyala; belum `on` | v0.2.24 | batas ditegakkan di agen DAN workflow; uji model-jahat lulus |
| 3 Presentasi | | | |
| 4 Serah-terima | | | |
| 5 Keparahan | diblokir keputusan §6.1 | | |

Temuan Fase 0: `config.json` produksi menyimpan `registry_path` eksplisit ke
`registry.yaml`, sehingga overlay `registry.local.yaml` TIDAK PERNAH dipakai: 0 tool aktif
di produksi sebelum v0.2.22. Setelah merge: 12 tool READ aktif, 0 WRITE. Artinya sebelum
v0.2.22 agen LLM tidak melihat tool eksternal sama sekali (perintah staf berbasis kode
tetap jalan karena tidak lewat registry LLM). Keputusan operator: 12 tool tetap aktif.

## 1. Peta kondisi sekarang (titik yang akan disentuh)

Jalur pesan WhatsApp saat ini, di `server.go` (handler `/api/wa/webhook`):

```
blocklist -> identifyCaller -> [staf?] handleStaffCommand -> (jika tak cocok) engine.Run
```

| Komponen | Lokasi | Catatan untuk plan |
|---|---|---|
| Pemilahan staf vs pelanggan | `identity_wire.go`, `internal/directory` | sudah deterministik |
| Perintah staf (kode) | `staff_command_wire.go`, `staff_status_wire.go`, `internal/directory/{command,list,status}.go` | pola regex, toleran typo |
| Agen LLM tunggal | `internal/agent/agent.go` (963 baris), `RunWith` | melayani pelanggan DAN staf-bebas; dibedakan hanya lewat blok identitas di prompt |
| Klasifikasi intent keluhan | `internal/conversation/flow.go` (`DetectIntent`), `internal/standard` | 4 intent teknis + CHAT/INFO/COMPLAINT/UNCLEAR |
| Workflow diagnosis | `workflows/customer-internet-down.yaml` (6 langkah), `internal/agent/workflow.go` | satu workflow saja |
| Case engine | `internal/caseengine`, `internal/agent/casewire.go` | state legal; RESOLVED butuh verifikasi |
| Eskalasi | `internal/escalation`, `escalation_wire.go` | domain network -> NOC Senior, selain itu -> Admin; on-call + timeout |
| Otorisasi | `internal/directory/authz.go`, `internal/policy`, `internal/registry` | tiga tempat |
| Registry tool | `tools/registry.yaml` (semua `enabled:false`) + `registry.local.yaml` | overlay lokal MENGGANTIKAN seluruh file |
| Pemecah pesan | `wa-gateway/app/split.js` | sudah ada |

Kelemahan nyata yang pernah terjadi di produksi dan menjadi alasan plan ini:
1. Staf dijawab seperti pelanggan ("apa kendala Anda?").
2. Pertanyaan staf bebas jatuh ke LLM tanpa tool: "belum ada pengecekan ke billing",
   "tidak punya akses ke billing" — keduanya salah.
3. Tool baru tidak aktif di server sampai `registry.local.yaml` diedit manual
   (terjadi 3 kali).

## 2. Prinsip yang tidak boleh dilanggar

1. **Kode memilih, model menalar.** Tim, tool wajib, hak akses, dan bentuk balasan
   diputuskan kode. Standar tetap sama walau model diganti.
2. **Tool-first.** Fakta sistem hanya dari tool; tanpa itu jawabannya "belum diperiksa".
3. **Satu pintu otorisasi.** Peran -> izin -> kebijakan -> audit.
4. **Read-only.** Tidak ada tool tulis yang diaktifkan di seluruh plan ini.
5. **Tiap fase bisa dimatikan** lewat flag, tanpa deploy ulang.
6. **Tiap fase dirilis sendiri** (tag terpisah), diuji sebelum fase berikutnya.
7. **Data uji sintetis.** Tidak ada nomor, username, atau nama pelanggan/staf nyata di
   fixture, korpus, atau dokumen yang masuk repo publik.

## 3. Mekanisme keselamatan perubahan

**Flag dan mode bertahap**, disimpan di `config.json`, bisa diubah dari halaman
Pengaturan (sesuai preferensi tombol UI, bukan CLI):

| Flag | Nilai | Arti |
|---|---|---|
| `team_routing` | `off` / `shadow` / `on` | `shadow` = router menghitung keputusan dan MENCATAT, tapi jalur lama tetap yang membalas |
| `cs_scope_enforce` | `off` / `on` | pembatas data-milik-pelanggan untuk tim CS |
| `handoff` | `off` / `on` | serah-terima CS <-> NOC terstruktur |
| `severity` | `off` / `on` | pemetaan P1..P4 |

**Rollback:** setiap flag `off` mengembalikan perilaku sebelumnya. Rollback versi:
updater menyimpan binary lama (`bin/ai-noc-go.bak-*`); langkah pemulihan tertulis di
runbook (§9).

**Uji bayangan (shadow):** sebelum `team_routing=on`, jalankan `shadow` minimal
beberapa hari lalu bandingkan keputusan router dengan jalur nyata. Selisih ditinjau
manual; baru dinyalakan bila tidak ada selisih yang merugikan.

## 4. Fase

Ukuran: S (kecil, sekali duduk), M (beberapa langkah), L (besar, dipecah rilis).
Saya sengaja tidak memberi estimasi hari; ukurannya relatif.

### Fase 0 — Fondasi dan pelunasan utang (M)

Tujuan: menutup hal yang akan menggigit semua fase berikutnya.

Tugas:
1. **Overlay registry yang menggabung, bukan mengganti.** `registry.local.yaml` hanya
   boleh menimpa `enabled` (dan sejenisnya); definisi tool tetap dari `registry.yaml`
   yang ikut rilis. Berkas: `internal/config/config.go` (`defaultRegistryPath`),
   `internal/registry/registry.go`. Dampak: tool baru tidak lagi butuh edit manual di
   server. Kompatibel mundur: overlay lama yang lengkap tetap berfungsi.
2. **Korpus uji sintetis.** Kumpulan percakapan tersamarkan (staf: perintah, status,
   daftar, pertanyaan bebas; pelanggan: mati, lambat, tagihan, sapaan, tak jelas)
   dalam satu berkas data uji. Dibuat dari POLA kejadian produksi, bukan salinannya.
3. **Flag di config + toggle di Pengaturan** (tampil sebagai kartu "Mode Tim").
4. **Metrik dasar**: hitung per pesan: tim terpilih, ditangani kode vs LLM, tool yang
   dipanggil, latensi. Memakai `internal/observability`.

Uji: tabel uji overlay (lengkap, parsial, kosong, rusak); korpus terparse; flag
default `off`/tidak mengubah perilaku.
Syarat lulus: seluruh paket lulus; tanpa flag, balasan identik dengan v0.2.21 pada
korpus (uji golden).
Rollback: tidak berlaku (tanpa perubahan perilaku).

### Fase 1 — Router intent (M)

Tujuan: satu pemilah deterministik menggantikan pemilahan yang tersebar.

Rancangan: paket baru `internal/router`:

```
Route(caller, message, sessionMeta) Decision
Decision { Team: cs|noc|none,
           Handler: status_integrasi | daftar_pelanggan | cek_billing | cek_pppoe |
                    cek_traffic | riwayat | keluhan_teknis | administrasi | obrolan |
                    llm_bebas,
           Args, Reason }
```

Aturan urutan (pertama yang cocok menang):
1. identitas menentukan tim (pelanggan/tak dikenal -> `cs`; staf -> `noc` atau
   `cs-lead` untuk Admin);
2. staf: status integrasi -> daftar/ringkasan -> perintah cek -> eskalasi masuk ->
   `llm_bebas` (hanya sebagai jalan terakhir);
3. pelanggan: sapaan/terima kasih -> administrasi (tagihan/isolir/paket) -> keluhan
   teknis (intent dari `conversation.DetectIntent`) -> `llm_bebas` (CS).

Memindahkan, bukan menulis ulang: parser yang ada (`directory.ParseCommand`,
`ParseListQuery`, `ParseStatusQuestion`) dipanggil dari router; perilakunya tetap.

Berkas: `internal/router/*.go` (baru), `server.go` (webhook memanggil router),
`staff_command_wire.go` (jadi handler yang dipanggil router).

Uji:
- uji tabel dari korpus: setiap pesan -> keputusan yang diharapkan;
- uji properti: pelanggan TIDAK PERNAH mendapat `Team=noc`; staf TIDAK PERNAH
  mendapat nada CS; pesan keluhan panjang tidak pernah dibaca sebagai perintah;
- uji kompatibilitas: keputusan router == perilaku v0.2.21 untuk korpus lama.

Rilis bertahap: `team_routing=shadow` dulu (catat selisih, jalur lama membalas),
baru `on`.
Syarat lulus: nol selisih tak terjelaskan pada korpus; mode `shadow` berjalan di
produksi tanpa error; setelah `on`, pertanyaan staf yang dulu salah dijawab LLM
kini ditangani kode.
Rollback: `team_routing=off`.

### Fase 2 — Profil tim, pembatas tool, pembatas data pelanggan (L)

Tujuan: CS dan NOC benar-benar dua tim dengan akses berbeda, ditegakkan di kode.

Rancangan:
```
TeamProfile { Name, SystemPrompt, AllowedTools[], ToolScope, Tone, MaxFollowups }
```
- Dua profil: `cs`, `noc` (plus varian `cs-lead` untuk Admin).
- **Penyaring tool**: daftar tool yang DIPAPARKAN ke model dan yang boleh DIEKSEKUSI
  berasal dari profil, bukan dari prompt. Tool di luar profil ditolak di dispatcher
  walau model memintanya.
- **Pembatas data pelanggan (CS)**: pembungkus dispatcher memaksa argumen `identity`
  = pelanggan hasil `identifyCaller`. Nilai dari model diabaikan/ditolak bila
  berbeda. CS tidak pernah melihat pelanggan lain.
- `billing.list_customers` dan statistik: khusus NOC.
- Prompt per tim dipisah dari mesin (`internal/agent/profiles/`).

Berkas: `internal/agent/agent.go` (parameterisasi `buildSystemPrompt` dan daftar
tool; belum memecah berkas), `internal/tool/tool.go` (pembungkus berscope),
`internal/directory/authz.go` (izin per tim).

Uji (yang paling penting di plan ini):
- uji penolakan: CS meminta `billing.list_customers` -> ditolak dan tercatat di audit;
- uji lintas pelanggan: CS dengan `identity` pelanggan lain -> ditolak/dipaksa ke
  miliknya sendiri;
- uji **invarian terhadap model**: jalankan skenario yang sama dengan model palsu
  yang berperilaku berbeda (patuh, ngawur, meminta tool terlarang); hasil keamanan
  dan format harus sama persis. Ini membuktikan standar ada di kode, bukan prompt;
- regresi: seluruh uji agen yang ada.
Syarat lulus: nol kebocoran lintas pelanggan di seluruh skenario uji; invarian
model lulus.
Rollback: `cs_scope_enforce=off`, `team_routing` ke `off`.

### Fase 3 — Lapisan presentasi (S–M)

Tujuan: nada dan bentuk balasan konsisten, tidak bergantung model.

Tugas:
1. `internal/presentation`: templat balasan per tim (CS ramah tanpa istilah teknis,
   NOC ringkas berbasis bukti), format rupiah/tanggal, penanda "UNKNOWN".
2. Penyaring keluaran CS: tidak boleh memuat nama/nomor staf, URL internal, nama
   host, atau token — dicek kode sebelum kirim.
3. Pemecahan pesan tetap di gateway (`split.js`); aturan 500 karakter jadi konfigurasi.

Uji: golden test templat; uji penyaring (kebocoran identitas staf/host/token ->
diblokir); uji panjang.
Syarat lulus: korpus CS tidak pernah mengandung identitas staf atau istilah internal.
Rollback: templat lama tetap dipakai bila flag `off`.

### Fase 4 — Serah-terima CS <-> NOC (L)

Tujuan: eskalasi dan kabar balik terstruktur dua arah.

Rancangan:
- `Handoff` (CS -> NOC): `case_id`, pelanggan (id/username), keluhan 1 kalimat, bukti
  per sistem (nilai `UNKNOWN` bila tak bisa diperiksa), yang sudah dicoba pelanggan,
  keparahan, domain. Dibangun di atas `escalation.Payload` yang sudah ada.
- `Resolution` (NOC -> CS): `case_id`, status (`diselidiki|butuh_lapangan|selesai|
  butuh_info`), temuan, `untuk_pelanggan` (kalimat yang boleh disampaikan).
- Perintah staf untuk menutup/memperbarui: mis. `tutup <CASE-ID> <pesan>`; diaudit.
  Penutupan tetap tunduk pada aturan case engine: RESOLVED butuh verifikasi.
- CS hanya menyampaikan `untuk_pelanggan`; jawaban teknis mentah NOC tidak diteruskan.
- Pengiriman kabar ke pelanggan lewat outbox yang sudah ada (`escalation/notify.go`),
  idempoten (tidak terkirim dua kali).

Berkas: `internal/escalation` (struktur dan outbox), `internal/caseengine` (status),
`staff_command_wire.go` (perintah tutup/perbarui), `escalation_wire.go`.

Uji: eskalasi menghasilkan `Handoff` lengkap; domain network -> NOC Senior, lainnya
-> Admin (sudah ada, diuji ulang); balasan tanpa `untuk_pelanggan` tidak diteruskan;
tutup tanpa verifikasi ditolak; kirim ganda tidak terjadi; timeout memindah ke staf
berikutnya.
Syarat lulus: alur utuh dalam uji integrasi: keluhan -> eskalasi -> NOC memperbarui ->
pelanggan menerima pesan yang disaring.
Rollback: `handoff=off` mengembalikan eskalasi lama.

### Fase 5 — Keparahan P1..P4 (M) — butuh keputusan Anda

Prasyarat: Anda menetapkan ambang (§6). Tanpa itu fase ini tidak dimulai.

Tugas:
1. `internal/diagnosis/severity` (atau di `incident`): fungsi murni
   `Severity(evidence, correlation) -> P1..P4` memakai `incident.MassThreshold` yang
   sudah ada dan ambang yang Anda tetapkan.
2. Keparahan masuk ke `Handoff` dan menentukan siapa diberi tahu serta kecepatan
   timeout eskalasi.
3. Kolom `severity` di insiden diisi oleh fungsi ini, bukan teks bebas.

Uji: tabel uji batas (tepat di ambang, di bawah, di atas); korelasi massal -> naik
level; tanpa bukti -> level konservatif yang terdokumentasi.
Syarat lulus: setiap insiden baru punya level dari fungsi, bukan nilai bebas.
Rollback: `severity=off`.

### Fase 6 — Agen NOC bebas, tool-first (M) — opsional, bergating

Tujuan: pertanyaan staf yang tak cocok pola tetap dijawab dengan data nyata.

Rancangan: untuk `llm_bebas` pada tim NOC, tool read-only yang sesuai hak staf
dipaparkan ke model, dan aturan "tidak ada klaim fakta tanpa tool" ditegakkan kode:
jawaban yang memuat angka/status sistem tanpa tool call di giliran itu ditandai dan
diganti "belum diperiksa". Ini membalik aturan konteks lama yang melarang tool untuk
pesan non-keluhan, jadi HANYA untuk tim NOC dan hanya setelah Fase 2 lulus.

Uji: skenario "kenapa billing X mahal?" -> memanggil tool, tidak menebak; model palsu
yang mengarang angka -> disanitasi; hak peran tetap berlaku.
Syarat lulus: nol klaim fakta tanpa bukti di korpus staf-bebas.
Rollback: kembali ke `llm_bebas` tanpa tool.

### Fase 7 — Penataan paket (L) — terakhir, hanya bila perlu

Pemecahan `internal/agent` (3.045 baris) menjadi paket per tim, penggabungan
otorisasi ke satu paket, dan penyamaan nama (`genieacs`). Dilakukan HANYA setelah fase
1–4 stabil, bertahap per paket, dengan seluruh uji hijau di tiap langkah. Tidak ada
perubahan perilaku. Boleh ditunda tanpa menghalangi fase lain.

## 5. Strategi pengujian menyeluruh

| Lapisan | Isi | Fase |
|---|---|---|
| Tabel uji router | pesan -> keputusan | 1 |
| Uji properti | pelanggan tak pernah ke NOC; staf tak pernah bernada CS | 1, 2 |
| Uji penolakan otorisasi | tool di luar profil ditolak + diaudit | 2 |
| Uji lintas pelanggan | `identity` dipaksa milik sendiri | 2 |
| Invarian model | model palsu patuh/ngawur -> hasil keamanan sama | 2, 6 |
| Golden templat | bentuk balasan tetap | 3 |
| Uji penyaring keluaran | tak ada identitas staf/host/token ke pelanggan | 3 |
| Integrasi serah-terima | keluhan -> eskalasi -> kabar balik | 4 |
| Uji batas keparahan | tepat di ambang | 5 |
| Replay korpus | seluruh korpus lulus di tiap rilis | semua |

Pelaksanaan: `go vet`, `go test ./...`, uji Node gateway; semuanya wajib hijau sebelum
tag. Tidak ada rilis dengan uji gagal.

## 6. Keputusan yang masih terbuka (milik Anda)

1. **Ambang P1/P2/P3**: jumlah pelanggan, jendela menit, dan cakupan (router/area/
   OLT/PON); layanan yang dianggap kritis. Dibutuhkan sebelum Fase 5.
2. **Hak Admin pada jaringan**: usulan baca saja.
3. **Apa yang CS boleh katakan tentang status eskalasi**: usulan status tanpa
   perkiraan waktu kecuali diisi NOC.
4. **Batas pertanyaan lanjutan CS per kasus.**
5. **Fase 6 dijalankan atau tidak**: ini mengubah aturan "tanpa tool untuk pesan
   non-keluhan" khusus NOC.

## 7. Risiko dan mitigasi

| Risiko | Dampak | Mitigasi |
|---|---|---|
| Router salah mengarahkan keluhan pelanggan jadi perintah | pelanggan dapat balasan aneh | aturan ketat, mode `shadow`, uji properti, korpus keluhan panjang |
| Pembatas CS terlalu ketat | CS tak bisa menjawab | uji skenario CS lengkap; flag `off` |
| Pembatas CS terlalu longgar | kebocoran data lintas pelanggan | uji penolakan wajib, invarian model |
| Perubahan registry merusak produksi | tool mati | overlay kompatibel mundur; backup otomatis sebelum perubahan |
| Penyaring keluaran memblokir balasan sah | pelanggan tak dibalas | log blokir; mode peringatan dulu sebelum memblokir |
| Eskalasi ganda | staf kebanjiran | idempotensi outbox, deduplikasi kasus |
| Server lama 139 masih membalas | balasan ganda | verifikasi dimatikan sebelum uji WA |
| Regresi alur pelanggan yang sekarang jalan | gangguan layanan | golden test terhadap v0.2.21, rilis per fase |

## 8. Definisi selesai (per fase dan keseluruhan)

Per fase: uji fase hijau, seluruh uji lama hijau, diuji di mode `shadow`/flag di
produksi tanpa error, laporan hasil (apa yang terbukti, apa yang belum diuji).

Keseluruhan:
- 100% pesan staf yang cocok pola ditangani kode, tanpa LLM.
- Nol kebocoran data lintas pelanggan di uji dan di audit produksi.
- Setiap eskalasi memuat bukti per sistem, keparahan, dan domain.
- Pergantian model tidak mengubah keputusan tim, hak akses, atau format balasan
  (dibuktikan uji invarian).
- Tool baru aktif lewat rilis saja, tanpa edit manual di server.

## 9. Runbook rilis dan pemulihan (ringkas)

Rilis: commit -> tes lokal hijau -> tag `vX.Y.Z` -> Actions membangun -> di server:
check update -> apply -> verifikasi `systemctl is-active`, versi, `/api/billing/check`,
`/api/wa/status` terhubung -> uji WhatsApp manual oleh operator.

Pemulihan: matikan flag fase terkait dari Pengaturan; bila perlu pulihkan binary
`bin/ai-noc-go.bak-*` dan restart `noc-sentinel`. Sebelum mengubah berkas registry
atau config di server, buat salinan bertanggal.

## 10. Urutan yang disarankan

Fase 0 -> 1 (shadow, lalu on) -> 2 -> 3 -> 4 -> (keputusan §6.1) 5 -> (keputusan §6.5) 6
-> 7 bila perlu. Fase 1 dan 2 memberi manfaat terbesar: itu yang menutup kelas bug
"staf dijawab seperti pelanggan" dan "LLM menebak status sistem".
