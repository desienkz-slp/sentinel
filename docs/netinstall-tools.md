# Skill — Tool Networking untuk Inspeksi NOC Sentinel

Daftar tool jaringan yang boleh dipasang di server NOC (melalui dashboard → Panel Tool Networking, atau `POST /api/netinstall/install` dengan PIN superadmin). Semua pemasangan = aksi rawan → hanya superadmin yang bisa menjalankan.

## Katalog & cara pakai

| Tool | Kategori | Kegunaan | Cara pakai |
|---|---|---|---|
| `mtr` | trace | Kombinasi ping + traceroute live, lihat loss per hop | `mtr -rw 8.8.8.8` (laporan; `-c N` untuk hitungan paket) |
| `iperf3` | port | Uji throughput TCP/UDP antar dua titik | server: `iperf3 -s`; client: `iperf3 -c <server> -p 5201` |
| `tcpdump` | capture | Tangkap & analisis paket pada interface | `tcpdump -i eth0 -n host 10.0.0.1 and port 53` |
| `nmap` | port | Pindai port & deteksi host/layanan | `nmap -sT -p 22,80,443 <host>` (TCP connect) |
| `whois` | dns | Info registrasi IP/domain (ASN, owner) | `whois 172.18.20.133` |
| `arping` | link | Cek keberadaan host di segmen L2 (ARP) | `arping -I eth0 -c 3 192.168.1.1` |
| `ethtool` | link | Info link fisik (speed/duplex/statistik NIC) | `ethtool eth0`; `ethtool -S eth0` (statistik) |
| `fping` | icmp | Ping banyak host sekaligus (subnet) | `fping -c 3 -g 10.10.10.0/24 2>&1 \| grep alive` |
| `tcptraceroute` | trace | Traceroute via TCP (tembus firewall blok ICMP) | `tcptraceroute -n <host> 80` |
| `traceroute` | trace | Lihat jalur hop ke tujuan | `traceroute -n 8.8.8.8` |
| `net-tools` | link | netstat/ifconfig/route/arp klasik | `netstat -rn` (routing); `netstat -an` (koneksi) |

## Alur pemakaian

1. **Endpoint B** (saat inspeksi butuh data lebih dalam) meminta pemasangan tool → jadi permintaan.
2. **Superadmin** menyetujui lewat dashboard (butuh PIN) → `apt-get install` dijalankan server.
3. Endpoint B / teknisi memakai tool sesuai tabel cara pakai di atas.
4. Hasil inspeksi direkam ke resep (`recipes.json`) sebagai "cara pengecekan" yang terbukti.

## Keamanan

- Hanya tool di katalog di atas yang boleh dipasang (allowlist — bukan paket arbitrer).
- Pemasangan wajib superadmin + PIN. Tidak ada auto-install oleh model.
- Tool hanya READ/diagnostik; tidak ada instalasi service baru atau perubahan konfigurasi jaringan.
