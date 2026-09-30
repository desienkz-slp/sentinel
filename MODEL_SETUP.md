# Panduan Setup Model — NOC Sentinel (ai-noc-go)

Ada **tiga tempat** model dikonfigurasi, dan ketiganya punya peran berbeda.

| # | Model | Dipakai untuk | Endpoint | Bisa diubah dari UI? |
|---|---|---|---|---|
| 1 | **Model analisis** | Agen diagnostik memilih tool & menyimpulkan | `/v1/chat/completions` | ✅ ya |
| 2 | **Model Codex** | Eskalasi analisis mendalam | `/v1/responses` (via Codex CLI) | ✅ ya |
| 3 | **Model Hermes** | Asisten yang sedang Anda ajak bicara | sesuai config Hermes | ❌ file `config.yaml` |

---

## 1. Model analisis (paling sering diubah)

Ini model yang **memilih tool diagnostik** (ping → DNS → TCP → traceroute → radius)
lalu menulis kesimpulan `VERDICT / KEYAKINAN / AKAR_MASALAH / BUKTI / REKOMENDASI`.

**Syarat mutlak: harus mendukung function calling.** Tanpa itu agen tidak bisa
memanggil tool sama sekali.

### Cara mengubah (lewat dashboard — disarankan)

1. Buka `http://127.0.0.1:8090`
2. Klik **⚙ Pengaturan**
3. Ubah **Model default** (atau pilih dari dropdown **Model analisis** di panel kiri)
4. Klik **Simpan**
5. Klik **Tes koneksi LLM (completion nyata)** untuk memastikan modelnya benar-benar menjawab

Berlaku langsung, tanpa restart.

### Cara mengubah (permanen, lewat file)

Edit `config.json` di folder aplikasi:

```json
{
  "llm_model": "grip/deepseek-v4-pro"
}
```

Atau lewat environment variable:

```bash
NOC_LLM_MODEL=grip/deepseek-v4-pro ./bin/ai-noc-go.exe
```

Urutan prioritas: **default → `config.json` → environment → UI (runtime)**

### Cara mengubah (tanpa membuka browser)

```bash
curl -X POST http://127.0.0.1:8090/api/config \
  -H 'Content-Type: application/json' \
  -d '{"llm_model":"grip/deepseek-v4-pro"}'
```

---

## 2. Model Codex (eskalasi)

Dipakai saat keyakinan agen < 60% atau LLM gagal — analisis dialihkan ke
Codex CLI untuk pendapat kedua.

```bash
# lewat UI: Pengaturan → Model Codex (eskalasi)
# atau:
curl -X POST http://127.0.0.1:8090/api/config \
  -H 'Content-Type: application/json' -d '{"codex_model":"cx/gpt-5.6-terra"}'
```

Uji dengan tombol **Tes Codex CLI** di panel Pengaturan.

> **Catatan:** model `cx/*` **hanya jalan lewat Codex** (endpoint `/v1/responses`).
> Model ini **tidak bisa** dipakai sebagai model analisis (lihat bagian Peringatan).

---

## 3. Model Hermes (asisten ini)

Bukan bagian dari ai-noc-go. Diatur di
`C:\Users\desie\AppData\Local\hermes\config.yaml`:

```yaml
model:
  default: claude-opus-4.8
  provider: griphub-router-claude
  base_url: https://griphubrouter.web.id/v1
  api_mode: anthropic_messages
```

Setelah mengedit, **restart Hermes** (perubahan config tidak berlaku live).

Konfigurasi MoA (Multi-Agent) juga di file itu, di bagian `moa.presets` —
ada preset `default`, `architect`, `backend` dengan aggregator Gemini.

---

## Hasil uji langsung semua kandidat

Diuji dengan **completion nyata + function calling** ke 9Router, bukan dari katalog:

| Model | Function calling | Latensi | Catatan |
|---|---|---|---|
| `ag/gemini-3.8-flash-high` | ✅ | 3.9s | **default sekarang** |
| `ag/gemini-pro-agent` | ✅ | 8.5s | aggregator MoA Anda |
| `ag/gemini-3.8-flash` | ✅ | 4.2s | varian lebih ringan |
| `ag/gpt-oss-120b-medium` | ✅ | 1.7s | **tercepat** |
| `grip/deepseek-v4-flash` | ✅ | 3.3s | hemat token |
| `grip/deepseek-v4-pro` | ✅ | — | paling kuat di keluarga deepseek |
| `grip/glm-5.3` | ✅ | 2.8s | alternatif murah |
| `grip/qwen-3.8-max` | ✅ | 1.4s | cepat |
| `cx/gpt-5.6-terra` | ❌ **GAGAL** | — | error `Invalid value: 'ultra'` |

**7 dari 8 kandidat lolos.** Katalog 9Router mengklaim **semua 92 model** mendukung
tools — klaim itu tidak akurat, karena `cx/*` gagal total di `/v1/chat/completions`.

---

## ⚠️ Peringatan penting: `cx/*` tidak bisa jadi model analisis

Model ber-prefix `cx/` (gpt-5.6-terra, gpt-6-sol, gpt-5.4, dst.) hanya berbicara
lewat endpoint **`/v1/responses`** — protokol yang dipakai Codex CLI.

Agen ai-noc-go memakai **`/v1/chat/completions`**. Kalau Anda memilih model `cx/*`
sebagai model analisis, setiap diagnosis akan gagal dengan:

```
llm http 400: {"error":{"message":"Invalid value: 'ultra'. Supported values are:
'none', 'minimal', 'low', 'medium', 'high', 'xhigh', and 'max'."}}
```

**Aturan praktis:**

| Prefix | Model analisis | Model Codex |
|---|---|---|
| `ag/*` | ✅ boleh | ❌ tidak perlu |
| `grip/*` | ✅ boleh | ❌ tidak perlu |
| `cx/*` | ❌ **jangan** | ✅ **harus ini** |

---

## Preset yang disarankan

### Cepat & murah (volume tinggi, banyak pesan WhatsApp)

```json
{ "llm_model": "ag/gpt-oss-120b-medium", "max_steps": 8 }
```

1.7 detik per langkah. Cocok untuk cek dasar "internet lambat".

### Seimbang (bawaan, disarankan)

```json
{ "llm_model": "ag/gemini-3.8-flash-high", "max_steps": 12 }
```

### Mendalam (kasus sulit, gangguan berulang)

```json
{ "llm_model": "grip/deepseek-v4-pro", "max_steps": 16, "llm_timeout_sec": 180 }
```

**Kalau `max_steps` dinaikkan, `wa-gateway/app/webhook.js` timeout juga harus naik**
(bawaan 180000 ms = 3 menit). Rumus anggaran waktu agen:

```
NOC_LLM_TIMEOUT × 2 + NOC_MAX_STEPS × NOC_DIAG_TIMEOUT + 120 detik
```

Nilainya **harus lebih kecil** dari timeout gateway, kalau tidak diagnosis akan
terpotong di tengah dan hasilnya `TIDAK DIKETAHUI`.

---

## Cara memverifikasi model benar-benar bekerja

Jangan percaya daftar model — **selalu uji dengan completion nyata.**

### Tes cepat dari dashboard
Tombol **Tes koneksi LLM** di panel Pengaturan. Menghasilkan `PONG` + latensi.

### Tes dari terminal
```bash
curl -s -X POST http://127.0.0.1:8090/api/llm/ping | python -m json.tool
```

### Tes function calling (yang paling menentukan)
```bash
curl -s http://127.0.0.1:8090/api/tools | python -c "import json,sys;print(len(json.load(sys.stdin)),'tool siap')"
```

Lalu jalankan diagnosis nyata dari panel kiri — kalau agen benar-benar memanggil
tool (muncul langkah `TOOL · ping …`), berarti function calling model itu bekerja.

### Tes end-to-end lewat WhatsApp
Kirim pesan pribadi ke nomor gateway: *"cek konektivitas ke 8.8.8.8"*.
Lihat log: `grep "\[WA\]" server.log`

---

## Kalau model bermasalah

| Gejala | Penyebab | Solusi |
|---|---|---|
| `Invalid value: 'ultra'` | model `cx/*` dipakai sebagai model analisis | ganti ke `ag/*` atau `grip/*` |
| `401 Invalid API key` | API key salah/kosong | isi di Pengaturan, atau cek `~/.codex/auth.json` |
| `No active credentials for provider` | provider belum punya kredensial di 9Router | pilih model dari provider lain |
| Agen tidak memanggil tool | model tidak mendukung function calling | ganti model (uji dulu dengan tabel di atas) |
| Hasil selalu `TIDAK DIKETAHUI` + error `dibatalkan / timeout` | timeout gateway terlalu pendek | naikkan `timeout` di `wa-gateway/app/webhook.js` |
| Balasan lambat > 90 detik | model berat / `max_steps` besar | pakai model lebih cepat atau turunkan `max_steps` |

---

## Ringkasan alur setup

```
Pilih model  →  Simpan  →  Tes koneksi LLM  →  Jalankan diagnosis uji  →  Kirim WA uji
                (UI)       (harus PONG)         (harus ada langkah TOOL)    (harus dibalas)
```

Kalau keempat langkah lolos, model sudah terpasang dengan benar.
