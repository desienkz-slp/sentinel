package wa

import (
	"fmt"
	"regexp"
	"strings"
)

// ReportView adalah data minimal yang dibutuhkan untuk menyusun pesan WhatsApp.
// Sengaja tidak memakai tipe agent.Report agar paket ini tetap bebas ketergantungan.
type ReportView struct {
	Target     string
	Verdict    string
	Confidence float64
	Engine     string
	ElapsedMS  int64
	Escalated  bool
	Answer     string
	// IsChat menandai balasan tanpa pengecekan (sapaan, pertanyaan umum, minta
	// perjelas). Pesan seperti ini TIDAK boleh dibungkus sebagai laporan diagnosis
	// dengan status "TIDAK DIKETAHUI" — cukup dikirim sebagai obrolan biasa.
	IsChat bool
}

const maxWALen = 3500 // batas aman sebelum WhatsApp memotong pesan

var (
	labelRe   = regexp.MustCompile(`(?im)^\s*(VERDICT|KEYAKINAN|AKAR_MASALAH|BUKTI|REKOMENDASI)\s*:\s*`)
	boldStrip = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
)

// VerdictEmoji memetakan verdict ke indikator visual.
func VerdictEmoji(v string) string {
	switch strings.ToUpper(strings.TrimSpace(v)) {
	case "SEHAT":
		return "✅"
	case "DEGRADASI":
		return "⚠️"
	case "GANGGUAN":
		return "🔴"
	default:
		return "❔"
	}
}

// FormatReport menyusun pesan laporan diagnosis untuk WhatsApp.
func FormatReport(v ReportView) string {
	// Balasan obrolan (tanpa pengecekan) dikirim apa adanya — tanpa header
	// laporan dan tanpa status, karena memang bukan hasil diagnosis.
	if v.IsChat {
		return truncateWALen(strings.TrimSpace(v.Answer))
	}

	var b strings.Builder

	verdict := strings.ToUpper(strings.TrimSpace(v.Verdict))
	if verdict == "" {
		verdict = "TIDAK DIKETAHUI"
	}

	b.WriteString("*🛰️ NOC Sentinel — Laporan Diagnosis*\n\n")
	fmt.Fprintf(&b, "*Target:* %s\n", orDash(v.Target))
	fmt.Fprintf(&b, "*Status:* %s %s\n", VerdictEmoji(verdict), verdict)
	if v.Confidence > 0 {
		fmt.Fprintf(&b, "*Keyakinan:* %.0f%%\n", v.Confidence)
	}
	fmt.Fprintf(&b, "*Mesin:* %s (%.1fs)\n", orDash(v.Engine), float64(v.ElapsedMS)/1000)
	if v.Escalated {
		b.WriteString("*Catatan:* analisis dieskalasi ke Codex CLI\n")
	}

	if body := formatBody(v.Answer); body != "" {
		b.WriteString("\n" + body)
	}

	out := strings.TrimSpace(b.String())
	return truncateWALen(out)
}

// truncateWALen memotong pesan agar tidak melewati batas aman WhatsApp.
func truncateWALen(s string) string {
	if len(s) > maxWALen {
		return s[:maxWALen] + "\n\n_… pesan dipotong, buka dashboard untuk detail._"
	}
	return s
}

// formatBody mengubah jawaban terstruktur model menjadi teks ramah WhatsApp.
// Baris "LABEL: isi" menjadi "*LABEL:* isi"; **tebal** markdown jadi *tebal*.
func formatBody(answer string) string {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return ""
	}
	answer = boldStrip.ReplaceAllString(answer, "*$1*")
	lines := strings.Split(answer, "\n")
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			out = append(out, "")
			continue
		}
		if m := labelRe.FindStringSubmatch(t); m != nil {
			label := strings.ToUpper(m[1])
			rest := strings.TrimSpace(t[len(m[0]):])
			out = append(out, fmt.Sprintf("*%s:* %s", label, rest))
			continue
		}
		out = append(out, t)
	}
	// Rapatkan baris kosong beruntun.
	var b strings.Builder
	blank := false
	for _, l := range out {
		if l == "" {
			if blank {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		b.WriteString(l + "\n")
	}
	return strings.TrimSpace(b.String())
}

// FormatReplyPendek dipakai untuk balasan cepat (mis. pesan ditolak allowlist).
func FormatReplyPendek(msg string) string {
	return "*🛰️ NOC Sentinel*\n\n" + strings.TrimSpace(msg)
}

// FormatProgress dipakai saat diagnosis panjang supaya pengirim tahu sedang diproses.
func FormatProgress(query string) string {
	q := strings.TrimSpace(query)
	if len(q) > 120 {
		q = q[:120] + "…"
	}
	return fmt.Sprintf("*🛰️ NOC Sentinel*\n\nSedang menganalisis: _%s_\nMohon tunggu, proses diagnosis 10–60 detik.", q)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}
