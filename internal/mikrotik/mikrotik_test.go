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

// TestToolNames hanya read-only.
func TestToolNames(t *testing.T) {
	a := New(Config{Host: "x", User: "y"})
	if a.Domain() != "mikrotik" {
		t.Errorf("Domain = %q, mau mikrotik", a.Domain())
	}
	names := a.ToolNames()
	if len(names) != 1 || names[0] != "mikrotik.get_pppoe_status" {
		t.Errorf("ToolNames = %v, mau [mikrotik.get_pppoe_status]", names)
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
