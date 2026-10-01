package wa

import (
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	list := []string{"628123456789", "628999888777"}

	allow := []string{
		"628123456789", "628123456789@s.whatsapp.net",
		"628999888777@c.us", " 628123456789 ",
	}
	for _, id := range allow {
		if !Allowed(list, id) {
			t.Errorf("Allowed(%q) = false, seharusnya true", id)
		}
	}

	deny := []string{"628000000000", "628123456780", "", "6281234567890"}
	for _, id := range deny {
		if Allowed(list, id) {
			t.Errorf("Allowed(%q) = true, seharusnya false", id)
		}
	}

	// Daftar kosong = mode dev, izinkan semua (non-kosong).
	if !Allowed(nil, "628111222333") {
		t.Error("allowlist kosong seharusnya mengizinkan semua")
	}
}

func TestNormalize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"628123456789@s.whatsapp.net", "628123456789"},
		{"628123456789@c.us", "628123456789"},
		{"+62 812-3456-789", "628123456789"},
		{"120363000000000000@g.us", "120363000000000000"},
		{"", ""},
	}
	for _, c := range cases {
		if got := normalize(c.in); got != c.want {
			t.Errorf("normalize(%q) = %q, mau %q", c.in, got, c.want)
		}
	}
}

func TestParseAllowlist(t *testing.T) {
	got := ParseAllowlist(" 628111 , 628222,,628333 ")
	if len(got) != 3 || got[0] != "628111" || got[2] != "628333" {
		t.Errorf("ParseAllowlist = %#v", got)
	}
	if len(ParseAllowlist("")) != 0 {
		t.Error("ParseAllowlist kosong harus menghasilkan slice kosong")
	}
}

// Identity mengutamakan SENDER (nomor telepon hasil resolusi gateway), bukan
// chat_id. WhatsApp kini mengirim sebagian chat sebagai @lid; memakai chat_id
// membuat nomor tidak cocok dengan allowlist sehingga pesan pelanggan ditolak.
func TestIdentityUtamakanSender(t *testing.T) {
	m := InboundMessage{ChatID: "628111@s.whatsapp.net", Sender: "628111"}
	if m.Identity() != "628111" {
		t.Errorf("Identity = %q, mau %q (sender)", m.Identity(), "628111")
	}
	m2 := InboundMessage{Sender: "628222"}
	if m2.Identity() != "628222" {
		t.Errorf("Identity = %q", m2.Identity())
	}
}

// Pesan grup harus terdeteksi dari chat_id maupun sender, dan TIDAK dianggap
// grup hanya karena kebetulan mengandung "g" atau "us".
func TestIsGroup(t *testing.T) {
	grup := [][2]string{
		{"120363000000000000@g.us", "628111222333"},
		{"120363000000000000@g.us", "628111222333@g.us"},
		{"123@g.us", ""},
		{"", "123@g.us"},
	}
	for _, g := range grup {
		if !IsGroup(g[0], g[1]) {
			t.Errorf("IsGroup(%q, %q) = false, seharusnya true", g[0], g[1])
		}
	}

	pribadi := [][2]string{
		{"628123456789@s.whatsapp.net", "628123456789@s.whatsapp.net"},
		{"628123456789", "628123456789"},
		{"628123456789@s.whatsapp.net", ""},
		{"status@broadcast", "628123456789"},
		{"", ""},
	}
	for _, p := range pribadi {
		if IsGroup(p[0], p[1]) {
			t.Errorf("IsGroup(%q, %q) = true, seharusnya false", p[0], p[1])
		}
	}
}

func TestIsConnected(t *testing.T) {
	yes := []map[string]any{
		{"status": "connected"}, {"status": "CONNECTED"},
		{"state": "open"}, {"connected": true},
		{"session_status": "authenticated"},
	}
	for _, m := range yes {
		if !isConnected(m) {
			t.Errorf("isConnected(%v) = false, seharusnya true", m)
		}
	}
	no := []map[string]any{
		{}, {"status": "disconnected"}, {"status": "connecting"},
		{"connected": false}, {"state": "close"},
	}
	for _, m := range no {
		if isConnected(m) {
			t.Errorf("isConnected(%v) = true, seharusnya false", m)
		}
	}
}

func TestFormatReport(t *testing.T) {
	// Format baru: balasan manusiawi pelanggan dulu, lalu ringkasan teknis ringkas.
	answer := "BALASAN: Saya sudah cek, Pak. Koneksi dari kami normal, kendalanya kemungkinan\n" +
		"di alat di rumah. Coba cabut pasang ONT ya.\n\n" +
		"VERDICT: DEGRADASI\nKEYAKINAN: 78\nAKAR_MASALAH: packet loss di uplink\n" +
		"BUKTI: ping 8.8.8.8 loss 12%"
	msg := FormatReport(ReportView{
		Target: "8.8.8.8", Verdict: "DEGRADASI", Confidence: 78,
		Engine: "llm", ElapsedMS: 34226, Answer: answer,
		Balasan: "Saya sudah cek, Pak. Koneksi dari kami normal, kendalanya kemungkinan\n" +
			"di alat di rumah. Coba cabut pasang ONT ya.",
	})

	// Balasan pelanggan harus muncul dan mendahului ringkasan teknis.
	if !strings.Contains(msg, "Saya sudah cek, Pak") {
		t.Errorf("balasan pelanggan hilang:\n%s", msg)
	}
	// Ringkasan teknis tetap ada untuk teknisi.
	for _, want := range []string{"DEGRADASI", "78%", "Penyebab: packet loss di uplink"} {
		if !strings.Contains(msg, want) {
			t.Errorf("ringkasan teknis kehilangan %q:\n%s", want, msg)
		}
	}
	// Label mentah TIDAK boleh terlihat pelanggan.
	for _, jangan := range []string{"BALASAN:", "VERDICT:", "BUKTI:", "KEYAKINAN:"} {
		if strings.Contains(msg, jangan) {
			t.Errorf("label teknis %q bocor ke pelanggan:\n%s", jangan, msg)
		}
	}
	// Header kaku lama sudah tidak dipakai.
	if strings.Contains(msg, "Laporan Diagnosis") {
		t.Errorf("header kaku masih dipakai:\n%s", msg)
	}
	if strings.Contains(msg, "**") {
		t.Errorf("markdown ** belum dikonversi:\n%s", msg)
	}
	if len(msg) > maxWALen {
		t.Errorf("pesan %d karakter melebihi batas", len(msg))
	}
}

func TestFormatReportChat(t *testing.T) {
	// Balasan obrolan harus dikirim apa adanya: tanpa header laporan, tanpa
	// status "TIDAK DIKETAHUI", tanpa metrik mesin.
	msg := FormatReport(ReportView{
		IsChat: true, Verdict: "TIDAK DIKETAHUI", Engine: "llm (tanpa pengecekan)",
		Balasan: "Halo! Ada yang bisa saya bantu terkait kendala jaringan Anda?",
	})
	if strings.Contains(msg, "Laporan Diagnosis") {
		t.Errorf("balasan chat tidak boleh berheader laporan:\n%s", msg)
	}
	if strings.Contains(msg, "TIDAK DIKETAHUI") || strings.Contains(msg, "Target:") {
		t.Errorf("balasan chat tidak boleh memuat status/target:\n%s", msg)
	}
	if !strings.Contains(msg, "Halo! Ada yang bisa saya bantu") {
		t.Errorf("isi balasan hilang:\n%s", msg)
	}
}

func TestFormatReportTruncates(t *testing.T) {
	long := strings.Repeat("gangguan pada segmen uplink. ", 400)
	msg := FormatReport(ReportView{Target: "x", Verdict: "GANGGUAN", Answer: long})
	if len(msg) > maxWALen+80 {
		t.Errorf("pesan tidak dipotong: %d karakter", len(msg))
	}
	if !strings.Contains(msg, "dipotong") {
		t.Error("pesan panjang harus diberi penanda dipotong")
	}
}

func TestVerdictEmoji(t *testing.T) {
	cases := map[string]string{"SEHAT": "✅", "sehat": "✅", "DEGRADASI": "⚠️", "GANGGUAN": "🔴", "": "❔"}
	for in, want := range cases {
		if got := VerdictEmoji(in); got != want {
			t.Errorf("VerdictEmoji(%q) = %q, mau %q", in, got, want)
		}
	}
}

// Client tanpa BaseURL harus menolak dengan jelas, bukan panic atau kirim ke mana pun.
func TestSendWithoutGateway(t *testing.T) {
	c := New("", 5)
	if c.Enabled() {
		t.Error("Enabled() = true tanpa base URL")
	}
	if _, err := c.Send(nil, "628111", "halo"); err == nil {
		t.Error("Send() harus gagal saat gateway belum dikonfigurasi")
	}
	c2 := New("http://127.0.0.1:3001", 5)
	if _, err := c2.Send(nil, "", "halo"); err == nil {
		t.Error("Send() harus menolak tujuan kosong")
	}
	if _, err := c2.Send(nil, "628111", "   "); err == nil {
		t.Error("Send() harus menolak pesan kosong")
	}
}

// Identity harus memakai nomor telepon, bukan @lid. Bug nyata: pesan masuk
// dengan chat_id=111111111111111@lid (LID) dan sender=628111222333 (nomor
// hasil resolusi gateway). Memakai ChatID membuat nomor tidak cocok dengan
// allowlist sehingga pesan pelanggan ikut ditolak.
func TestIdentityUtamakanNomorBukanLID(t *testing.T) {
	m := InboundMessage{
		ChatID: "111111111111111@lid",
		Sender: "628111222333",
	}
	if got := m.Identity(); got != "628111222333" {
		t.Errorf("Identity()=%q, mau nomor telepon 628111222333 (bukan LID)", got)
	}
}

// Pesan dari nomor biasa (tanpa LID) tetap memakai sender.
func TestIdentityNomorBiasa(t *testing.T) {
	m := InboundMessage{ChatID: "628111222333@s.whatsapp.net", Sender: "628111222333"}
	if got := m.Identity(); got != "628111222333" {
		t.Errorf("Identity()=%q, mau 628111222333", got)
	}
}

// Bila sender kosong, pakai chat_id selama bukan LID.
func TestIdentityFallbackChatID(t *testing.T) {
	m := InboundMessage{ChatID: "628999888777@s.whatsapp.net"}
	if got := m.Identity(); got != "628999888777@s.whatsapp.net" {
		t.Errorf("Identity()=%q, mau chat_id", got)
	}
}

// Bila keduanya LID, kembalikan apa adanya supaya tetap tercatat di log
// (jangan mengembalikan string kosong yang membuat pesan tak bisa dilacak).
func TestIdentityKeduanyaLID(t *testing.T) {
	m := InboundMessage{ChatID: "222222222222222@lid", Sender: "222222222222222@lid"}
	if got := m.Identity(); got == "" {
		t.Error("Identity() kosong; harus mengembalikan LID apa adanya")
	}
}

func TestIsLID(t *testing.T) {
	cases := map[string]bool{
		"111111111111111@lid":         true,
		"222222222222222@LID":         true, // huruf besar tetap dikenali
		"628123456789@s.whatsapp.net": false,
		"628123456789":                false,
		"120363000000000000@g.us":     false,
		"":                            false,
	}
	for in, mau := range cases {
		if got := IsLID(in); got != mau {
			t.Errorf("IsLID(%q)=%v, mau %v", in, got, mau)
		}
	}
}

// Allowlist harus cocok dengan nomor hasil resolusi, walau chat datang @lid.
func TestAllowlistCocokDenganNomorHasilResolusi(t *testing.T) {
	allow := []string{"628111222333"}
	m := InboundMessage{ChatID: "111111111111111@lid", Sender: "628111222333"}
	if !Allowed(allow, m.Identity()) {
		t.Error("pesan dari nomor terdaftar ditolak karena identitas memakai LID")
	}
}
