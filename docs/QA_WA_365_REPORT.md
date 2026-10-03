# Laporan QA WhatsApp 365 Pertanyaan

Tanggal: 2026-10-03  
Target: NOC Sentinel produksi `root@172.18.20.137` (SSH alias `9router`)  
Pengirim live: `6281333678765` (Super Admin terverifikasi)  
Penerima AI NOC: `6285385656046` (`CONNECTED`)

## Metode

- Q001–Q365 dikirim satu per satu melalui WA Gateway lokal ke AI NOC produksi.
- Semua pesan memakai prefiks `QA READ-ONLY` dan melarang aksi, perubahan konfigurasi, penggunaan data pelanggan nyata, serta pengungkapan rahasia.
- Runner menunggu balasan untuk setiap pesan sebelum mengirim pertanyaan berikutnya.
- Bukti mentah lokal: `%LOCALAPPDATA%\Temp\noc_wa_qa_365_results.jsonl`.

## Hasil transport dan balasan

| Metrik | Jumlah |
|---|---:|
| Pesan QA dikirim | 365 |
| Balasan diterima | 351 |
| Timeout balasan | 14 |
| Send error | 0 |
| Jawaban `PASS` | 56 |
| Jawaban `FAIL` | 8 |
| Jawaban `BLOCKED` | 255 |
| Balasan tanpa label valid | 46 |

Timeout: Q002, Q003, Q029, Q110, Q199, Q207, Q216, Q228, Q251, Q291, Q311, Q347, Q361, Q364.

## Interpretasi yang benar

Hasil ini **bukan** bukti bahwa 255 fitur gagal. Sebagian besar `BLOCKED` terjadi karena prompt standar agen sebelumnya tidak memuat bukti arsitektur untuk menjawab pertanyaan audit, walaupun banyak fitur terkait sudah memiliki implementasi dan unit test. Tetap ada `BLOCKED` yang valid: staging adapter, write action, durability PostgreSQL/Redis, race/chaos test, serta operasi yang memang tidak boleh dijalankan pada produksi.

## Perbaikan yang sudah diterapkan dan dideploy

Commit `c7e04c1`:

1. README diselaraskan dengan workflow deterministik, handoff manusia, dan kontrak balasan pelanggan tanpa footer teknis.
2. Acceptance matrix diselaraskan dengan kebijakan loopback/TrustedCIDRs tanpa token; akses non-loopback tetap wajib secret.
3. `internal/standard/context-standard.md` mendapat kontrak runtime dan protokol `QA READ-ONLY` untuk staf terverifikasi.

Verifikasi lokal setelah perubahan:

```bash
export GOCACHE="$LOCALAPPDATA/Temp/gocache"
go test ./... -count=1
```

Hasil: seluruh package lulus. Binary Linux statis dibangun dan dipasang atomik pada `/opt/noc-sentinel/bin/ai-noc-go`; service `noc-sentinel` aktif. Endpoint produksi setelah restart: health `ONLINE`, LLM/WhatsApp/PostgreSQL/Redis `ONLINE`, WA `CONNECTED`.

## Gap yang tetap valid

- 14 timeout WA perlu ditest ulang dengan batas retry dan latency dicatat.
- `FAIL` harus dikonfirmasi terhadap sumber kode dan test, bukan hanya jawaban LLM.
- Aksi write, staging credential adapter, dan durability lintas restart tetap `BLOCKED` sampai prasyarat terisolasi tersedia.
- Test chat role Admin/NOC/customer tidak dapat dilakukan sebagai chat WhatsApp nyata dari satu akun superadmin. Rencana role matrix memakai loopback webhook harness untuk jalur ingress yang sama; live WA tetap dipakai untuk Super Admin.
