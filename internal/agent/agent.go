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
	"ainoc/internal/session"
	"ainoc/internal/standard"
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
	// Jejak standar & sesi — supaya laporan bisa diaudit: standar versi berapa,
	// intent apa menurut kode, nomor siapa, dan apakah konteks lanjutan dipakai.
	Standar  string `json:"standar,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Sesi     string `json:"sesi,omitempty"`
	Lanjutan bool   `json:"lanjutan,omitempty"`
	CacheHit bool   `json:"cache_hit,omitempty"`
	HematMS  int64  `json:"hemat_ms,omitempty"`
}

// Escalator adalah abstraksi Codex bridge (memudahkan pengujian).
type Escalator interface {
	Available() bool
	Run(ctx context.Context, task string) codexbridge.Result
}

// Engine adalah orkestrator agen. Sesi percakapan (per nomor) dan cache disimpan
// di sini supaya konteks pelanggan terisolasi dan jawaban identik tidak dihitung ulang.
type Engine struct {
	Cfg   *config.Config
	LLM   *llm.Client
	Diag  *diag.Runner
	Codex Escalator
	Sesi  *session.Store

	mu      sync.Mutex
	reports []Report
}

func New(cfg *config.Config, client *llm.Client, runner *diag.Runner, esc Escalator, sesi *session.Store) *Engine {
	if sesi == nil {
		sesi = session.New(session.DefaultConfig())
	}
	return &Engine{Cfg: cfg, LLM: client, Diag: runner, Codex: esc, Sesi: sesi}
}

// buildSystemPrompt menyusun kontrak perilaku: dokumen standar (yang bisa diganti
// operator tanpa build ulang) + instruksi operasional yang tetap.
func (e *Engine) buildSystemPrompt() string {
	doc := standard.Doc(e.Cfg.StandardDoc)
	return `Anda adalah "NOC Sentinel", asisten NOC (Network Operations Center) untuk ISP.
Anda menerima pesan dari pelanggan dan teknisi lewat WhatsApp.

=== STANDAR KONTEKS v` + standard.Version + ` (WAJIB DIPATUHI) ===
` + doc + `
=== AKHIR STANDAR ===

CATATAN OPERASIONAL:
- Balas dalam Bahasa Indonesia, ringkas, dan operasional.
- Jangan menyebut nama tool, nama model, atau istilah internal kepada pelanggan.
- Bila pengirim tidak menyebut target, pilih sendiri alamat yang paling masuk akal
  untuk keluhan itu (keluhan umum: gateway lokal lalu DNS publik seperti 8.8.8.8).
- Bila sudah ada konteks percakapan sebelumnya dengan pengirim ini, gunakan sebagai
  rujukan; pesan lanjutan tidak perlu mengulang detail.`
}

// HasilKlasifikasi menyatukan keputusan kode (standar) dengan konteks sesi.
type HasilKlasifikasi struct {
	Intent   standard.Intent
	Key      string
	CacheKey string
	CacheHit bool
	Lanjutan bool // ada percakapan sebelumnya yang masih aktif
}

// Klasifikasi menjalankan standar secara deterministik: tentukan intent,
// kunci sesi (per nomor), dan kunci cache.
func (e *Engine) Klasifikasi(identity, query string) HasilKlasifikasi {
	key := session.Key(identity)
	intent := standard.Classify(query)
	_, lastIntent, turns := e.Sesi.Meta(key)
	lanjutan := turns > 0
	// Pesan lanjutan yang pendek (mis. "masih lambat") dianggap keluhan bila
	// percakapan sebelumnya juga tentang keluhan — konteks per nomor dipakai.
	if intent == standard.IntentUnclear && lanjutan && lastIntent == string(standard.IntentComplaint) {
		if len(strings.Fields(standard.Normalize(query))) <= 5 {
			intent = standard.IntentComplaint
		}
	}
	return HasilKlasifikasi{
		Intent:   intent,
		Key:      key,
		CacheKey: session.CacheKey(identity, query),
		Lanjutan: lanjutan,
	}
}

// Prompt lama dihapus — kontrak perilaku kini berasal dari paket standard
// (standards/context-standard.md) sehingga bisa diubah tanpa menyentuh kode.

// Run menjalankan satu sesi diagnosis penuh (tanpa streaming).
// identity = pengirim (nomor). Gunakan RunWith bila ingin streaming langkah.
func (e *Engine) Run(ctx context.Context, identity, query, target string) Report {
	return e.RunWith(ctx, identity, query, target, nil)
}

// RunWith sama seperti Run, tetapi memanggil emit untuk setiap langkah baru
// sehingga UI dapat menampilkannya secara live (SSE).
//
// identity adalah pengirim (nomor WhatsApp). Konteks percakapan dan cache
// dipisahkan per identity sehingga pelanggan tidak saling mengganggu.
func (e *Engine) RunWith(ctx context.Context, identity, query, target string, emit func(Step)) Report {
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

	// STANDAR dijalankan kode: tentukan intent + kunci sesi + kunci cache.
	klas := e.Klasifikasi(identity, query)
	rep.Intent = string(klas.Intent)
	rep.Standar = standard.Version
	rep.Sesi = klas.Key
	rep.Lanjutan = klas.Lanjutan

	// ---- CACHE: pertanyaan sama dari nomor sama dalam jendela waktu ----
	if ans, verdict, conf, engine, elapsed, _, ok := e.Sesi.CacheLookup(klas.CacheKey); ok {
		rep.Answer, rep.Verdict, rep.Confidence = ans, verdict, conf
		rep.Engine = engine + " (cache)"
		rep.ElapsedMS = time.Since(start).Milliseconds()
		rep.CacheHit = true
		rep.HematMS = elapsed - rep.ElapsedMS
		add(Step{Kind: "cache", Text: fmt.Sprintf(
			"jawaban identik dalam %d menit terakhir untuk nomor ini — dipakai ulang (hemat %dms)",
			int(e.Sesi.CacheTTL().Minutes()), rep.HematMS)})
		if rep.Target == "" {
			rep.Target = e.Sesi.LastTarget(klas.Key)
		}
		e.simpan(rep)
		return rep
	}

	// ---- GERBANG KERAS: hanya keluhan nyata boleh memicu pengecekan ----
	// Ini yang membuat standar tetap sama untuk model apa pun: model tidak bisa
	// memaksa probe pada sapaan/informasi walaupun ia "berhalusinasi".
	bolehProbe := standard.BolehProbe(klas.Intent)
	tools := e.Diag.Tools()
	if !bolehProbe {
		tools = nil
		add(Step{Kind: "intent", Text: fmt.Sprintf(
			"standar v%s: intent=%s -> %s", standard.Version, klas.Intent,
			map[bool]string{true: "boleh cek jaringan", false: "TIDAK boleh cek jaringan (tanpa tool)"}[bolehProbe])})
	}

	// ---- RIWAYAT: konteks percakapan nomor ini saja ----
	msgs := []llm.Message{{Role: "system", Content: e.buildSystemPrompt()}}
	msgs = append(msgs, e.Sesi.History(klas.Key)...)

	user := query
	if target != "" {
		user += fmt.Sprintf("\n\nTarget yang disebut pengirim: %s", target)
	}
	if !bolehProbe {
		user += "\n\n(Pesan ini terdeteksi sebagai " + string(klas.Intent) +
			" oleh standar konteks. Jawab langsung tanpa melakukan pengecekan jaringan.)"
	}
	if lastTarget := e.Sesi.LastTarget(klas.Key); lastTarget != "" && target == "" {
		user += "\n\nKonteks: percakapan sebelumnya dengan pengirim ini memeriksa " + lastTarget + "."
	}
	// Peringatkan model saat ini BUKAN pesan pertama, supaya ia tidak mengulang
	// sapaan dan tidak meminta data yang sudah kita punya (nomor pengirim).
	if klas.Lanjutan {
		user += "\n\n(Percakapan ini sudah berjalan. JANGAN ulangi sapaan seperti \"Halo\", " +
			"jangan perkenalan ulang, dan jangan minta nomor/ID pelanggan — kita sudah tahu " +
			"nomornya. Tanggapi langsung isi pesan terakhir.)"
	} else {
		user += "\n\n(Ini pesan pertama dari pengirim ini. Balas singkat dan ramah.)"
	}
	msgs = append(msgs, llm.Message{Role: "user", Content: user})

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
	if len(usedTools) == 0 {
		rep.Engine = "llm (tanpa pengecekan)"
		if rep.Target == "" {
			rep.Target = probedTarget(rep.Steps)
		}
		e.catat(klas, rep)
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

	e.catat(klas, rep)
	e.simpan(rep)
	return rep
}

// catat menyimpan pertukaran ke riwayat percakapan nomor tersebut dan
// menyimpannya ke cache (hanya bila hasilnya layak dipakai ulang).
func (e *Engine) catat(klas HasilKlasifikasi, rep Report) {
	e.Sesi.Append(klas.Key, "user", rep.Query, string(klas.Intent), "", "")
	e.Sesi.Append(klas.Key, "assistant", rep.Answer, string(klas.Intent), rep.Verdict, rep.Target)

	// Cache hanya untuk jawaban yang selesai (tidak error, tidak timeout).
	if rep.Error != "" {
		return
	}
	steps := make([]string, 0, len(rep.Steps))
	for _, s := range rep.Steps {
		if s.Kind == "tool" {
			steps = append(steps, s.Tool)
		}
	}
	e.Sesi.CacheStore(klas.CacheKey, rep.Answer, rep.Verdict, rep.Confidence,
		rep.Engine, rep.ElapsedMS, steps)
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

// shouldEscalate menentukan apakah analisis perlu dilanjutkan ke Codex CLI.
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
