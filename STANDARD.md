# STANDAR KONTEKS & SESI — NOC Sentinel

Dokumen ini menjelaskan bagaimana sistem menjaga **perilaku yang konsisten meski model
diganti**, bagaimana **cache** bekerja, dan bagaimana **konteks dipisahkan per nomor**.

---

## 1. Masalah yang diselesaikan

Sebelumnya, keputusan "perlu dicek atau tidak" sepenuhnya bergantung pada penilaian model.
Akibatnya perilaku berubah bila model diganti:

| Model | Pesan "halo" | Pesan "tahlil kan ?" |
|---|---|---|
| Gemini 3.8 Flash | jawab tanpa cek ✔ | jawab tanpa cek ✔ |
| Model lemah | bisa memicu probe ✘ | bisa memicu 9 probe ✘ |

Selain itu, semua percakapan memakai konteks yang sama — pesan pelanggan A bisa
memengaruhi analisis pelanggan B.

---

## 2. Standar dijalankan KODE, bukan model

Aturan konteks ditegakkan di `internal/standard/standard.go` dengan **aturan tetap**.
Model hanya menjalankan; ia tidak bisa melanggarnya.

### Klasifikasi intent (deterministik)

| Intent | Contoh | Boleh probe? |
|---|---|---|
| `CHAT` | "halo", "pagi", "terima kasih", "tes" | ❌ tidak |
| `INFO` | "STATUS USER: SERVER KABUH", "TERHUBUNG KEMBALI" | ❌ tidak |
| `COMPLAINT` | "internet lambat", "wifi mati", "login PPPoE gagal" | ✅ ya |
| `UNCLEAR` | "tahlil kan ?", "apakah bisa cek jaringan?" | ❌ tidak (tanya balik) |

Urutan pemeriksaan (penting, menentukan hasil):

```
INFO  →  gejala gangguan kuat  →  pertanyaan  →  kata jaringan  →  sapaan  →  UNCLEAR
```

Dua tingkat kata keluhan, supaya tidak salah kelas:

- **`strongComplaint`** — gejala nyata (`lambat`, `mati`, `putus`, `gagal`, `loss`, …)
  → langsung `COMPLAINT`.
- **`weakContext`** — kata seputar jaringan (`internet`, `wifi`, `koneksi`, `pppoe`, …)
  → hanya `COMPLAINT` bila **bukan** pertanyaan. Ini mencegah
  *"apakah nomor ini bisa dipakai cek jaringan?"* dibaca sebagai keluhan.

Pencocokan memakai **kata utuh**, bukan substring — supaya `"p"` tidak cocok di `"pppoe"`
dan `"hi"` tidak cocok di `"wifi"`.

### Gerbang keras (tidak bisa dilanggar model)

```go
bolehProbe := standard.BolehProbe(intent)   // hanya COMPLAINT yang true
if !bolehProbe {
    tools = nil                             // daftar tool dikosongkan
}
```

Kalau intent bukan keluhan, **daftar tool tidak dikirim ke model sama sekali**.
Model tidak bisa memanggil tool yang tidak ada — jadi probe mustahil terjadi,
sekeras apa pun model "berhalusinasi".

### Standar bisa diedit tanpa build ulang

Standar tersimpan di `internal/standard/context-standard.md` dan di-embed ke binary.
Operator bisa menimpanya:

```bash
NOC_STANDARD_DOC=/path/standar-saya.md ./bin/ai-noc-go
```

Bila path tidak terbaca, **standar bawaan tetap dipakai** — sistem tidak pernah
kehilangan standar. Versi standar tercatat di setiap laporan (`standar: "1.0.0"`)
sehingga hasil bisa diaudit.

---

## 2b. Gaya bahasa (anti-template)

Masalah nyata yang ditemukan di lapangan: balasan selalu dibuka *"Halo, ada yang bisa kami
bantu?"* dan selalu meminta ID pelanggan — walaupun percakapan sudah berjalan dan nomor
pengirim sudah kita ketahui. Terasa seperti template, bukan obrolan.

Aturan yang ditambahkan ke standar:

- **"Halo" hanya untuk pesan pertama.** Pesan kedua dan seterusnya langsung ke isi.
- **Jangan minta data yang sudah kita punya** — nomor pengirim sudah diketahui.
- Hindari basa-basi kaku: *"Mohon informasikan"*, *"Silakan jelaskan"*,
  *"agar dapat kami bantu periksa"*.
- Balas **1–3 kalimat** untuk obrolan biasa.
- **Tanggapi isi pesannya**; kalau tidak paham, tanya **satu hal** saja.
- Balasan buruk vs baik:

| Pengirim | Buruk (kaku) | Baik (manusiawi) |
|---|---|---|
| "masi les" | "Halo, ada yang bisa kami bantu terkait layanan internet Anda? Silakan informasikan kendala atau ID pelanggan Anda." | "Oke, santai aja. Nanti kalau sudah sempat kabari lagi ya." |
| "gantii" | "Halo, ada yang bisa kami bantu? Mohon informasikan lebih jelas apa yang ingin Anda ganti…" | "Ganti apa nih? Password wifi, paket, atau perangkat?" |
| "ayah" | "Halo, mohon maaf, pesan yang Anda kirimkan kurang jelas…" | "Maaf, saya kurang paham. Maksudnya gimana ya?" |

Selain standar, agen juga menyuntikkan penanda konteks ke prompt setiap permintaan:

- Pesan pertama → *"(Ini pesan pertama dari pengirim ini. Balas singkat dan ramah.)"*
- Pesan lanjutan → *"(Percakapan ini sudah berjalan. JANGAN ulangi sapaan, jangan perkenalan
  ulang, dan jangan minta nomor/ID pelanggan.)"*

Penanda ini yang membuat model tidak mengulang sapaan, terlepas dari model apa pun yang dipakai.

### Hasil uji

```
kamu : ayahhhh     bot : Halo! Maaf, maksudnya gimana ya? Ada kendala dengan internetnya?
kamu : ayah        bot : Maaf, saya masih kurang paham nih. Maksudnya gimana ya?
kamu : gantii      bot : Ganti apa nih? Password wifi, paket, atau perangkat?
kamu : masi les    bot : Oke, santai aja. Nanti kalau sudah sempat kabari lagi ya.
kamu : ganti jam 8 bot : Oke, nanti jam 8 ya. Yang mau diganti apa nih?
```

---

## 3. Sesi percakapan per nomor

Setiap pengirim punya konteksnya sendiri (`internal/session/session.go`).

### Kunci sesi

Semua bentuk identitas nomor yang sama disatukan:

| Input | Kunci |
|---|---|
| `628123456789@s.whatsapp.net` | `628123456789` |
| `628123456789:79@s.whatsapp.net` | `628123456789` (device id dibuang) |
| `+62 812-3456-789` | `628123456789` |
| `120363000000000000@g.us` | `120363000000000000` |

### Yang disimpan per nomor

- **Riwayat percakapan** — 10 giliran terakhir
- **Target terakhir** — supaya pesan lanjutan *"masih lambat"* tidak perlu menyebut ulang alamat
- **Intent terakhir** — untuk mendeteksi percakapan lanjutan

### Isolasi

Konteks pelanggan A **tidak** bisa memengaruhi analisis pelanggan B:

- Riwayat disimpan per kunci sesi — `History(keyA)` tidak memuat pesan `keyB`.
- Cache memakai kunci `hash(nomor + pertanyaan)` — pertanyaan sama dari nomor berbeda
  menghasilkan kunci berbeda, jadi tidak berbagi jawaban.

### Kedaluwarsa

| Parameter | Default | Arti |
|---|---|---|
| `sesi_ttl_min` | 120 | Percakapan dianggap selesai setelah idle 2 jam |
| `cache_ttl_min` | 10 | Jawaban cache berlaku 10 menit |
| `maks_giliran` | 10 | Batas riwayat per nomor (proteksi memori) |
| — | 500 | Batas percakapan aktif; yang paling lama idle dibuang |

---

## 4. Cache jawaban

Tujuan: keluhan identik dari nomor sama dalam jendela waktu singkat tidak dihitung ulang.
Hemat token dan waktu secara signifikan.

### Cara kerja

```
Kunci cache = SHA256( nomor + "|" + normalisasi(pertanyaan) )
```

Normalisasi (huruf kecil, tanda baca dibuang, spasi dirapatkan) membuat variasi penulisan
berbagi entri yang sama:

| Pertanyaan | Kunci cache |
|---|---|
| `Internet Lambat!` | sama |
| `internet   lambat` | sama |
| `internet lambat, tolong cek ke 8.8.8.8` | berbeda (bukan pertanyaan yang sama) |

### Yang TIDAK di-cache

- Hasil yang error atau timeout (`rep.Error != ""`)
- Jawaban kosong

### Cache dibuang otomatis saat model berganti

```go
if modelBaru != modelLama { sesi.CacheClearAll() }
```

Alasan: standar konteks tetap sama, tapi **kualitas jawaban berbeda antar model**.
Menyajikan jawaban model lama setelah ganti model akan menyesatkan.

Bisa juga dikosongkan manual: tombol **🗑 Kosongkan cache** di dashboard, atau
`POST /api/sesi/cache`.

### Hasil uji

```
1) "internet lambat, tolong cek ke 8.8.8.8"  → cache=False  18.399 ms
2) "Internet Lambat, Tolong Cek Ke 8.8.8.8"  → cache=True        0 ms   (penulisan beda)
3) nomor berbeda, pertanyaan sama             → cache=False  24.884 ms  (konteks terpisah)
```

---

## 5. Cara memantau

Dashboard → panel **Standar Konteks & Sesi**:

- Versi standar aktif (dan apakah memakai file custom)
- Jumlah percakapan aktif, entri cache, rasio hit/miss
- Tabel percakapan: nomor, giliran, intent, target, idle, pesan terakhir

Endpoint:

```bash
curl -s http://127.0.0.1:8090/api/sesi | python -m json.tool
curl -X POST http://127.0.0.1:8090/api/sesi/cache      # kosongkan cache
```

---

## 6. Konfigurasi

| Env | Default | Arti |
|---|---|---|
| `NOC_STANDARD_DOC` | *(kosong)* | path file standar pengganti |
| `NOC_CACHE_TTL_MIN` | `10` | umur cache (menit) |
| `NOC_SESI_TTL_MIN` | `120` | umur sesi (menit) |

---

## 7. Menambah aturan baru

1. Tambahkan kata kunci ke `strongComplaint` (gejala) atau `weakContext` (konteks jaringan)
   di `internal/standard/standard.go`.
2. Tambahkan kasus uji di `internal/standard/standard_test.go`.
3. Jalankan `go test ./internal/standard/`.
4. Naikkan `Version` bila aturannya berubah, supaya laporan lama bisa dibedakan.

Untuk perubahan yang lebih besar (alur berpikir, format jawaban), edit
`internal/standard/context-standard.md` — tidak perlu menyentuh kode Go.
