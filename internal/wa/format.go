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
	// Balasan = teks untuk pelanggan (bahasa manusia), terpisah dari Answer
	// yang memuat ringkasan teknis lengkap.
	Balasan string
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

// FormatReport menyusun pesan WhatsApp: balasan manusiawi untuk pelanggan,
// lalu ringkasan teknis singkat di bawahnya (berguna untuk teknisi yang ikut
// membaca percakapan).
func FormatReport(v ReportView) string {
	// Balasan obrolan (tanpa pengecekan) dikirim apa adanya — tanpa header
	// laporan dan tanpa status, karena memang bukan hasil diagnosis.
	if v.IsChat {
		return truncateWALen(strings.TrimSpace(v.Balasan))
	}

	var b strings.Builder

	verdict := strings.ToUpper(strings.TrimSpace(v.Verdict))
	if verdict == "" {
		verdict = "TIDAK DIKETAHUI"
	}

	// 1) Balasan untuk pelanggan — ini bagian utama.
	balasan := strings.TrimSpace(v.Balasan)
	if balasan == "" {
		balasan = strings.TrimSpace(v.Answer)
	}
	if balasan != "" {
		b.WriteString(balasan)
	}

	// 2) Ringkasan teknis ringkas — dipisah garis supaya jelas bedanya.
	teknis := ringkasTeknis(v)
	if teknis != "" {
		if b.Len() > 0 {
			b.WriteString("\n\n———\n")
		}
		b.WriteString(teknis)
	}

	return truncateWALen(strings.TrimSpace(b.String()))
}

// ringkasTeknis menyusun ringkasan teknis singkat: status, keyakinan, dan
// akar masalah (bila ada di jawaban model). Sengaja TIDAK memuat seluruh BUKTI
// agar tidak membanjiri pelanggan.
func ringkasTeknis(v ReportView) string {
	verdict := strings.ToUpper(strings.TrimSpace(v.Verdict))
	if verdict == "" {
		verdict = "TIDAK DIKETAHUI"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", VerdictEmoji(verdict), verdict)
	if v.Confidence > 0 {
		fmt.Fprintf(&b, " · %.0f%%", v.Confidence)
	}
	if v.Escalated {
		b.WriteString(" · dianalisis lanjut")
	}
	if akar := akarMasalah(v.Answer); akar != "" {
		b.WriteString("\n" + akar)
	}
	return b.String()
}

// akarMasalah mengambil baris AKAR_MASALAH dari jawaban model (bila ada).
var akarRe = regexp.MustCompile(`(?im)^\s*AKAR_MASALAH\s*[:=]\s*(.+)$`)

func akarMasalah(answer string) string {
	if m := akarRe.FindStringSubmatch(answer); m != nil {
		return "Penyebab: " + strings.TrimSpace(m[1])
	}
	return ""
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
