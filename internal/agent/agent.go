// Package agent mengorkestrasi loop OODA: OBSERVE -> UNDERSTAND -> PLAN ->
// ACT (tool) -> VERIFY, dengan dukungan LLM lokal/9Router dan eskalaasi ke Codex CLI.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"ainoc/internal/codexbridge"
	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/llm"
)

type Step struct {
	Kind       string `json:"kind"` // thought | tool | answer | escalate | error
	Text       string `json:"text,omitempty"`
	Tool       string `json:"tool,omitempty"`
	Target     string `json:"target,omitempty"`
	Output     string `json:"output,omitempty"`
	OK         bool   `json:"ok,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

type Report struct {
	ID         string    `json:"id"`
	Query      string    `json:"query"`
	Target     string    `json:"target"`
	StartedAt  time.Time `json:"started_at"`
	Steps      []Step    `json:"steps"`
	Answer     string    `json:"answer"`
	Verdict    string    `json:"verdict"` // SEHAT | DEGRADASI | GANGGUAN | TIDAK DIKETAHUI
	Confidence float64   `json:"confidence"`
	Engine     string    `json:"engine"`
	ElapsedMS  int64     `json:"elapsed_ms"`
	Escalated  bool      `json:"escalated"`
	Escalation string    `json:"escalation,omitempty"`
	Error      string    `json:"error,omitempty"`
}

// Escalator adalah abstraksi Codex bridge (memudahkan pengujian).
type Escalator interface {
	Available() bool
	Run(ctx context.Context, task string) codexbridge.Result
}

type Engine struct {
	Cfg   *config.Config
	LLM   *llm.Client
	Diag  *diag.Runner
	Codex Escalator

	mu      sync.Mutex
	reports []Report
}

func New(cfg *config.Config, client *llm.Client, runner *diag.Runner, esc Escalator) *Engine {
	return &Engine{Cfg: cfg, LLM: client, Diag: runner, Codex: esc}
}

const systemPrompt = `Anda adalah "NOC Sentinel", asisten NOC (Network Operations Center) untuk ISP.
Anda menerima pesan apa pun dari pelanggan atau teknisi lewat WhatsApp.

CARA BERPIKIR — WAJIB BERTAHAP:

TAHAP 1 — PAHAMI DULU (jangan langsung cek).
Baca pesan dan tentukan: apa keluhannya, dan apakah perlu pengecekan teknis sama sekali.
  - Sapaan, terima kasih, atau pertanyaan umum -> JAWAB LANGSUNG. JANGAN pakai tool apa pun.
  - Pesan informasi / broadcast otomatis (laporan status sistem) -> akui singkat. JANGAN pakai tool.
  - Keluhan gangguan nyata -> lanjut ke TAHAP 2.
  - Target tidak jelas dan tidak bisa disimpulkan dari pesan -> TANYA BALIK ke pengirim.
    Jangan melakukan probe buta ke alamat yang tidak diminta.

TAHAP 2 — CEK SESUAI KELUHAN (hanya yang relevan).
Jalankan HANYA tool yang benar-benar menguji dugaan Anda atas keluhan itu.
  - JANGAN menjalankan probe seragam untuk semua pesan.
  - Cukup 1-3 probe. Begitu bukti cukup untuk menyimpulkan, BERHENTI.
  - Jangan mengulang kombinasi tool+target yang sudah dijalankan.
  - Pemetaan keluhan -> probe yang tepat:
      "lambat" / "lelet"          -> ping (latensi & loss), lalu http atau tcp
      "tidak bisa buka situs"     -> dns, lalu http
      "mati total" / "tidak konek"-> ping ke gateway lokal dulu, baru ping publik
      "putus nyambung" / "kedip"  -> ping dengan paket lebih banyak, lalu traceroute
      "wifi lemah" / perangkat    -> interface, lalu ping ke gateway lokal
      "login/PPPoE/RADIUS gagal"  -> radius, lalu service
      "email/website tertentu"    -> dns, lalu http ke domain itu

TAHAP 3 — SIMPULKAN.
Bila Anda melakukan pengecekan, jawab dengan format PERSIS:
   VERDICT: <SEHAT|DEGRADASI|GANGGUAN|TIDAK DIKETAHUI>
   KEYAKINAN: <0-100>
   AKAR_MASALAH: <satu kalimat>
   BUKTI: <poin bukti dari hasil tool>
   REKOMENDASI: <langkah perbaikan konkret, 1-3 poin>

Bila Anda TIDAK melakukan pengecekan (sapaan/pertanyaan umum/informasi), cukup balas singkat dan
ramah dalam Bahasa Indonesia — TANPA format VERDICT di atas.

Catatan: bila bukti menunjukkan masalah di luar jangkauan tool (mis. perlu ubah konfigurasi
router/OLT), tetap simpulkan dan tandai rekomendasi sebagai tindakan manual.`

// Run menjalankan satu sesi diagnosis penuh (tanpa streaming).
func (e *Engine) Run(ctx context.Context, query, target string) Report {
	return e.RunWith(ctx, query, target, nil)
}

// RunWith sama seperti Run, tetapi memanggil emit untuk setiap langkah baru
// sehingga UI dapat menampilkannya secara live (SSE).
func (e *Engine) RunWith(ctx context.Context, query, target string, emit func(Step)) Report {
	start := time.Now()
	rep := Report{
		ID:        fmt.Sprintf("INC-%d", start.Unix()),
		Query:     query,
		Target:    target,
		StartedAt: start,
		Engine:    "llm",
	}
	add := func(s Step) {
		rep.Steps = append(rep.Steps, s)
		if emit != nil {
			emit(s)
		}
	}
	// Target TIDAK ditebak dari pesan. Sebelumnya sistem mengekstrak alamat apa pun
	// yang mirip host dari teks bebas, lalu memakainya untuk probe — itu membuat
	// pesan seperti "tahlil kan ?" memicu pengecekan ke alamat yang tidak diminta.
	// Sekarang target hanya dipakai bila operator/pelanggan menyebutkannya secara
	// eksplisit; selebihnya agen yang memutuskan (termasuk bertanya balik).
	msgs := []llm.Message{{Role: "system", Content: systemPrompt}}
	user := query
	if target != "" {
		user += fmt.Sprintf("\n\nTarget yang disebut pengirim: %s", target)
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: user})

	tools := e.Diag.Tools()
	usedTools := map[string]int{}

	for step := 0; step < e.Cfg.MaxSteps; step++ {
		if ctx.Err() != nil {
			rep.Error = "dibatalkan / timeout"
			break
		}
		msg, err := e.LLM.Chat(ctx, msgs, tools)
		if err != nil {
			add(Step{Kind: "error", Text: "LLM gagal: " + err.Error()})
			rep.Error = err.Error()
			break
		}
		if t := strings.TrimSpace(msg.Content); t != "" {
			add(Step{Kind: "thought", Text: t})
		}
		if len(msg.ToolCalls) == 0 {
			rep.Answer = strings.TrimSpace(msg.Content)
			break
		}
		msgs = append(msgs, *msg)

		for _, tc := range msg.ToolCalls {
			args := map[string]any{}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			tgt := firstString(args, "target", "name")
			usedTools[tc.Function.Name]++

			res := e.Diag.Run(ctx, tc.Function.Name, tgt)
			add(Step{
				Kind: "tool", Tool: res.Tool, Target: res.Target,
				Output: res.Output, OK: res.OK, DurationMS: res.DurationMS,
			})
			payload, _ := json.Marshal(map[string]any{
				"tool": res.Tool, "target": res.Target, "ok": res.OK,
				"output": res.Output, "error": res.Err, "duration_ms": res.DurationMS,
			})
			msgs = append(msgs, llm.Message{
				Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload),
			})
		}
	}

	rep.Answer, rep.Verdict, rep.Confidence = parseVerdict(rep.Answer)
	rep.ElapsedMS = time.Since(start).Milliseconds()

	// Bila agen menjawab tanpa pengecekan (sapaan, pertanyaan umum, minta perjelas),
	// jangan eskalasi ke Codex — tidak ada yang perlu dianalisis dan verdict memang kosong.
	// Eskalasi hanya berguna ketika ada bukti probe yang belum terjelaskan.
	if len(usedTools) == 0 {
		rep.Engine = "llm (tanpa pengecekan)"
		if rep.Target == "" {
			rep.Target = probedTarget(rep.Steps)
		}
		e.simpan(rep)
		return rep
	}

	// Eskalasi ke Codex bila keyakinan rendah atau verdict tidak jelas.
	if e.shouldEscalate(rep, usedTools) {
		e.escalate(ctx, &rep)
	}

	// Bila pengirim tidak menyebut target, tampilkan alamat yang benar-benar dicek
	// agar laporan tidak menampilkan "Target: —" padahal ada probe yang dijalankan.
	if rep.Target == "" {
		rep.Target = probedTarget(rep.Steps)
	}

	e.simpan(rep)
	return rep
}

// simpan menyimpan laporan ke riwayat (dipakai kedua jalur keluar RunWith).
func (e *Engine) simpan(rep Report) {
	e.mu.Lock()
	e.reports = append(e.reports, rep)
	if len(e.reports) > 200 {
		e.reports = e.reports[len(e.reports)-200:]
	}
	e.mu.Unlock()
}

// probedTarget mengembalikan target pertama yang benar-benar dicek agen,
// supaya riwayat menampilkan alamat nyata meski pengirim tidak menyebutkannya.
func probedTarget(steps []Step) string {
	for _, s := range steps {
		if s.Kind == "tool" && s.Target != "" {
			return s.Target
		}
	}
	return ""
}

// Catatan: fungsi extractTarget() dihapus. Menebak alamat dari teks bebas membuat
// pesan yang tidak meminta pengecekan (mis. "tahlil kan ?") ikut memicu probe ke
// alamat yang tidak diminta. Keputusan target kini sepenuhnya milik agen.

func (e *Engine) shouldEscalate(rep Report, used map[string]int) bool {
	if e.Codex == nil || !e.Codex.Available() {
		return false
	}
	if rep.Error != "" || rep.Answer == "" {
		return true
	}
	if rep.Verdict == "" || rep.Verdict == "TIDAK DIKETAHUI" || rep.Confidence < 60 {
		return true
	}
	return false
}

func (e *Engine) escalate(ctx context.Context, rep *Report) {
	task := buildCodexTask(*rep)
	rep.Steps = append(rep.Steps, Step{Kind: "escalate", Text: "Keyakinan rendah -> eskalasi analisis ke Codex CLI (" + e.Cfg.CodexModel + ")"})
	res := e.Codex.Run(ctx, task)
	rep.Escalated = true
	if res.Err != "" {
		rep.Escalation = "Codex gagal: " + res.Err
		rep.Steps = append(rep.Steps, Step{Kind: "error", Text: rep.Escalation})
		return
	}
	rep.Escalation = res.Output
	rep.Engine = "llm+codex"
	rep.Steps = append(rep.Steps, Step{Kind: "answer", Text: res.Output, OK: true, DurationMS: res.ElapsedMS})
	if a, v, c := parseVerdict(res.Output); a != "" {
		if v != "" && v != "TIDAK DIKETAHUI" {
			rep.Verdict = v
		}
		if c > 0 {
			rep.Confidence = c
		}
		rep.Answer = a
	}
}

func buildCodexTask(rep Report) string {
	var b strings.Builder
	b.WriteString("Anda adalah analis NOC senior. Berikut sesi diagnosis otomatis yang keyakinannya rendah.\n\n")
	b.WriteString("PERTANYAAN OPERATOR:\n" + rep.Query + "\n\n")
	if rep.Target != "" {
		b.WriteString("TARGET: " + rep.Target + "\n\n")
	}
	b.WriteString("BUKTI PROBE YANG SUDAH DIKUMPULKAN:\n")
	for _, s := range rep.Steps {
		if s.Kind == "tool" {
			fmt.Fprintf(&b, "- %s %s (ok=%v, %dms): %s\n", s.Tool, s.Target, s.OK, s.DurationMS, oneLine(s.Output))
		}
	}
	b.WriteString("\nJANGAN menjalankan perintah jaringan baru dan jangan mengubah file apa pun.\n")
	b.WriteString("Berikan analisis akar masalah dan langkah perbaikan operasional untuk teknisi ISP.\n")
	b.WriteString("Akhiri jawaban dengan format persis:\nVERDICT: <SEHAT|DEGRADASI|GANGGUAN|TIDAK DIKETAHUI>\nKEYAKINAN: <0-100>\n")
	return b.String()
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " | ")
	s = strings.ReplaceAll(s, "\n", " | ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 500 {
		return s[:500] + "..."
	}
	return s
}

var (
	verdictRe    = regexp.MustCompile(`(?i)VERDICT\s*[:=]\s*([^\n]{0,80})`)
	confRe       = regexp.MustCompile(`(?i)KEYAKINAN\s*[:=]\s*([0-9]+(?:[.,][0-9]+)?)\s*%?`)
	verdictWords = []string{"TIDAK DIKETAHUI", "SEHAT", "DEGRADASI", "GANGGUAN"}
)

// parseVerdict mengekstrak VERDICT/KEYAKINAN dari jawaban bebas.
//
// Sengaja memakai regex, bukan pencocokan awal baris, karena model nyata
// mengembalikan variasi seperti "**VERDICT:** SEHAT" (markdown tebal) atau
// "VERDICT: SEHAT dan KEYAKINAN: 88" dalam satu baris.
func parseVerdict(answer string) (clean, verdict string, conf float64) {
	clean = strings.TrimSpace(answer)
	norm := strings.NewReplacer("*", " ", "_", " ", "`", " ").Replace(clean)

	if m := verdictRe.FindStringSubmatch(norm); m != nil {
		seg := strings.ToUpper(m[1])
		// Ambil kata kunci yang muncul paling awal (paling spesifik menang).
		best, bestIdx := "", -1
		for _, w := range verdictWords {
			if i := strings.Index(seg, w); i >= 0 && (bestIdx < 0 || i < bestIdx) {
				best, bestIdx = w, i
			}
		}
		verdict = best
	}
	if m := confRe.FindStringSubmatch(norm); m != nil {
		v := strings.ReplaceAll(m[1], ",", ".")
		var n float64
		if _, err := fmt.Sscanf(v, "%f", &n); err == nil {
			if n > 0 && n <= 1 {
				n *= 100
			}
			conf = n
		}
	}
	if verdict == "" {
		verdict = "TIDAK DIKETAHUI"
	}
	return clean, verdict, conf
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s, ok := v.(string); ok {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// extractTarget dihapus — lihat catatan di atas tentang alasan penghapusannya.

// Reports mengembalikan riwayat (terbaru dulu).
func (e *Engine) Reports() []Report {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]Report, 0, len(e.reports))
	for i := len(e.reports) - 1; i >= 0; i-- {
		out = append(out, e.reports[i])
	}
	return out
}
