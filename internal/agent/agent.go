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

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/codexbridge"
	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/directory"
	"ainoc/internal/incident"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/memory"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/session"
	"ainoc/internal/standard"
	"ainoc/internal/tool"
	"ainoc/internal/workflow"
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
	// Case ID + state dari Case Engine (fase 1). Kosong bila wiring tidak aktif.
	CaseID    string `json:"case_id,omitempty"`
	CaseState string `json:"case_state,omitempty"`
	// Jejak standar & sesi — supaya laporan bisa diaudit: standar versi berapa,
	// intent apa menurut kode, nomor siapa, dan apakah konteks lanjutan dipakai.
	Standar  string `json:"standar,omitempty"`
	Intent   string `json:"intent,omitempty"`
	Sesi     string `json:"sesi,omitempty"`
	Lanjutan bool   `json:"lanjutan,omitempty"`
	CacheHit bool   `json:"cache_hit,omitempty"`
	HematMS  int64  `json:"hemat_ms,omitempty"`
	// Jejak pembelajaran: kategori keluhan dan berapa kali berulang.
	Signature string `json:"signature,omitempty"`
	Ulang     int    `json:"ulang,omitempty"`
	// Balasan = teks untuk pelanggan (bahasa manusia). Answer memuat ringkasan
	// teknis lengkap untuk dashboard/audit.
	Balasan string `json:"balasan,omitempty"`
}

// Escalator adalah abstraksi Codex bridge (memudahkan pengujian).
type Escalator interface {
	Available() bool
	Run(ctx context.Context, task string) codexbridge.Result
}

// Engine adalah orkestrator agen. Sesi percakapan (per nomor), memory, dan
// pembelajaran disimpan di sini supaya konteks pelanggan terisolasi, jawaban
// identik tidak dihitung ulang, dan sistem makin pintar dari waktu ke waktu.
type Engine struct {
	Cfg   *config.Config
	LLM   *llm.Client
	Diag  *diag.Runner
	Codex Escalator
	Sesi  *session.Store
	Mem   *memory.Store
	Learn *learning.Store
	Inc   *incident.Store
	Aud   *audit.Store
	// Reg & Disp menghubungkan agent ke tool eksternal (Billing/RADIUS/dll).
	// Bila nil, agent hanya memakai probe jaringan bawaan (diag).
	Reg  *registry.Registry
	Disp *tool.Dispatcher
	// Wkf menghubungkan agent ke workflow engine deterministik (blueprint §13).
	// Bila ada workflow cocok untuk intent keluhan, KODE yang mengontrol urutan
	// langkah — AI tidak mengimprovisasi tiap langkah operasional.
	Wkf *workflow.Registry

	// Case tracker fase 1: memetakan identity -> Case dan mendorong state machine
	// (NEW->...->INVESTIGATION/ESCALATION). Bila nil, alur diagnosis jalan tanpa
	// case (backward-compatible).
	Case *CaseWire

	// PolicyGate fase 3: satu-satunya jalur keputusan eksekusi aksi. Bila nil,
	// agent TIDAK mengeksekusi tool eksternal (dispatcher tetap deny-by-default)
	// — hanya probe jaringan read-only yang jalan. Bila terisi, setiap usulan
	// aksi AI lewat Check() dulu: LOW read-only ALLOW, selainnya eskalasi.
	Policy *PolicyGate

	// Verify fase 4: Verification Engine — satu-satunya jalur yang boleh
	// menutup aksi WRITE menjadi RESOLVED. Bila nil, aksi write (yang sudah
	// diizinkan) tetap dieksekusi TANPA verifikasi — jangan biarkan nil di
	// produksi (dipasang di main.go). Read-only tidak membutuhkan gate ini.
	Verify *VerificationGate

	mu      sync.Mutex
	reports []Report
}

func New(cfg *config.Config, client *llm.Client, runner *diag.Runner, esc Escalator, sesi *session.Store, mem *memory.Store, learn *learning.Store) *Engine {
	if sesi == nil {
		sesi = session.New(session.DefaultConfig())
	}
	if mem == nil {
		mem = memory.New(memory.Config{})
	}
	if learn == nil {
		learn = learning.New()
	}
	return &Engine{Cfg: cfg, LLM: client, Diag: runner, Codex: esc, Sesi: sesi, Mem: mem, Learn: learn}
}

// KonteksAI adalah seluruh informasi yang "dilempar" ke model sebelum menjawab.
// Ini yang membuat AI tahu banyak konteks, bukan hanya pesan terakhir.
type KonteksAI struct {
	Klas         HasilKlasifikasi
	Signature    learning.Signature
	Facts        []memory.Fact
	Insiden      []memory.Incident
	Ulang        int
	VerdictAkhir string
	Playbook     []string
	SudahDicek   bool
}

// SusunKonteks mengumpulkan semua konteks yang relevan untuk satu pesan masuk.
// Dipanggil sebelum LLM, hasilnya disuntikkan ke prompt.
func (e *Engine) SusunKonteks(identity, query string) KonteksAI {
	klas := e.Klasifikasi(identity, query)
	sig := learning.SignatureOf(query)

	k := KonteksAI{
		Klas:      klas,
		Signature: sig,
		Facts:     e.Mem.Facts(klas.Key),
		Insiden:   e.Mem.RecentIncidents(klas.Key, 14*24*time.Hour),
		Playbook:  e.Learn.Playbook(sig),
	}
	k.Ulang, k.VerdictAkhir, _ = e.Mem.Recurrence(klas.Key, string(sig), 7*24*time.Hour)
	// Tandai bila target ini sudah pernah diperiksa (hindari probe berulang).
	if tgt := e.Sesi.LastTarget(klas.Key); tgt != "" {
		k.SudahDicek = true
	}
	return k
}

// blokKonteks menyusun teks konteks yang disuntikkan ke prompt. Inilah bagian
// yang membuat AI "tahu banyak konteks" saat ada pesan WhatsApp masuk.
func (k KonteksAI) blokKonteks() string {
	var b strings.Builder

	if len(k.Facts) > 0 {
		b.WriteString("\n\nYang sudah kita ketahui tentang pengirim ini:\n")
		for i, f := range k.Facts {
			if i >= 6 {
				break
			}
			b.WriteString("- " + f.Kind + ": " + f.Text)
			if f.Hits > 1 {
				fmt.Fprintf(&b, " (disebut %dx)", f.Hits)
			}
			b.WriteString("\n")
		}
	}

	if len(k.Insiden) > 0 {
		fmt.Fprintf(&b, "\nRiwayat gangguan pengirim ini (%d insiden terakhir):\n", len(k.Insiden))
		for i, inc := range k.Insiden {
			if i >= 5 {
				break
			}
			fmt.Fprintf(&b, "- %s: %s -> %s (%s, target %s)\n",
				inc.At.Format("02 Jan 15:04"), inc.Signature, inc.Verdict, inc.Engine, inc.Target)
		}
	}

	if k.Ulang >= 2 {
		fmt.Fprintf(&b, "\nPERHATIAN: keluhan kategori %s sudah %dx dalam 7 hari terakhir (terakhir: %s). "+
			"Ini indikasi masalah BERULANG/kronis, bukan gangguan sesaat — sebutkan hal ini di analisis.\n",
			k.Signature, k.Ulang, k.VerdictAkhir)
	}

	if len(k.Playbook) > 0 {
		b.WriteString("\nUrutan probe yang paling terbukti berguna untuk keluhan jenis ini " +
			"(hasil pembelajaran dari diagnosis sebelumnya): " + strings.Join(k.Playbook, " -> ") + "\n")
	}

	if k.SudahDicek {
		b.WriteString("\nCatatan: target ini sudah pernah diperiksa di percakapan sebelumnya — " +
			"jangan ulangi probe yang sama kecuali pengirim meminta ulang.\n")
	}

	return b.String()
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
// identity adalah pengirim (nomor WhatsApp). Konteks percakapan, memory, dan
// cache dipisahkan per identity sehingga pelanggan tidak saling mengganggu.
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

	// KONTEKS: standar (intent) + memory (fakta & riwayat) + pembelajaran (playbook).
	konteks := e.SusunKonteks(identity, query)
	klas := konteks.Klas
	rep.Intent = string(klas.Intent)
	rep.Standar = standard.Version
	rep.Sesi = klas.Key
	rep.Lanjutan = klas.Lanjutan
	rep.Signature = string(konteks.Signature)
	rep.Ulang = konteks.Ulang

	// ---- FASE 1: hubungkan Case Engine (buat/lanjutkan case per pengirim) ----
	// Case ID + state disalin ke Report supaya muncul di laporan/audit. Bila
	// wiring mati (e.Case nil), alur diagnosis tetap jalan seperti semula.
	if e.Case != nil {
		snap := e.Case.begin(klas.Key, "whatsapp")
		rep.CaseID, rep.CaseState = snap.CaseID, snap.CaseState
	}

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

	// ---- FASE 1: dorong state case ke jalur diagnosis bila keluhan nyata ----
	if e.Case != nil {
		snap := e.Case.onDiagnosis(klas.Key, klas.Intent)
		rep.CaseID, rep.CaseState = snap.CaseID, snap.CaseState
	}

	// ---- WORKFLOW DETERMINISTIK (blueprint §13) ----
	// Bila ada workflow cocok untuk intent keluhan, KODE yang mengontrol urutan
	// langkah (identity -> billing -> radius -> mikrotik -> genieacs -> correlate).
	// AI TIDAK mengimprovisasi langkah operasional. LLM hanya menyusun balasan.
	// (Dijalankan SETELAH gerbang keras: hanya COMPLAINT yang boleh menyentuh
	// workflow; sapaan/info tidak pernah memicu probe.)
	if bolehProbe && e.Wkf != nil {
		if def, ok := e.Wkf.ForIntent(string(klas.Intent)); ok {
			add(Step{Kind: "intent", Text: fmt.Sprintf(
				"standar v%s: intent=%s -> workflow %s (deterministik)", standard.Version, klas.Intent, def.Name)})
			h := e.runWorkflow(ctx, klas.Key, target, def, add)
			rep.Steps = append(rep.Steps, h.Steps...)
			used := map[string]int{}
			if h.RanAnyTool {
				for _, s := range h.Steps {
					if s.Kind == "tool" {
						used[s.Tool]++
					}
				}
			}
			// LLM menyusun balasan manusiawi dari bukti (bukan memutuskan langkah).
			rep.Balasan, rep.Answer = e.buildWorkflowBalasan(klas.Key, query, h)
			rep.Answer, rep.Verdict, rep.Confidence = parseVerdict(rep.Answer)
			if rep.Balasan == "" {
				rep.Balasan = rep.Answer
			}
			rep.Engine = "workflow"
			rep.ElapsedMS = time.Since(start).Milliseconds()
			if rep.Target == "" {
				rep.Target = probedTarget(rep.Steps)
			}
			// FASE 1: tutup state case (INVESTIGATION/ESCALATION).
			// Workflow deterministik untuk keluhan SELALU dianggap diagnosis
			// nyata — walau semua adapter nonaktif (dilewati) dan korelasinya
			// berujung TIDAK DIKETAHUI, itu tetap hasil diagnosis yang harus
			// bisa di-eskalasi.
			if e.Case != nil {
				snap := e.Case.onResult(klas.Key, klas.Intent, rep.Verdict, rep.Confidence, true)
				rep.CaseID, rep.CaseState = snap.CaseID, snap.CaseState
			}
			// Eskalasi ke Codex bila keyakinan rendah (analisis senior).
			if e.shouldEscalate(rep, used) {
				e.escalate(ctx, &rep)
			}
			e.catat(klas, konteks, rep)
			e.simpan(rep)
			return rep
		}
	}

	// ---- FASE 1: lanjutkan state case setelah diagnosis selesai ----

	tools := e.Diag.Tools()
	// Tambahkan tool eksternal yang aktif (registry) bila ada dan diizinkan.
	if e.Reg != nil && bolehProbe {
		tools = append(tools, e.Reg.LLMTools()...)
	}
	if !bolehProbe {
		tools = nil
		add(Step{Kind: "intent", Text: fmt.Sprintf(
			"standar v%s: intent=%s -> %s", standard.Version, klas.Intent,
			map[bool]string{true: "boleh cek jaringan", false: "TIDAK boleh cek jaringan (tanpa tool)"}[bolehProbe])})
	}

	// ---- KONTEKS KE AI: riwayat percakapan + memory + pembelajaran ----
	// Inilah yang membuat AI tahu banyak konteks saat ada pesan WhatsApp masuk,
	// bukan hanya pesan terakhir.
	msgs := []llm.Message{{Role: "system", Content: e.buildSystemPrompt()}}
	msgs = append(msgs, e.Sesi.History(klas.Key)...)

	user := query
	if target != "" {
		user += fmt.Sprintf("\n\nTarget yang disebut pengirim: %s", target)
	}
	if blok := konteks.blokKonteks(); blok != "" {
		user += blok
		if len(konteks.Facts) > 0 || len(konteks.Insiden) > 0 {
			add(Step{Kind: "konteks", Text: fmt.Sprintf(
				"konteks dikirim ke AI: %d fakta, %d insiden, %d pengulangan, playbook %v",
				len(konteks.Facts), len(konteks.Insiden), konteks.Ulang, konteks.Playbook)})
		}
	}
	if c, ok := directory.CallerFrom(ctx); ok {
		user += c.PromptBlock()
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

			// ---- FASE 3: gerbang kebijakan SEBELUM eksekusi apa pun ----
			// AI tidak pernah mengeksekusi tool tanpa policy check. Hanya
			// ALLOW (read-only LOW) yang diteruskan; APPROVAL_REQUIRED/DENY
			// (MEDIUM+) → eskalasi ke manusia, TIDAK dieksekusi otomatis.
			if e.Policy != nil {
				if auth := e.Policy.Check(rep.CaseID, tc.Function.Name, args); auth.Decision != policy.Allow {
					usedTools[tc.Function.Name]++
					reason := strings.Join(auth.Reasons, "; ")
					if reason == "" {
						reason = "kebijakan menahan aksi — butuh keputusan manusia"
					}
					add(Step{Kind: "policy", Tool: tc.Function.Name, OK: false,
						Output: string(auth.Decision) + ": " + reason})
					if snap := e.Policy.Escalate(klas.Key, rep.CaseID, tc.Function.Name, auth); snap.CaseID != "" {
						rep.CaseID, rep.CaseState = snap.CaseID, snap.CaseState
					}
					payload, _ := json.Marshal(map[string]any{
						"tool": tc.Function.Name, "ok": false, "decision": auth.Decision,
						"output": "", "error": "ditahan policy gate: " + reason,
						"request_id": "policy-gate",
					})
					msgs = append(msgs, llm.Message{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload),
					})
					continue
				}
			}

			// Rutekan: tool eksternal (registry) vs probe jaringan bawaan (diag).
			if e.Disp != nil && e.Reg != nil {
				if _, isExternal := e.Reg.Get(tc.Function.Name); isExternal {
					// ---- FASE 4: aksi WRITE lewat Verification Engine ----
					// Aksi yang mengubah state TIDAK boleh dianggap selesai tanpa
					// verifikasi nyata (master spec §13, §53 rule 7). Bila gerbang
					// verifikasi terpasang DAN tool ini write, eksekusi + verifikasi
					// + tutup loop (RESOLVED/ESCALATION) dilakukan RunAction. Tanpa
					// gerbang (nil), write tidak dijalankan — fail-closed.
					if toolIsWrite(e.Reg, tc.Function.Name) {
						if e.Verify == nil {
							rres := tool.Result{Tool: tc.Function.Name,
								Error: "verification engine tidak tersedia — aksi write tidak dijalankan"}
							usedTools[tc.Function.Name]++
							add(Step{Kind: "verify", Tool: tc.Function.Name, OK: false,
								Output: rres.Error})
							payload, _ := json.Marshal(map[string]any{
								"tool": tc.Function.Name, "ok": false, "decision": policy.Deny,
								"output": "", "error": rres.Error, "request_id": "verify-gate",
							})
							msgs = append(msgs, llm.Message{
								Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload),
							})
							continue
						}
						rres, vsnap := e.Verify.RunAction(ctx, klas.Key, rep.CaseID, tc.Function.Name, args, nil)
						usedTools[tc.Function.Name]++
						if vsnap.CaseID != "" {
							rep.CaseID, rep.CaseState = vsnap.CaseID, vsnap.CaseState
						}
						add(Step{
							Kind: "tool", Tool: rres.Tool, Target: firstString(args, "identity", "device_id", "target"),
							Output: rres.Output.Text, OK: rres.OK, DurationMS: rres.LatencyMS,
						})
						add(Step{Kind: "verify", Tool: tc.Function.Name, OK: vsnap.CaseState == string(caseengine.StateResolved),
							Output: "verifikasi pasca-aksi → case " + vsnap.CaseState})
						payload, _ := json.Marshal(map[string]any{
							"tool": rres.Tool, "ok": rres.OK, "decision": rres.Decision,
							"output": rres.Output.Text, "error": rres.Error,
							"request_id": rres.RequestID, "duration_ms": rres.LatencyMS,
							"case_state": vsnap.CaseState,
						})
						msgs = append(msgs, llm.Message{
							Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload),
						})
						continue
					}
					rres := e.Disp.Invoke(ctx, tc.Function.Name, args)
					usedTools[tc.Function.Name]++
					add(Step{
						Kind: "tool", Tool: rres.Tool, Target: firstString(args, "identity", "device_id", "target"),
						Output: rres.Output.Text, OK: rres.OK, DurationMS: rres.LatencyMS,
					})
					payload, _ := json.Marshal(map[string]any{
						"tool": rres.Tool, "ok": rres.OK, "decision": rres.Decision,
						"output": rres.Output.Text, "error": rres.Error,
						"request_id": rres.RequestID, "duration_ms": rres.LatencyMS,
					})
					msgs = append(msgs, llm.Message{
						Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload),
					})
					continue
				}
			}

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

	// Pisahkan: teks untuk pelanggan vs ringkasan teknis untuk sistem.
	// Answer disimpan sebagai RINGKASAN TEKNIS saja (tanpa teks pelanggan dan
	// tanpa label "BALASAN:") supaya dashboard tidak menampilkan teks campur.
	balasan, teknis := PisahBalasan(rep.Answer)
	rep.Balasan = balasan
	if strings.TrimSpace(teknis) != "" {
		rep.Answer = strings.TrimSpace(teknis)
	}
	rep.Answer, rep.Verdict, rep.Confidence = parseVerdict(rep.Answer)
	if rep.Balasan == "" {
		rep.Balasan = rep.Answer // fallback: jangan pernah kosong
	}
	rep.ElapsedMS = time.Since(start).Milliseconds()

	// ---- FASE 1: tutup state case (INVESTIGATION/ESCALATION) ----
	if e.Case != nil {
		snap := e.Case.onResult(klas.Key, klas.Intent, rep.Verdict, rep.Confidence, len(usedTools) > 0)
		rep.CaseID, rep.CaseState = snap.CaseID, snap.CaseState
	}

	// Bila agen menjawab tanpa pengecekan (sapaan, pertanyaan umum, minta perjelas),
	// jangan eskalasi ke Codex — tidak ada yang perlu dianalisis dan verdict memang kosong.
	if len(usedTools) == 0 {
		rep.Engine = "llm (tanpa pengecekan)"
		if rep.Target == "" {
			rep.Target = probedTarget(rep.Steps)
		}
		e.catat(klas, konteks, rep)
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

	e.catat(klas, konteks, rep)
	e.simpan(rep)
	return rep
}

// catat menyimpan pertukaran ke riwayat percakapan nomor tersebut, menyimpannya
// ke cache, MEMPELAJARI hasilnya, dan MENYIMPAN fakta baru ke memory.
func (e *Engine) catat(klas HasilKlasifikasi, konteks KonteksAI, rep Report) {
	e.Sesi.Append(klas.Key, "user", rep.Query, string(klas.Intent), "", "")
	e.Sesi.Append(klas.Key, "assistant", rep.Answer, string(klas.Intent), rep.Verdict, rep.Target)

	// --- SELF LEARNING: catat hasil untuk memperbaiki playbook ---
	probes := make([]string, 0, len(rep.Steps))
	durasi := map[string]int64{}
	for _, s := range rep.Steps {
		if s.Kind == "tool" {
			probes = append(probes, s.Tool)
			durasi[s.Tool] = s.DurationMS
		}
	}
	// Hanya pelajari hasil yang selesai (bukan timeout/error), supaya playbook
	// tidak belajar dari diagnosis yang terpotong.
	if rep.Error == "" {
		e.Learn.Record(konteks.Signature, rep.Verdict, probes, durasi)
	}

	// --- MEMORY: fakta otomatis dari pesan pengirim ---
	for _, f := range memory.ExtractFacts(rep.Query) {
		e.Mem.AddFact(klas.Key, f.Kind, f.Text)
	}

	// --- MEMORY: catat insiden (hanya bila ada pengecekan) ---
	if len(probes) > 0 {
		e.Mem.AddIncident(klas.Key, memory.Incident{
			ID: rep.ID, At: rep.StartedAt, Query: truncate(rep.Query, 200),
			Signature: string(konteks.Signature), Intent: string(klas.Intent),
			Verdict: rep.Verdict, Confidence: rep.Confidence,
			Target: rep.Target, Probes: probes, Engine: rep.Engine,
		})
	}

	// --- INCIDENT STORE: rekam insiden terstruktur (blueprint §28, §31) ---
	if e.Inc != nil {
		inc := incident.Incident{
			ID:         rep.ID,
			Status:     incident.StatusInvestigating,
			Source:     "whatsapp",
			Identity:   klas.Key,
			Intent:     string(klas.Intent),
			Query:      truncate(rep.Query, 200),
			Target:     rep.Target,
			Verdict:    rep.Verdict,
			Confidence: rep.Confidence,
			StartedAt:  rep.StartedAt,
			RequestID:  rep.ID,
		}
		if len(probes) == 0 {
			inc.Status = incident.StatusClosed // sapaan/info: bukan gangguan nyata
		}
		if rep.Error != "" {
			inc.Status = incident.StatusUnknown
		}
		e.Inc.Add(inc)
	}

	// --- AUDIT: rekam keputusan diagnosa (append-only) ---
	if e.Aud != nil {
		e.Aud.Record(audit.Entry{
			EventType:  "diagnosis",
			Actor:      "agent",
			CaseID:     rep.CaseID,
			EntityType: "incident",
			EntityID:   rep.ID,
			After: map[string]any{
				"verdict":    rep.Verdict,
				"confidence": rep.Confidence,
				"engine":     rep.Engine,
				"case_state": rep.CaseState,
				"probes":     probes,
			},
			Note: "diagnosis selesai",
		})
	}

	// Cache hanya untuk jawaban yang selesai (tidak error, tidak timeout).
	if rep.Error != "" {
		return
	}
	e.Sesi.CacheStore(klas.CacheKey, rep.Answer, rep.Verdict, rep.Confidence,
		rep.Engine, rep.ElapsedMS, probes)
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
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

// shouldEscalate — FASE 3 — memutuskan apakah analisis perlu eskalasi, dengan
// dasar POLICY GATE (bukan hardcode return false):
//
//   - Aksi yang ditahan kebijakan (APPROVAL_REQUIRED/DENY pada usulan AI) →
//     true, dan eskalasi disalurkan ke MANUSIA (CaseWire ESCALATION + handoff
//     WA fase 2) — escalate() tidak pernah memanggil model lain untuk kasus ini
//     (phase 0: otoritas manusia tidak didelegasikan ke model/CLI lain).
//   - Keyakinan rendah tanpa aksi berisiko → tetap false di sini karena jalur
//     manusianya sudah ditangani CaseWire.onResult + deliverEscalation.
func (e *Engine) shouldEscalate(rep Report, used map[string]int) bool {
	if e.Policy == nil {
		return false
	}
	return e.Policy.RequiresHuman(rep.Steps)
}

func (e *Engine) escalate(ctx context.Context, rep *Report) {
	// FASE 3: eskalasi kebijakan → manusia. Handoff WA (fase 2) terpicu oleh
	// rep.CaseState == ESCALATION; di sini kita hanya menandai laporan dan
	// TIDAK memanggil model lain — keputusan berisiko tidak boleh digeser ke
	// Codex/model lain secara diam-diam (phase 0).
	if e.Policy != nil && e.Policy.RequiresHuman(rep.Steps) {
		rep.Escalated = true
		rep.Escalation = "policy gate menahan aksi berisiko — butuh keputusan manusia (handoff NOC/Admin)"
		rep.Steps = append(rep.Steps, Step{Kind: "escalate",
			Text: "FASE 3: aksi ditahan kebijakan → eskalasi ke manusia, tanpa eksekusi otomatis"})
		return
	}

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
	verdictRe = regexp.MustCompile(`(?i)VERDICT\s*[:=]\s*([^\n]{0,80})`)
	confRe    = regexp.MustCompile(`(?i)KEYAKINAN\s*[:=]\s*([0-9]+(?:[.,][0-9]+)?)\s*%?`)
	// labelStripRe membuang label format dari teks pelanggan.
	labelStripRe = regexp.MustCompile(`(?im)^\s*(BALASAN|VERDICT|KEYAKINAN|AKAR_MASALAH|BUKTI|REKOMENDASI)\s*[:=]\s*`)
	// labelTeknisRe mengenali baris awal blok ringkasan teknis.
	labelTeknisRe = regexp.MustCompile(`(?im)^\s*(VERDICT|KEYAKINAN|AKAR_MASALAH|BUKTI)\s*[:=]`)
	verdictWords  = []string{"TIDAK DIKETAHUI", "SEHAT", "DEGRADASI", "GANGGUAN"}
)

// PisahBalasan memisahkan jawaban model menjadi dua bagian:
//   - balasan: teks untuk pelanggan (bahasa manusia)
//   - teknis : ringkasan VERDICT/KEYAKINAN/AKAR_MASALAH/BUKTI untuk sistem
//
// Dipisah berbasis BARIS (bukan satu regex besar) karena Go's RE2 tidak
// mendukung lookahead — pendekatan regex tunggal akan ikut memakan kata
// "VERDICT" di awal blok teknis.
//
// Bila model tidak memakai format "BALASAN:", seluruh jawaban dianggap balasan
// pelanggan dengan baris teknis dibuang, supaya perilakunya tetap aman.
func PisahBalasan(answer string) (balasan, teknis string) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return "", ""
	}
	lines := strings.Split(answer, "\n")

	// Cari awal blok teknis: baris pertama yang diawali label teknis.
	teknisStart := -1
	for i, ln := range lines {
		if labelTeknisRe.MatchString(ln) {
			teknisStart = i
			break
		}
	}

	// Cari awal blok balasan (baris "BALASAN:"), bila ada.
	balasanStart := -1
	for i, ln := range lines {
		if i == teknisStart {
			break
		}
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(ln)), "BALASAN") {
			balasanStart = i
			break
		}
	}

	// Tentukan batas tiap bagian.
	balasanEnd := len(lines)
	if teknisStart >= 0 {
		balasanEnd = teknisStart
	}
	awal := 0
	if balasanStart >= 0 {
		awal = balasanStart
	}
	if balasanEnd > awal {
		balasan = strings.TrimSpace(strings.Join(lines[awal:balasanEnd], "\n"))
	}
	balasan = strings.TrimSpace(labelStripRe.ReplaceAllString(balasan, ""))

	if teknisStart >= 0 {
		teknis = strings.TrimSpace(strings.Join(lines[teknisStart:], "\n"))
	} else {
		// Tidak ada blok teknis terpisah: jawaban model seluruhnya dianggap
		// ringkasan (dipakai parseVerdict), dan balasan tetap teks pelanggan.
		teknis = answer
	}
	return balasan, teknis
}

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
