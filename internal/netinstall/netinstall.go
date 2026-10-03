// Package netinstall mengelola pemasangan tool jaringan (diagnostik) di server,
// untuk membantu inspeksi. Semua aksi pemasangan = rawan (menjalankan apt,
// mengubah sistem) -> WAJIB persetujuan superadmin, tidak pernah otomatis.
//
// Tool yang diizinkan dibatasi daftar tetap (allowlist) supaya Endpoint B tidak
// bisa meminta paket arbitrer. Setiap tool punya deskripsi + contoh pemakaian
// (disimpan sebagai "skill"/resep cara pakai).
package netinstall

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// ToolInfo mendeskripsikan satu tool jaringan yang boleh dipasang + cara pakainya.
type ToolInfo struct {
	Name        string // nama binary
	Pkg         string // nama paket apt (biasanya = Name)
	Description string // kegunaan untuk inspeksi NOC
	Usage       string // contoh perintah + penjelasan singkat
	Category    string // pengelompokan (icmp / dns / trace / capture / port / link)
}

// Catalog = daftar tool jaringan yang diizinkan dipasang (allowlist tetap).
// Ini "skill/cara pakai" yang tersimpan: apa tool-nya, untuk apa, bagaimana.
var Catalog = []ToolInfo{
	{Name: "mtr", Pkg: "mtr", Category: "trace",
		Description: "Kombinasi ping + traceroute live, melihat loss per hop.",
		Usage:       "mtr -rw 8.8.8.8  (laporan 10 paket; -c untuk hitungan)"},
	{Name: "iperf3", Pkg: "iperf3", Category: "port",
		Description: "Uji throughput TCP/UDP antar dua titik.",
		Usage:       "iperf3 -c <server> -p 5201  (client); iperf3 -s (server)"},
	{Name: "tcpdump", Pkg: "tcpdump", Category: "capture",
		Description: "Tangkap & analisis paket pada interface.",
		Usage:       "tcpdump -i eth0 -n host 10.0.0.1 and port 53"},
	{Name: "nmap", Pkg: "nmap", Category: "port",
		Description: "Pindai port & deteksi host/layanan.",
		Usage:       "nmap -sT -p 22,80,443 <host>  (TCP connect, non-root)"},
	{Name: "whois", Pkg: "whois", Category: "dns",
		Description: "Info registrasi IP/domain (ASN, owner).",
		Usage:       "whois 172.18.20.133"},
	{Name: "arping", Pkg: "arping", Category: "link",
		Description: "Cek keberadaan host di segmen L2 (ARP).",
		Usage:       "arping -I eth0 -c 3 192.168.1.1"},
	{Name: "ethtool", Pkg: "ethtool", Category: "link",
		Description: "Info/setelan link fisik (speed, duplex, statistik NIC).",
		Usage:       "ethtool eth0  (atau ethtool -S eth0 untuk statistik)"},
	{Name: "fping", Pkg: "fping", Category: "icmp",
		Description: "Ping banyak host sekaligus (jangkauan/subnet).",
		Usage:       "fping -c 3 -g 10.10.10.0/24 2>&1 | grep alive"},
	{Name: "tcptraceroute", Pkg: "tcptraceroute", Category: "trace",
		Description: "Traceroute via paket TCP (menembus firewall yang blok ICMP).",
		Usage:       "tcptraceroute -n <host> 80"},
	{Name: "traceroute", Pkg: "traceroute", Category: "trace",
		Description: "Lihat jalur hop ke tujuan (UDP/ICMP).",
		Usage:       "traceroute -n 8.8.8.8"},
	{Name: "net-tools", Pkg: "net-tools", Category: "link",
		Description: "Menyediakan netstat/ifconfig/route/arp klasik.",
		Usage:       "netstat -rn  (tabel routing); netstat -an  (koneksi)"},
}

// Lookup mengembalikan info tool berdasarkan nama (case-insensitive).
func Lookup(name string) (ToolInfo, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	for _, t := range Catalog {
		if strings.ToLower(t.Name) == n || strings.ToLower(t.Pkg) == n {
			return t, true
		}
	}
	return ToolInfo{}, false
}

// Names mengembalikan semua nama tool ter-allowlist (urut).
func Names() []string {
	out := make([]string, 0, len(Catalog))
	for _, t := range Catalog {
		out = append(out, t.Name)
	}
	sort.Strings(out)
	return out
}

// IsAllowed melaporkan apakah paket/binary boleh dipasang (allowlist).
func IsAllowed(name string) bool {
	_, ok := Lookup(name)
	return ok
}

// Install memasang satu tool via apt-get (root). Mengembalikan error bila paket
// tidak ada di allowlist atau apt gagal. Dipanggil HANYA setelah superadmin
// menyetujui — fungsi ini murni eksekutor, tidak punya gerbang sendiri.
func Install(name string) error {
	info, ok := Lookup(name)
	if !ok {
		return fmt.Errorf("tool %q tidak ada di daftar yang diizinkan", name)
	}
	// apt-get update singkat lalu install. DEBIAN_FRONTEND=noninteractive supaya
	// tidak menggantung menunggu input.
	cmd := exec.Command("apt-get", "install", "-y", "--no-install-recommends", info.Pkg)
	cmd.Env = append(cmd.Env, "DEBIAN_FRONTEND=noninteractive")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apt-get install %s gagal: %v: %s", info.Pkg, err, tail(out, 300))
	}
	return nil
}

func tail(s []byte, n int) string {
	str := strings.TrimSpace(string(s))
	if len(str) > n {
		return str[len(str)-n:]
	}
	return str
}

// Installed melaporkan tool yang sudah terpasang (binary ditemukan di PATH).
func Installed(names []string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		_, err := exec.LookPath(n)
		out[n] = err == nil
	}
	return out
}
