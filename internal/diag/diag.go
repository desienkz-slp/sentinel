// Package diag menjalankan probe diagnostik jaringan yang aman (whitelist,
// tanpa shell, argumen terpisah) untuk dipakai agen NOC maupun tombol manual di UI.
package diag

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"ainoc/internal/llm"
)

type Result struct {
	Tool       string `json:"tool"`
	Target     string `json:"target"`
	OK         bool   `json:"ok"`
	Output     string `json:"output"`
	Err        string `json:"err,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type Runner struct {
	Timeout time.Duration
}

func New(timeoutSec int) *Runner {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	return &Runner{Timeout: time.Duration(timeoutSec) * time.Second}
}

var (
	hostRe   = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]{0,252}[A-Za-z0-9])?$`)
	hostPort = regexp.MustCompile(`^([A-Za-z0-9._-]+):(\d{1,5})$`)
)

// Allowed memastikan target tidak menyusupkan flag/argumen tambahan.
func Allowed(target string) bool {
	if target == "" || strings.HasPrefix(target, "-") || strings.ContainsAny(target, " 	\"'`;&|$<>\n\r\\") {
		return false
	}
	// URL http(s) lengkap (untuk tool http) -> validasi lewat parser URL.
	if strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://") {
		u, err := url.Parse(target)
		return err == nil && u.Host != "" && hostRe.MatchString(u.Hostname())
	}
	if hostPort.MatchString(target) {
		return true
	}
	return hostRe.MatchString(target)
}

func splitHostPort(target string, defPort string) (string, string) {
	if m := hostPort.FindStringSubmatch(target); m != nil {
		return m[1], m[2]
	}
	return target, defPort
}

// Tools mengembalikan definisi tool untuk function-calling LLM.
func (r *Runner) Tools() []llm.Tool {
	mk := func(name, desc string, props map[string]any, required ...string) llm.Tool {
		schema := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			schema["required"] = required
		}
		return llm.Tool{Type: "function", Function: llm.Function{Name: name, Description: desc, Parameters: schema}}
	}
	target := map[string]any{
		"type":        "string",
		"description": "hostname, IP, atau host:port. Contoh: 8.8.8.8, google.com, 10.10.10.1:1812",
	}
	return []llm.Tool{
		mk("ping", "ICMP ping 4 paket ke target; mengembalikan RTT rata-rata dan packet loss.", map[string]any{"target": target}, "target"),
		mk("traceroute", "Traceroute (tracert -d, maks 15 hop) untuk melihat jalur dan lokasi loss.", map[string]any{"target": target}, "target"),
		mk("dns", "Resolusi DNS untuk sebuah nama; mengembalikan A/AAAA dan waktu resolusi.", map[string]any{"name": target}, "name"),
		mk("tcp", "Uji koneksi TCP ke host:port (default port 80) dan ukur waktu handshake.", map[string]any{"target": target}, "target"),
		mk("http", "Ambil HTTP GET ke URL/host; mengembalikan status code, waktu respons, dan header penting.", map[string]any{"target": target}, "target"),
		mk("radius", "Probe liveness server RADIUS (UDP 1812) dengan Access-Request minimal; mendeteksi server hidup/mati untuk ISP.", map[string]any{"target": target, "user": map[string]any{"type": "string", "description": "username untuk Access-Request (opsional)"}}, "target"),
		mk("interface", "Tampilkan konfigurasi interface jaringan lokal (ipconfig) untuk cek IP/gateway/DNS.", map[string]any{}),
		mk("service", "Cek status Windows service (sc query) mis. FreeRADIUS, nginx, postgres.", map[string]any{"name": map[string]any{"type": "string", "description": "nama service Windows"}}, "name"),
		mk("system", "Ringkasan kesehatan host NOC: CPU/RAM proses ini, jumlah koneksi netstat, load.", map[string]any{}),
	}
}

// Run mengeksekusi satu probe. Selalu mengembalikan Result (Err diisi bila gagal).
func (r *Runner) Run(ctx context.Context, tool, target string) (res Result) {
	start := time.Now()
	res = Result{Tool: tool, Target: target}
	defer func() { res.DurationMS = time.Since(start).Milliseconds() }()

	if tool != "interface" && tool != "system" {
		if !Allowed(target) {
			res.Err = "target tidak valid / ditolak oleh guard"
			return res
		}
	}

	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()

	set := func(out string, err error) {
		res.Output = out
		if err != nil {
			res.Err = err.Error()
		}
	}

	switch tool {
	case "ping":
		set(r.ping(ctx, target))
	case "traceroute":
		set(r.traceroute(ctx, target))
	case "dns":
		set(r.dns(ctx, target))
	case "tcp":
		set(r.tcpProbe(ctx, target))
	case "http":
		set(r.httpProbe(ctx, target))
	case "radius":
		set(r.radiusProbe(ctx, target, ""))
	case "interface":
		set(r.ipconfig(ctx))
	case "service":
		set(r.service(ctx, target))
	case "system":
		set(r.system(ctx))
	default:
		res.Err = "tool tidak dikenal: " + tool
	}
	res.OK = res.Err == "" && strings.TrimSpace(res.Output) != ""
	return res
}

// ---- tools ----

func (r *Runner) ping(ctx context.Context, target string) (string, error) {
	args := []string{"-n", "4", "-w", "2000", target}
	if runtime.GOOS != "windows" {
		args = []string{"-c", "4", "-W", "2", target}
	}
	out, err := runCmd(ctx, "ping", args...)
	return trimOut(out, 4000), err
}

func (r *Runner) traceroute(ctx context.Context, target string) (string, error) {
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"-d", "-h", "15", "-w", "1000", target}
	} else {
		args = []string{"-n", "-m", "15", "-w", "1", target}
	}
	bin := "tracert"
	if runtime.GOOS != "windows" {
		bin = "traceroute"
	}
	out, err := runCmd(ctx, bin, args...)
	return trimOut(out, 6000), err
}

func (r *Runner) dns(ctx context.Context, name string) (string, error) {
	start := time.Now()
	addrs, err := net.DefaultResolver.LookupHost(ctx, name)
	el := time.Since(start)
	if err != nil {
		return "", fmt.Errorf("resolusi gagal setelah %dms: %v", el.Milliseconds(), err)
	}
	return fmt.Sprintf("lookup %s -> %s (dalam %dms)", name, strings.Join(addrs, ", "), el.Milliseconds()), nil
}

func (r *Runner) tcpProbe(ctx context.Context, target string) (string, error) {
	host, port := splitHostPort(target, "80")
	addr := net.JoinHostPort(host, port)
	start := time.Now()
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", addr)
	el := time.Since(start)
	if err != nil {
		return fmt.Sprintf("TCP %s GAGAL setelah %dms: %v", addr, el.Milliseconds(), err), nil
	}
	defer conn.Close()
	return fmt.Sprintf("TCP %s OK, handshake %dms, local=%s", addr, el.Milliseconds(), conn.LocalAddr()), nil
}

func (r *Runner) httpProbe(ctx context.Context, target string) (string, error) {
	url := target
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ai-noc-go/1.0 (+diagnostic)")
	start := time.Now()
	resp, err := (&http.Client{Timeout: r.Timeout}).Do(req)
	el := time.Since(start)
	if err != nil {
		return fmt.Sprintf("HTTP GET %s GAGAL setelah %dms: %v", url, el.Milliseconds(), err), nil
	}
	defer resp.Body.Close()
	hdr := []string{}
	for _, h := range []string{"Server", "Content-Type", "Cache-Control", "Cf-Cache-Status"} {
		if v := resp.Header.Get(h); v != "" {
			hdr = append(hdr, h+"="+v)
		}
	}
	return fmt.Sprintf("HTTP GET %s -> %d dalam %dms | %s", url, resp.StatusCode, el.Milliseconds(), strings.Join(hdr, " ; ")), nil
}

func (r *Runner) radiusProbe(ctx context.Context, target, user string) (string, error) {
	host, port := splitHostPort(target, "1812")
	if user == "" {
		user = "noc-probe"
	}
	pkt := buildAccessRequest(user)
	addr, err := net.ResolveUDPAddr("udp", net.JoinHostPort(host, port))
	if err != nil {
		return "", err
	}
	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(r.Timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	start := time.Now()
	if _, err := conn.Write(pkt); err != nil {
		return "", err
	}
	buf := make([]byte, 4096)
	n, err := conn.Read(buf)
	el := time.Since(start)
	if err != nil {
		return fmt.Sprintf("RADIUS %s:%s TIDAK MERESPONS dalam %dms (server mati / port tertutup / difilter)", host, port, el.Milliseconds()), nil
	}
	if n < 4 {
		return fmt.Sprintf("RADIUS %s:%s merespons %d byte (tidak valid)", host, port, n), nil
	}
	code := buf[0]
	name := map[byte]string{2: "Access-Accept", 3: "Access-Reject", 11: "Access-Challenge"}[code]
	if name == "" {
		name = "code=" + strconv.Itoa(int(code))
	}
	verdict := "SERVER HIDUP"
	if code == 3 {
		verdict = "SERVER HIDUP (kredensial uji ditolak — normal untuk probe tanpa secret)"
	}
	return fmt.Sprintf("RADIUS %s:%s -> %s dalam %dms — %s", host, port, name, el.Milliseconds(), verdict), nil
}

// buildAccessRequest membuat paket Access-Request (RFC 2865) dengan User-Name saja.
func buildAccessRequest(user string) []byte {
	pkt := make([]byte, 0, 64)
	pkt = append(pkt, 1) // Code: Access-Request
	pkt = append(pkt, 0) // Identifier (diisi nanti)
	pkt = append(pkt, 0, 0)
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	pkt = append(pkt, auth...)
	// Attribute 1 = User-Name
	pkt = append(pkt, 1, byte(2+len(user)))
	pkt = append(pkt, []byte(user)...)
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	return pkt
}

func (r *Runner) ipconfig(ctx context.Context) (string, error) {
	bin, args := "ipconfig", []string{"/all"}
	if runtime.GOOS != "windows" {
		bin, args = "ip", []string{"addr"}
	}
	out, err := runCmd(ctx, bin, args...)
	if err != nil {
		return "", err
	}
	return trimOut(condenseIPConfig(out), 4000), nil
}

func (r *Runner) service(ctx context.Context, name string) (string, error) {
	bin, args := "sc", []string{"query", name}
	if runtime.GOOS != "windows" {
		bin, args = "systemctl", []string{"is-active", name}
	}
	out, err := runCmd(ctx, bin, args...)
	return trimOut(out, 2000), err
}

func (r *Runner) system(ctx context.Context) (string, error) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	out, _ := runCmd(ctx, "netstat", "-an")
	established, listen := 0, 0
	for _, line := range strings.Split(out, "\n") {
		l := strings.ToUpper(line)
		if strings.Contains(l, "ESTABLISHED") {
			established++
		} else if strings.Contains(l, "LISTENING") || strings.Contains(l, "LISTEN") {
			listen++
		}
	}
	return fmt.Sprintf("host=%s/%s goroutines=%d heap=%.1fMB cpu=%d | netstat: established=%d listening=%d | uptime_proses_detak=%d",
		runtime.GOOS, runtime.GOARCH, runtime.NumGoroutine(), float64(ms.HeapAlloc)/1048576, runtime.NumCPU(),
		established, listen, time.Now().Unix()%100000), nil
}

// ---- helpers ----

func runCmd(ctx context.Context, bin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return "", fmt.Errorf("%s: %v", bin, err)
	}
	if err != nil {
		// ping/tracert mengembalikan exit code non-nol saat target tak terjangkau,
		// tapi outputnya tetap bukti yang berguna.
		return text, nil
	}
	return text, nil
}

func trimOut(s string, max int) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n"))
	if len(s) > max {
		return s[:max] + "\n... (dipotong)"
	}
	return s
}

// condenseIPConfig meringkas ipconfig /all agar hemat token LLM.
func condenseIPConfig(s string) string {
	keep := []string{"adapter ", "IPv4", "Subnet", "Default Gateway", "DNS Servers", "Description", "Physical Address", "Media State", "DHCP Enabled"}
	var b strings.Builder
	for _, line := range strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		for _, k := range keep {
			if strings.Contains(t, k) {
				b.WriteString(t)
				b.WriteString("\n")
				break
			}
		}
	}
	return b.String()
}
