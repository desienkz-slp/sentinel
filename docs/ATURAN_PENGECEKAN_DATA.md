# Aturan pengecekan data (WhatsApp → staf lokal → billing)

Berlaku di `internal/directory/identifier.go` (`Identify`) dan `internal/router/rules.go`.
Ditegakkan di kode, bukan di prompt LLM.

## 1. Siapa pengirim? (urutan tetap)
1. **Nomor WhatsApp** dinormalisasi (`identity.Normalize`).
2. **Direktori staf lokal** (`staff_members` di config) dicek PERTAMA.
   Ada dan aktif → pengirim = staf dengan jabatannya. Selesai; billing TIDAK menentukan peran.
3. **Billing** dicek hanya bila bukan staf. Nomor ada di billing → pelanggan (data miliknya sendiri).
4. Tidak ketemu di mana pun → pelanggan tak dikenal: dilayani sebagai CS, tanpa data internal.
5. Billing error/mati → identifikasi tidak gagal; dicatat `BillingError`, pengirim tetap
   dilayani (staf tetap staf; selain itu pelanggan tak dikenal).

## 2. Apa yang boleh dicek?
| Pengirim | Data yang boleh | Target diagnosis |
|---|---|---|
| Pelanggan / tak dikenal | Hanya miliknya sendiri | Nomor pengirimnya sendiri |
| Staf (NOC/Admin/Super) | Sesuai jabatan & scope (billing/RADIUS/GenieACS/MikroTik) | HARUS disebut eksplisit |

- Staf tidak pernah diperlakukan sebagai pelanggan, walau nomornya juga ada di billing.
- Nomor staf tidak dipakai sebagai target diagnosis.
- Staf menulis keluhan tanpa target → ditanya: "Pelanggan mana yang dicek?".
- Pelanggan tidak bisa memakai perintah staf (`cek user …`, `tutup/update CASE-…`):
  ditolak kode. Pelanggan tidak melihat nama/nomor staf; cukup "tim NOC".

## 3. Pengecekan billing
- Pencarian by nomor: hanya untuk mengenali pengirim (langkah 1.3).
- Pencarian by username/nama oleh staf: perintah `cek billing <target>` ditangani KODE
  lewat dispatcher + gerbang otorisasi, tanpa LLM.
- Data tak ditemukan/billing mati: prinsipnya dikatakan apa adanya, tidak dikarang.
  (Redaksi pesan persisnya belum diaudit per kasus; diuji di tahap berikutnya.)
