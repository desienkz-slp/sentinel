package agent

import "testing"

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

func TestExtractTarget(t *testing.T) {
	cases := []struct{ in, want string }{
		{"cek ke 8.8.8.8 dong", "8.8.8.8"},
		{"internet lambat, target google.com", "google.com"},
		{"tolong cek pelanggan.net:443", "pelanggan.net:443"},
		{"tidak ada target di sini", ""},
		{"ping -c 4 8.8.8.8", "8.8.8.8"}, // flag diabaikan
	}
	for _, c := range cases {
		if got := extractTarget(c.in); got != c.want {
			t.Errorf("extractTarget(%q) = %q, mau %q", c.in, got, c.want)
		}
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
