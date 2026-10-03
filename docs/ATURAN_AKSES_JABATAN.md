# Aturan penanganan pesan per jabatan

Sumber di kode: `internal/router/rules.go` (aturan), `internal/directory/directory.go`
(izin per jabatan), `internal/teamscope` (batas tim CS). Dokumen ini ringkasan; bila
berbeda dengan kode, kode yang benar.

## Prinsip (berlaku untuk semua)

1. **Jabatan ditentukan kode dari nomor pengirim** (direktori staf, lalu billing), bukan
   dari isi pesan dan bukan dari klaim LLM.
2. **Staf tidak pernah diperlakukan sebagai pelanggan.** Pesan keluhan dari staf tanpa
   menyebut pelanggan dijawab "Pelanggan mana?", tidak pernah memakai nomor staf sebagai
   subjek diagnosa.
3. **Pelanggan hanya mendapat data miliknya sendiri**, dipaksa kode (bukan parameter model).
4. Tulis jaringan, ubah/hapus data, kelola staf **wajib PIN**.
5. Nilai di tabel di bawah adalah perilaku `routing=on`. Saat `off`/`shadow`, perilaku lama
   dipertahankan.

## Matriks

| Jabatan | Tim | Subjek pesan | Data yang boleh dilihat | Tulis |
|---|---|---|---|---|
| Pelanggan / tak dikenal | CS | pengirim sendiri | hanya miliknya; tanpa nama/nomor staf; hanya menyebut "tim NOC" | tidak |
| Admin | CS Lead | pelanggan yang disebut | semua pelanggan, billing, laporan, baca jaringan | ubah data pelanggan (PIN) |
| NOC Senior | NOC | pelanggan yang disebut | semua pelanggan, baca jaringan | tulis jaringan (PIN) |
| Super Admin | NOC | pelanggan yang disebut | semua termasuk direktori staf | semua, risiko pakai PIN |

## Pesan dari staf

| Pesan | Perlakuan |
|---|---|
| Perintah dikenali (`cek user X`, `cek billing X`, `daftar pelanggan`, `status radius`) | dijalankan kode, tanpa LLM, lewat otorisasi jabatan |
| `tutup/update CASE-... <pesan>` | serah-terima kasus; NOC untuk domain jaringan, Admin untuk lainnya |
| Keluhan layanan **tanpa pelanggan disebut** ("cek internet sekarang", "internet mati") | dijawab: *Pelanggan mana yang dicek?* |
| Keluhan **dengan** subjek (angka/ID/`@`/"pelanggan X") | diteruskan ke LLM tim NOC |
| Pertanyaan tentang sistem ("status radius", "server mati?") | status integrasi / LLM, bukan minta target |
| Sapaan, pertanyaan umum ("kamu tahu siapa saya?") | LLM, tanpa pengecekan jaringan |

## Pesan dari pelanggan

Selalu tim CS. Tidak pernah masuk jalur perintah staf. Identitas pelanggan dipaksa ke nomor
terverifikasi. Balasan disaring (host/IP internal, identitas staf, token) sebelum terkirim.
Keluhan nyata dibuka kasus dan dieskalasi ke NOC dengan bukti per sistem.

## Yang belum diputuskan

Ambang keparahan P1–P4, hak Admin pada jaringan, batas pertanyaan lanjutan CS.
