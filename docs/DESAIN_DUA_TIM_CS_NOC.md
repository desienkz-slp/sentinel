# Rancangan Dua Tim: CS dan NOC

Status: RANCANGAN untuk disetujui. Belum diimplementasikan.
Tujuan: dua "tim" dengan tanggung jawab, akses, dan alur kerja yang jelas —
CS bekerja seperti Customer Service, NOC bekerja seperti NOC. Standar
perilaku ditegakkan di KODE (router, otorisasi, templat), bukan hanya di
prompt, sehingga tetap sama walau model diganti.

## 1. Prinsip

1. **Kode memilih, model menalar.** Pemilihan tim, pemilihan tool wajib, hak
   akses, dan bentuk balasan diputuskan kode. Model hanya menalar dari bukti
   dan menyusun kalimat.
2. **Tool-first.** Tim tidak boleh menjawab fakta sistem (status, tagihan,
   sesi) dari ingatan model. Fakta datang dari tool, atau jawabannya "belum
   diperiksa".
3. **Satu pintu otorisasi.** Setiap pemanggilan tool melewati
   peran -> izin -> kebijakan -> audit. Tim CS tidak punya jalan pintas ke
   data yang bukan miliknya.
4. **Read-only dulu.** Semua tool aktif sekarang READ. Tindakan tulis
   (mis. `mikrotik.disconnect_pppoe`) tetap nonaktif sampai ada persetujuan.
5. **Serah-terima eksplisit.** CS dan NOC saling mengoper kasus lewat satu
   format (lihat §6), bukan lewat teks bebas.

## 2. Siapa masuk ke tim mana

Penentu: hasil `identity` (Caller), bukan isi pesan.

| Pengirim | Tim | Catatan |
|---|---|---|
| Pelanggan terdaftar atau tak dikenal | **CS** | satu-satunya jalur pelanggan |
| Staf: Admin | **CS-Lead** (akses CS + data billing luas) | bukan jaringan |
| Staf: NOC Senior | **NOC** | jaringan, termasuk perintah staf |
| Staf: Super Admin | **NOC** (default) dan boleh memanggil CS | semua izin |

Pelanggan TIDAK PERNAH masuk tim NOC. Staf TIDAK PERNAH dilayani dengan nada
layanan pelanggan.

## 3. Tim CS — Customer Service

**Peran:** satu-satunya wajah ke pelanggan. Ramah, jelas, singkat, tidak
membocorkan istilah teknis berlebihan, tidak mengarang.

### Job desk
1. Menerima keluhan dan pertanyaan pelanggan (internet mati, lambat, tagihan,
   status isolir, paket).
2. Mengenali pelanggan dari nomor WA (normalisasi format 62/08/+62), tidak
   menanyakan hal yang sudah diketahui.
3. Mengumpulkan informasi yang kurang (sudah coba restart? lampu modem?
   sejak kapan?) — maksimal sejumlah pertanyaan, tidak mengulang.
4. Menjawab pertanyaan administrasi dari data billing: status akun, isolir
   dan sejak kapan, paket, tanggal tagih/isolir, tunggakan dan bulan lunas.
5. Menjalankan pemeriksaan teknis dasar yang aman (ping/DNS/TCP) bila relevan.
6. Memutuskan **selesai di CS** atau **eskalasi ke NOC**.
7. Memberi kabar balik ke pelanggan saat NOC menyelesaikan kasus.

### Akses (dibatasi data MILIK pelanggan itu sendiri)
| Sistem | Tool | Batas |
|---|---|---|
| Billing | `billing.get_customer`, `billing.get_history` | hanya pelanggan itu |
| RADIUS | — | tidak ada tool per-pelanggan: upstream hanya menyediakan bulk session/user tanpa exact server-side filter/projection |
| MikroTik | `mikrotik.get_pppoe_status` | hanya akun pelanggan itu |
| GenieACS | `genieacs.get_device_state` | hanya perangkat pelanggan itu |
| Probe | ping, dns, tcp, http | target dari pelanggan/akun itu |
| Staf | — | tidak melihat direktori; hanya menerima nama "tim NOC" |

CS **tidak** punya: daftar pelanggan lain, `billing.list_customers`,
statistik sistem, trafik interface, tindakan tulis.

### Larangan
- Tidak membuka data pelanggan lain.
- Tidak menjanjikan waktu perbaikan yang tidak diketahui.
- Tidak menyebut nama/nomor staf NOC ke pelanggan.
- Tidak menjawab status sistem tanpa memanggil tool.

### Alur CS
```
pesan pelanggan
  -> identity (Caller=pelanggan)
  -> klasifikasi intent (kode): tagihan | status akun | internet mati |
     lambat | pertanyaan umum | lainnya
  -> intent administrasi  : billing tool -> jawab
  -> intent teknis        : workflow diagnosis (billing -> radius ->
                            mikrotik -> genieacs -> probe) -> kesimpulan
  -> cukup informasi?     : tidak -> tanya (terbatas, tidak berulang)
  -> keputusan            : selesai (jawab pelanggan)
                            atau eskalasi (serah-terima ke NOC, §6)
  -> balasan lewat templat presentasi (nada CS, <=500 karakter/pesan)
```

## 4. Tim NOC — Network Operations Center

**Peran:** teknis, ringkas, berbasis bukti. Melayani staf dan menangani kasus
teknis yang dieskalasi dari CS.

### Job desk
1. Menjawab perintah dan pertanyaan staf: `cek user pppoe`, `cek traffic`,
   `cek billing`, daftar/ringkasan pelanggan, status integrasi.
2. Menerima eskalasi dari CS dan menyelidiki lintas sistem
   (billing -> RADIUS -> MikroTik -> GenieACS -> probe).
3. Mengorelasikan bukti menjadi kesimpulan dengan tingkat keyakinan;
   menandai UNKNOWN bila sistem tak tersedia (tidak menebak).
4. Menentukan tingkat keparahan dan dampak (§5).
5. Menyiapkan rekomendasi tindakan; tindakan tulis hanya dengan persetujuan
   dan PIN sesuai kebijakan.
6. Mengelola insiden: buka, korelasi kasus serupa, tutup dengan verifikasi.
7. Melaporkan hasil ke CS agar pelanggan diberi kabar.

### Akses (lintas pelanggan, sesuai jabatan staf)
| Sistem | Tool | Batas |
|---|---|---|
| Billing | semua `billing.*` (termasuk `list_customers`, `get_history`) | read-only |
| RADIUS | `get_system_stats` | aggregate-only; session/user bulk endpoints are blocked |
| MikroTik | `get_pppoe_status`, `get_interface_stats`, `get_interface_live`, `get_customer_traffic` | read-only |
| GenieACS | `get_device_state` (device ID tepat, respons terproyeksi) | read-only; bulk device list dinonaktifkan |
| Probe | ping, dns, tcp, http, mtr | read-only |
| Tulis | `mikrotik.disconnect_pppoe` | NONAKTIF; butuh persetujuan + PIN |
| Staf | direktori | sesuai jabatan, lihat §7 |

### Larangan
- Tidak bertindak pada banyak pelanggan sekaligus (kebijakan `deny-mass-actions`).
- Tidak mengklaim sistem sehat tanpa pemeriksaan nyata.
- Tidak menjawab dengan nada layanan pelanggan.

### Alur NOC
```
pesan staf / eskalasi CS
  -> router (kode):
       1. pertanyaan status integrasi   -> health adapter  -> jawab
       2. daftar/ringkasan pelanggan    -> billing.list     -> jawab
       3. perintah cek (billing/pppoe/traffic/riwayat)
                                        -> tool via authz+audit -> jawab
       4. eskalasi dari CS              -> workflow diagnosis
       5. selebihnya                    -> noc_agent (LLM, tool-first)
  -> bukti terkumpul -> korelasi -> kesimpulan + keyakinan + keparahan
  -> rekomendasi / minta persetujuan (tulis) / catat insiden
  -> balasan lewat templat presentasi (nada NOC, ringkas, <=500/pesan)
```

## 5. Tingkat keparahan (PERLU KEPUTUSAN ANDA)

Kode sekarang belum punya aturan keparahan; hanya kolom teks bebas. Usulan
kerangka (ambang angka sengaja KOSONG sampai Anda menetapkannya):

| Level | Arti | Contoh sinyal | Siapa ditindak |
|---|---|---|---|
| P1 | Layanan luas terganggu | banyak pelanggan satu router/area putus | NOC Senior segera + Admin diberi tahu |
| P2 | Kelompok terganggu | beberapa pelanggan satu ODP/area | NOC Senior |
| P3 | Satu pelanggan | satu akun/perangkat bermasalah | NOC; CS menindaklanjuti ke pelanggan |
| P4 | Informasi/administrasi | tagihan, isolir, pertanyaan | selesai di CS |

Yang harus Anda tetapkan: ambang "banyak" (berapa pelanggan dalam berapa menit
dalam satu router/area), dan layanan mana yang dianggap kritis.

## 6. Serah-terima CS <-> NOC

Format tunggal (struktur data, bukan teks bebas):

```
Eskalasi CS -> NOC
  case_id, waktu
  pelanggan : id, username (tanpa nomor telepon pelanggan di teks WA staf)
  keluhan   : ringkasan 1 kalimat
  bukti     : status per sistem (billing/radius/mikrotik/genieacs/probe),
              nilai UNKNOWN bila tidak dapat diperiksa
  sudah dicoba pelanggan : daftar
  keparahan : P1..P4 (dari aturan kode)
  domain    : network | billing | lainnya  -> menentukan peran penerima

Balasan NOC -> CS
  case_id, status: diselidiki | butuh_tindakan_lapangan | selesai | butuh_info
  temuan    : 1-3 kalimat, tanpa istilah internal
  untuk_pelanggan : kalimat yang boleh disampaikan CS (disetujui NOC)
```

Aturan: CS hanya menyampaikan isi `untuk_pelanggan`. Jawaban teknis mentah
NOC tidak diteruskan langsung ke pelanggan.

Penerima eskalasi ditentukan kode dari domain: `network` -> NOC Senior,
selain itu -> Admin (sudah berlaku sekarang di `internal/escalation`). Staf
dipilih dari direktori: aktif, on-call, urut nama; bila tak ada yang
mengakui dalam batas waktu, lanjut ke berikutnya (`AdvanceOnTimeout`).

## 7. Akses staf sesuai jabatan

Sumber tunggal: direktori staf di Pengaturan. Izin per peran (sudah di kode):

| Jabatan | Izin | Artinya |
|---|---|---|
| Admin | baca semua, laporan, baca jaringan, ubah data pelanggan | urusan billing/data; bukan jaringan tulis |
| NOC Senior | baca semua, laporan, baca jaringan, TULIS jaringan | teknis; tulis butuh PIN |
| Super Admin | semua termasuk hapus data dan kelola staf | PIN untuk aksi berisiko |

Aksi berisiko (tulis jaringan, ubah/hapus data, kelola staf) wajib PIN.
Yang boleh dilihat tim CS tentang staf: hanya nama tim, tidak nomor/identitas.
Yang boleh dilihat tim NOC: direktori sesuai jabatan, untuk eskalasi dan
mengetahui siapa yang sedang bertugas.

## 8. Apa yang sudah ada dan apa yang belum

Sudah ada (di kode sekarang):
- Identitas pengirim dan peran staf; otorisasi tool per peran; PIN; audit.
- Adapter billing/RADIUS/MikroTik/GenieACS read-only; registry deny-by-default.
- Workflow `customer-internet-down` (6 langkah), korelasi bukti, case engine.
- Eskalasi deterministik berdasarkan domain, direktori on-call, timeout.
- Perintah staf deterministik (cek billing/pppoe/traffic, daftar pelanggan,
  status integrasi), pemecah pesan >500 karakter.

Belum ada (perlu dibangun):
1. **Router intent eksplisit** yang memilih tim — sekarang tersebar di
   `handleStaffCommand` dan agent.
2. **Pemisahan dua agen** (CS vs NOC) dengan set tool dan prompt berbeda.
   Sekarang satu agen melayani semua, dibedakan lewat blok identitas di prompt.
3. **Batas "hanya data pelanggan itu"** untuk CS di level tool (sekarang
   pembatasan ada di peran, belum per-pelanggan).
4. **Aturan keparahan** (§5) — butuh keputusan Anda.
5. **Format serah-terima terstruktur** (§6) — sebagian ada di payload
   eskalasi, belum ada jalur balik NOC -> CS.
6. **Templat presentasi** per tim (nada, format) sebagai lapisan kode.

## 9. Usulan urutan membangun

1. Router intent + pemisahan jalur (kode murni, mudah diuji).
2. Set tool per tim + pembatas per-pelanggan untuk CS.
3. Prompt dan templat per tim.
4. Format serah-terima dan jalur balik NOC -> CS.
5. Aturan keparahan (setelah Anda menetapkan ambang).
6. Uji alur utuh dengan skenario nyata (replay dari log yang sudah disamarkan).

## 10. Keputusan yang dibutuhkan dari Anda

1. Ambang keparahan P1/P2/P3 dan layanan kritis (§5).
2. Apakah Admin boleh memakai tim NOC untuk jaringan? (usulan: tidak, hanya
   baca; tulis jaringan khusus NOC Senior/Super Admin.)
3. Apakah CS boleh memberi tahu pelanggan bahwa kasusnya "sedang ditangani
   tim NOC" dan perkiraan waktu? (usulan: boleh status, tanpa perkiraan waktu
   kecuali NOC mengisinya.)
4. Batas jumlah pertanyaan lanjutan CS per kasus.
