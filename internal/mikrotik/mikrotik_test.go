package mikrotik

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
)

// mockRouterOS menjalankan server API native RouterOS di port acak untuk tes.
// Menangani login plaintext (6.43+) dan satu command.
func mockRouterOS(t *testing.T, user, pass string, respond func(cmd string) [][]string) (addr string, stop func()) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go handleMockConn(t, conn, user, pass, respond)
		}
	}()
	return ln.Addr().String(), func() { ln.Close() }
}

func handleMockConn(t *testing.T, conn net.Conn, user, pass string, respond func(cmd string) [][]string) {
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := bufio.NewWriter(conn)

	// Login sentence pertama.
	sentence, err := readSentence(r)
	if err != nil {
		return
	}
	gotUser, gotPass := "", ""
	for _, wd := range sentence {
		if strings.HasPrefix(wd, "=name=") {
			gotUser = strings.TrimPrefix(wd, "=name=")
		}
		if strings.HasPrefix(wd, "=password=") {
			gotPass = strings.TrimPrefix(wd, "=password=")
		}
	}
	if gotUser != user || gotPass != pass {
		_ = writeSentence(w, []string{"!trap", "=message=invalid user name or password (6)"})
		return
	}
	_ = writeSentence(w, []string{"!done"})

	// Baca command selanjutnya.
	sentence, err = readSentence(r)
	if err != nil {
		return
	}
	cmd := sentence[0]
	for _, row := range respond(cmd) {
		words := []string{"!re"}
		words = append(words, row...)
		_ = writeSentence(w, words)
	}
	_ = writeSentence(w, []string{"!done"})
}

// TestLoginAndQuery: login benar + baca /system/resource.
func TestLoginAndQuery(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		if cmd == "/system/resource/print" {
			return [][]string{{"=version=7.14.2", "=board-name=CCR1036", "=uptime=3d", "=cpu-load=2"}}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	d, err := a.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping error: %v", err)
	}
	if d.Version != "7.14.2" || d.BoardName != "CCR1036" {
		t.Errorf("Ping = %+v, mau version 7.14.2 board CCR1036", d)
	}
}

// TestLoginWrongPassword: kredensial salah -> error jelas.
func TestLoginWrongPassword(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "benar", func(cmd string) [][]string { return nil })
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "salah"})
	_, err := a.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping harusnya error saat password salah")
	}
	if !strings.Contains(err.Error(), "invalid user name or password") {
		t.Errorf("error = %q, mau memuat pesan auth gagal", err.Error())
	}
}

// TestGetPPPoEStatus: cocokkan username persis/contains.
func TestGetPPPoEStatus(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		if cmd == "/ppp/active/print" {
			return [][]string{
				{"=name=628123456789@netlayer", "=service=pppoe", "=caller-id=AA:BB", "=address=10.0.0.5", "=uptime=1h2m"},
				{"=name=other-user", "=service=pppoe", "=address=10.0.0.6", "=uptime=2h"},
			}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{"identity": "628123456789"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "628123456789@netlayer") {
		t.Errorf("Text = %q, mau memuat username cocok", out.Text)
	}
	if strings.Contains(out.Text, "other-user") {
		t.Errorf("Text = %q, tidak boleh memuat username tidak cocok", out.Text)
	}
}

// TestGetPPPoEStatusNoMatch: tidak ada sesi -> "TIDAK aktif".
func TestGetPPPoEStatusNoMatch(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		return [][]string{}
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{"identity": "nobody"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "TIDAK aktif") {
		t.Errorf("Text = %q, mau memuat 'TIDAK aktif'", out.Text)
	}
}

// TestPortDefault: port 0 -> 8728 (plaintext) / 8729 (TLS).
func TestPortDefault(t *testing.T) {
	if got := New(Config{Host: "x", User: "y"}).port(); got != 8728 {
		t.Errorf("port default plaintext = %d, mau 8728", got)
	}
	if got := New(Config{Host: "x", User: "y", TLS: true}).port(); got != 8729 {
		t.Errorf("port default TLS = %d, mau 8729", got)
	}
	if got := New(Config{Host: "x", User: "y", Port: 8118}).port(); got != 8118 {
		t.Errorf("port custom = %d, mau 8118", got)
	}
}

// TestConfigured: butuh host + user.
func TestConfigured(t *testing.T) {
	if New(Config{}).Configured() {
		t.Error("Configured = true untuk config kosong")
	}
	if New(Config{Host: "x"}).Configured() {
		t.Error("Configured = true tanpa user")
	}
	if !New(Config{Host: "x", User: "y"}).Configured() {
		t.Error("Configured = false padahal host+user ada")
	}
}

// TestToolNames hanya read-only (4 tool traffic/status).
func TestToolNames(t *testing.T) {
	a := New(Config{Host: "x", User: "y"})
	if a.Domain() != "mikrotik" {
		t.Errorf("Domain = %q, mau mikrotik", a.Domain())
	}
	names := a.ToolNames()
	want := []string{
		"mikrotik.get_pppoe_status",
		"mikrotik.get_interface_stats",
		"mikrotik.get_interface_live",
		"mikrotik.get_customer_traffic",
	}
	if len(names) != len(want) {
		t.Fatalf("ToolNames = %v, mau %v", names, want)
	}
	for i, w := range want {
		if names[i] != w {
			t.Errorf("ToolNames[%d] = %q, mau %q", i, names[i], w)
		}
	}
}

// ---- helper ----

func hostOf(addr string) string {
	h, _, _ := net.SplitHostPort(addr)
	return h
}

func portOf(addr string) int {
	_, p, _ := net.SplitHostPort(addr)
	n := 0
	for _, c := range p {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// TestGetInterfaceStats: parse rx/tx byte + humanBytes.
func TestGetInterfaceStats(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		if cmd == "/interface/print" {
			return [][]string{
				{"=name=ether1", "=type=ether", "=rx-byte=228405285520454", "=tx-byte=23184709199749"},
				{"=name=ether2", "=type=ether", "=rx-byte=27235253974210", "=tx-byte=211288494220234"},
			}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_interface_stats", map[string]any{})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "ether1") || !strings.Contains(out.Text, "ether2") {
		t.Errorf("Text = %q, mau memuat kedua interface", out.Text)
	}
	if !strings.Contains(out.Text, "TB") {
		t.Errorf("Text = %q, mau memuat ukuran terbaca (TB)", out.Text)
	}
}

// TestGetInterfaceLive: parse live bps.
func TestGetInterfaceLive(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		if cmd == "/interface/monitor-traffic" {
			return [][]string{{"=name=ether1", "=rx-bits-per-second=1531456376", "=tx-bits-per-second=125983088"}}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_interface_live", map[string]any{"interface": "ether1"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "Gbps") {
		t.Errorf("Text = %q, mau memuat 'Gbps' (1531456376 bps ~ 1.5 Gbps)", out.Text)
	}
}

// TestGetCustomerTraffic: parse simple queue name <pppoe-USER> + bytes/rate.
func TestGetCustomerTraffic(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		if cmd == "/queue/simple/print" {
			return [][]string{
				{"=name=<pppoe-pelanggan-satu>", "=bytes=43793175530/416066955503", "=rate=280200/7115056", "=max-limit=50000000/50000000"},
				{"=name=<pppoe-pelanggan-dua>", "=bytes=8958194383/59929609759", "=rate=0/0", "=max-limit=16000000/16000000"},
			}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_customer_traffic", map[string]any{"identity": "pelanggan-satu"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "pelanggan-satu") {
		t.Errorf("Text = %q, mau memuat pelanggan-satu", out.Text)
	}
	if strings.Contains(out.Text, "pelanggan-dua") {
		t.Errorf("Text = %q, tidak boleh memuat pelanggan-dua (filter pelanggan-satu)", out.Text)
	}
}

// TestHumanBytesAndBps: verifikasi format helper.
func TestHumanBytesAndBps(t *testing.T) {
	if got := humanBytes(1024); got != "1.0 KB" {
		t.Errorf("humanBytes(1024) = %q, mau 1.0 KB", got)
	}
	if got := humanBytes(1024 * 1024 * 1024 * 1024); got != "1.0 TB" {
		t.Errorf("humanBytes(1TB) = %q, mau 1.0 TB", got)
	}
	if got := humanBps(1000000000); got != "1.0 Gbps" {
		t.Errorf("humanBps(1e9) = %q, mau 1.0 Gbps", got)
	}
	if got := humanBps(50000000); got != "50.0 Mbps" {
		t.Errorf("humanBps(50e6) = %q, mau 50.0 Mbps", got)
	}
	if rx, tx := splitPair("280200/7115056"); rx != 280200 || tx != 7115056 {
		t.Errorf("splitPair = %d/%d, mau 280200/7115056", rx, tx)
	}
}
