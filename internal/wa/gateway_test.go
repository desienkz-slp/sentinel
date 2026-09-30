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

func TestIdentityPrefersChatID(t *testing.T) {
	m := InboundMessage{ChatID: "628111@s.whatsapp.net", Sender: "628111"}
	if m.Identity() != "628111@s.whatsapp.net" {
		t.Errorf("Identity = %q", m.Identity())
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
	answer := "VERDICT: DEGRADASI\nKEYAKINAN: 78\nAKAR_MASALAH: packet loss di uplink\nBUKTI:\n- ping 8.8.8.8 loss 12%\nREKOMENDASI: cek uplink ke IX"
	msg := FormatReport(ReportView{
		Target: "8.8.8.8", Verdict: "DEGRADASI", Confidence: 78,
		Engine: "llm", ElapsedMS: 34226, Answer: answer,
	})

	for _, want := range []string{
		"*Target:* 8.8.8.8", "DEGRADASI", "78%", "llm", "34.2s",
		"*VERDICT:* DEGRADASI", "*AKAR_MASALAH:* packet loss", "*REKOMENDASI:* cek uplink",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("pesan kehilangan %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "**") {
		t.Errorf("markdown ** belum dikonversi ke format WhatsApp:\n%s", msg)
	}
	if len(msg) > maxWALen {
		t.Errorf("pesan %d karakter melebihi batas", len(msg))
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
