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
	return mockRouterOSWithSentence(t, user, pass, func(sentence []string) [][]string {
		return respond(sentence[0])
	})
}

func mockRouterOSWithSentence(t *testing.T, user, pass string, respond func(sentence []string) [][]string) (addr string, stop func()) {
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

func handleMockConn(t *testing.T, conn net.Conn, user, pass string, respond func(sentence []string) [][]string) {
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
	for _, row := range respond(sentence) {
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

// TestGetPPPoEStatusUsesExactIdentityFilter ensures this tool cannot list or
// prefix-search active PPPoE sessions.
func TestGetPPPoEStatusUsesExactIdentityFilter(t *testing.T) {
	var commands [][]string
	addr, stop := mockRouterOSWithSentence(t, "staff", "rahasia", func(sentence []string) [][]string {
		commands = append(commands, append([]string(nil), sentence...))
		if sentence[0] == "/ppp/active/print" {
			return [][]string{{"=name=628123456789@netlayer", "=service=pppoe", "=caller-id=AA:BB", "=address=10.0.0.5", "=uptime=1h2m"}}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{"identity": "628123456789@netlayer"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "628123456789@netlayer") {
		t.Errorf("Text = %q, mau memuat username cocok", out.Text)
	}
	if len(commands) != 1 || commands[0][0] != "/ppp/active/print" || !sentenceHas(commands[0], "=?name=628123456789@netlayer") {
		t.Errorf("commands = %v, mau satu lookup PPPoE exact tanpa daftar sesi", commands)
	}
	rows, ok := out.Data.([]map[string]string)
	if !ok || len(rows) != 1 || rows[0]["name"] != "628123456789@netlayer" {
		t.Errorf("Data = %#v, mau tepat satu sesi yang diminta", out.Data)
	}
}

func TestGetPPPoEStatusRequiresIdentityWithoutQuery(t *testing.T) {
	a := New(Config{Host: "127.0.0.1", Port: 1, User: "staff", Pass: "rahasia"})
	_, err := a.Invoke(context.Background(), "mikrotik.get_pppoe_status", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "butuh identity") {
		t.Fatalf("status tanpa identity error = %v, mau penolakan sebelum query", err)
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

// TestGetInterfaceStatsUsesExactInterfaceFilter ensures cumulative statistics
// are bounded to one requested interface at the RouterOS query layer.
func TestGetInterfaceStatsUsesExactInterfaceFilter(t *testing.T) {
	var commands [][]string
	addr, stop := mockRouterOSWithSentence(t, "staff", "rahasia", func(sentence []string) [][]string {
		commands = append(commands, append([]string(nil), sentence...))
		if sentence[0] == "/interface/print" {
			return [][]string{{"=name=ether1", "=type=ether", "=rx-byte=228405285520454", "=tx-byte=23184709199749"}}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_interface_stats", map[string]any{"interface": "ether1"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "ether1") || strings.Contains(out.Text, "ether2") {
		t.Errorf("Text = %q, mau hanya memuat interface yang diminta", out.Text)
	}
	if !strings.Contains(out.Text, "TB") {
		t.Errorf("Text = %q, mau memuat ukuran terbaca (TB)", out.Text)
	}
	if len(commands) != 1 || commands[0][0] != "/interface/print" || !sentenceHas(commands[0], "=?name=ether1") {
		t.Errorf("commands = %v, mau satu lookup interface exact tanpa daftar interface", commands)
	}
	rows, ok := out.Data.([]map[string]string)
	if !ok || len(rows) != 1 || rows[0]["name"] != "ether1" {
		t.Errorf("Data = %#v, mau tepat satu interface", out.Data)
	}
}

func TestGetInterfaceStatsRequiresInterfaceWithoutQuery(t *testing.T) {
	a := New(Config{Host: "127.0.0.1", Port: 1, User: "staff", Pass: "rahasia"})
	_, err := a.Invoke(context.Background(), "mikrotik.get_interface_stats", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "butuh interface") {
		t.Fatalf("stats tanpa interface error = %v, mau penolakan sebelum query", err)
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

func TestGetInterfaceLiveRejectsNonUniqueResponse(t *testing.T) {
	addr, stop := mockRouterOS(t, "staff", "rahasia", func(cmd string) [][]string {
		if cmd == "/interface/monitor-traffic" {
			return [][]string{
				{"=name=ether1", "=rx-bits-per-second=1", "=tx-bits-per-second=2"},
				{"=name=ether2", "=rx-bits-per-second=3", "=tx-bits-per-second=4"},
			}
		}
		return nil
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_interface_live", map[string]any{"interface": "ether1"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "tidak dapat di-resolve secara unik") || out.Data != nil {
		t.Errorf("non-unique live output = %+v, mau diblok tanpa data", out)
	}
}

// TestGetCustomerTrafficUsesBoundedPPPoEInterfaceTraffic proves customer traffic
// never reads queues: it resolves one exact PPPoE active session, then samples
// only that session's interface once.
func TestGetCustomerTrafficUsesBoundedPPPoEInterfaceTraffic(t *testing.T) {
	var commands [][]string
	addr, stop := mockRouterOSWithSentence(t, "staff", "rahasia", func(sentence []string) [][]string {
		commands = append(commands, append([]string(nil), sentence...))
		switch sentence[0] {
		case "/ppp/active/print":
			return [][]string{{"=name=pelanggan-satu", "=interface=pppoe-pelanggan-satu"}}
		case "/interface/monitor-traffic":
			return [][]string{{"=name=pppoe-pelanggan-satu", "=rx-bits-per-second=280200", "=tx-bits-per-second=7115056"}}
		default:
			return nil
		}
	})
	defer stop()

	a := New(Config{Host: hostOf(addr), Port: portOf(addr), User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_customer_traffic", map[string]any{"identity": "pelanggan-satu"})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "pppoe-pelanggan-satu") || !strings.Contains(out.Text, "7.1 Mbps") {
		t.Errorf("Text = %q, mau traffic interface PPPoE", out.Text)
	}
	if len(commands) != 2 || commands[0][0] != "/ppp/active/print" || commands[1][0] != "/interface/monitor-traffic" {
		t.Errorf("commands = %v, mau /ppp/active/print lalu /interface/monitor-traffic; queue dilarang", commands)
		return
	}
	if !sentenceHas(commands[0], "=?name=pelanggan-satu") {
		t.Errorf("PPPoE lookup = %v, mau filter server-side tepat ?name=pelanggan-satu", commands[0])
	}
	if !sentenceHas(commands[1], "=interface=pppoe-pelanggan-satu") || !sentenceHas(commands[1], "=once=") {
		t.Errorf("traffic read = %v, mau satu monitor untuk interface resolved dengan once", commands[1])
	}
}

func TestGetCustomerTrafficRequiresIdentity(t *testing.T) {
	a := New(Config{Host: "127.0.0.1", Port: 1, User: "staff", Pass: "rahasia"})
	out, err := a.Invoke(context.Background(), "mikrotik.get_customer_traffic", map[string]any{})
	if err != nil {
		t.Fatalf("Invoke error: %v", err)
	}
	if !strings.Contains(out.Text, "UNKNOWN") || out.Data != nil {
		t.Errorf("traffic without identity = %+v, mau UNKNOWN tanpa data", out)
	}
}

// TestGetCustomerTrafficRejectsAmbiguousSession ensures an unexpected non-unique
// response cannot cause an interface sample or expose either returned session.
func TestGetCustomerTrafficRejectsAmbiguousSession(t *testing.T) {
	var commands [][]string
	addr, stop := mockRouterOSWithSentence(t, "staff", "rahasia", func(sentence []string) [][]string {
		commands = append(commands, append([]string(nil), sentence...))
		if sentence[0] == "/ppp/active/print" {
			return [][]string{
				{"=name=pelanggan-satu", "=interface=pppoe-pelanggan-satu"},
				{"=name=pelanggan-satu", "=interface=pppoe-pelanggan-satu-dua"},
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
	if !strings.Contains(out.Text, "UNKNOWN") || out.Data != nil {
		t.Errorf("ambiguous traffic = %+v, mau UNKNOWN tanpa data", out)
	}
	if len(commands) != 1 || commands[0][0] != "/ppp/active/print" || !sentenceHas(commands[0], "=?name=pelanggan-satu") {
		t.Errorf("commands = %v, mau hanya PPPoE lookup exact tanpa monitor", commands)
	}
}

func sentenceHas(sentence []string, want string) bool {
	for _, word := range sentence {
		if word == want {
			return true
		}
	}
	return false
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
}
