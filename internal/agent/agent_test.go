package agent

import (
	"strings"
	"testing"

	"ainoc/internal/standard"
)

func TestParseVerdict(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		verdict string
		conf    float64
	}{
		{
			"lengkap",
			"VERDICT: DEGRADASI\nKEYAKINAN: 78\nAKAR_MASALAH: packet loss di uplink",
			"DEGRADASI", 78,
		},
		{
			"lowercase + markdown",
			"**verdict:** gangguan\nkeyakinan: 91%",
			"GANGGUAN", 91,
		},
		{
			"rasio 0-1 dikonversi",
			"VERDICT: SEHAT\nKEYAKINAN: 0.9",
			"SEHAT", 90,
		},
		{
			"tanpa verdict",
			"Hasil probe normal, tidak ada masalah.",
			"TIDAK DIKETAHUI", 0,
		},
		{
			"jawaban tanpa pengecekan (sapaan)",
			"Halo! Ada yang bisa saya bantu terkait jaringan Anda?",
			"TIDAK DIKETAHUI", 0,
		},
		{
			"minta perjelas",
			"Maaf, bisakah Anda jelaskan gangguan yang dialami?",
			"TIDAK DIKETAHUI", 0,
		},
		{
			"verdict di tengah kalimat",
			"Berdasarkan bukti, VERDICT: SEHAT dan KEYAKINAN: 88",
			"SEHAT", 88,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, v, conf := parseVerdict(c.in)
			if v != c.verdict {
				t.Errorf("verdict = %q, mau %q", v, c.verdict)
			}
			if conf != c.conf {
				t.Errorf("confidence = %v, mau %v", conf, c.conf)
			}
		})
	}
}

// probedTarget mengambil target dari langkah tool pertama — dipakai agar riwayat
// menampilkan alamat yang benar-benar dicek meski pengirim tidak menyebutkannya.
func TestLowConfidenceNeverAutomaticallyEscalatesToCodex(t *testing.T) {
	e := &Engine{Codex: nil}
	if e.shouldEscalate(Report{Verdict: "TIDAK DIKETAHUI", Confidence: 0}, nil) {
		t.Fatal("low-confidence report must enter human escalation, not Codex")
	}
}

func TestProbedTarget(t *testing.T) {
	if got := probedTarget(nil); got != "" {
		t.Errorf("probedTarget(nil) = %q, mau kosong", got)
	}
	steps := []Step{
		{Kind: "thought", Text: "menganalisis"},
		{Kind: "tool", Tool: "ping", Target: "192.168.1.1"},
		{Kind: "tool", Tool: "dns", Target: "google.com"},
	}
	if got := probedTarget(steps); got != "192.168.1.1" {
		t.Errorf("probedTarget = %q, mau 192.168.1.1", got)
	}
	// Langkah tanpa target (interface/system) harus dilewati, bukan dianggap target.
	noTarget := []Step{{Kind: "tool", Tool: "interface"}, {Kind: "tool", Tool: "ping", Target: "8.8.8.8"}}
	if got := probedTarget(noTarget); got != "8.8.8.8" {
		t.Errorf("probedTarget = %q, mau 8.8.8.8", got)
	}
}

func TestBuildCodexTask(t *testing.T) {
	rep := Report{
		Query:  "pelanggan lapor putus",
		Target: "8.8.8.8",
		Steps: []Step{
			{Kind: "thought", Text: "ini tidak boleh masuk bukti"},
			{Kind: "tool", Tool: "ping", Target: "8.8.8.8", OK: true, DurationMS: 3000,
				Output: "Reply from 8.8.8.8\nline kedua"},
		},
	}
	task := buildCodexTask(rep)
	for _, want := range []string{"pelanggan lapor putus", "8.8.8.8", "ping", "Reply from"} {
		if !contains(task, want) {
			t.Errorf("tugas Codex kehilangan %q", want)
		}
	}
	if contains(task, "tidak boleh masuk") {
		t.Error("tugas Codex memuat langkah thought, seharusnya hanya bukti tool")
	}
	// Output multi-baris harus diratakan agar prompt tetap terbaca.
	if contains(task, "Reply from 8.8.8.8\nline kedua") {
		t.Error("output tool tidak diratakan ke satu baris")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// PisahBalasan memisahkan teks pelanggan dari ringkasan teknis. Ini yang
// mencegah pelanggan menerima "VERDICT:"/"BUKTI:" mentah.
func TestPisahBalasan(t *testing.T) {
	answer := "BALASAN: Halo Pak, koneksi dari kami normal ya.\n" +
		"Coba cabut pasang ONT di rumah.\n\n" +
		"VERDICT: DEGRADASI\nKEYAKINAN: 78\nAKAR_MASALAH: packet loss di uplink\nBUKTI: ping loss 12%"

	balasan, teknis := PisahBalasan(answer)

	if !strings.Contains(balasan, "Halo Pak") || !strings.Contains(balasan, "cabut pasang ONT") {
		t.Errorf("balasan pelanggan tidak lengkap: %q", balasan)
	}
	// Label dan ringkasan teknis TIDAK boleh ikut ke pelanggan.
	for _, jangan := range []string{"BALASAN:", "VERDICT:", "KEYAKINAN:", "BUKTI:", "packet loss"} {
		if strings.Contains(balasan, jangan) {
			t.Errorf("balasan pelanggan memuat %q: %q", jangan, balasan)
		}
	}
	// Ringkasan teknis harus terpisah dan lengkap.
	for _, wajib := range []string{"VERDICT: DEGRADASI", "KEYAKINAN: 78", "packet loss"} {
		if !strings.Contains(teknis, wajib) {
			t.Errorf("ringkasan teknis kehilangan %q: %q", wajib, teknis)
		}
	}
}

// Tanpa format BALASAN, jawaban tetap aman: label teknis dibuang.
func TestPisahBalasanTanpaFormat(t *testing.T) {
	answer := "Koneksi dari kami normal ya Pak.\nVERDICT: SEHAT\nBUKTI: ping 28ms"
	balasan, teknis := PisahBalasan(answer)
	if strings.Contains(balasan, "VERDICT") || strings.Contains(balasan, "BUKTI") {
		t.Errorf("label teknis bocor: %q", balasan)
	}
	if !strings.Contains(balasan, "Koneksi dari kami normal") {
		t.Errorf("isi balasan hilang: %q", balasan)
	}
	if !strings.Contains(teknis, "VERDICT: SEHAT") {
		t.Errorf("ringkasan teknis hilang: %q", teknis)
	}
}

// Jawaban kosong tidak boleh panic.
func TestPisahBalasanKosong(t *testing.T) {
	b, tk := PisahBalasan("")
	if b != "" || tk != "" {
		t.Errorf("jawaban kosong -> (%q, %q), mau kosong", b, tk)
	}
}

// TestBlokKonteksPlaybookHanyaUntukKeluhan membuktikan playbook (urutan probe
// teknis dari pembelajaran) TIDAK disuntikkan untuk intent non-keluhan. Ini
// mencegah model membocorkan urutan probe (mis. "mikrotik.get_pppoe_status ->
// genieacs...") ke pelanggan pada sapaan/info — gerbang kode, bukan prompt.
func TestBlokKonteksPlaybookHanyaUntukKeluhan(t *testing.T) {
	playbook := []string{"mikrotik.get_pppoe_status", "genieacs.get_device_state"}

	// Intent INFO (mis. broadcast "STATUS USER") -> playbook TIDAK boleh muncul.
	k := KonteksAI{
		Klas:     HasilKlasifikasi{Intent: standard.IntentInfo},
		Playbook: playbook,
	}
	if got := k.blokKonteks(); strings.Contains(got, "mikrotik.get_pppoe_status") {
		t.Errorf("intent INFO: playbook bocor ke konteks: %q", got)
	}

	// Intent CHAT (sapaan) -> sama, tidak boleh.
	k.Klas.Intent = standard.IntentChat
	if got := k.blokKonteks(); strings.Contains(got, "mikrotik.get_pppoe_status") {
		t.Errorf("intent CHAT: playbook bocor ke konteks: %q", got)
	}

	// Intent COMPLAINT -> playbook WAJIB disuntikkan (berguna untuk diagnosis).
	k.Klas.Intent = standard.IntentComplaint
	if got := k.blokKonteks(); !strings.Contains(got, "mikrotik.get_pppoe_status") {
		t.Errorf("intent COMPLAINT: playbook hilang dari konteks: %q", got)
	}
}
