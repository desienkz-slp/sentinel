// Eksekutor workflow deterministik (blueprint §13).
//
// Pemisahan yang ditegakkan blueprint: SKILL menjelaskan APA, WORKFLOW menjelaskan
// BAGAIMANA. Ketika pesan keluhan masuk dan ada workflow cocok, KODE inilah yang
// mengontrol urutan langkah — bukan LLM. LLM hanya dipakai di akhir untuk menyusun
// balasan manusiawi dari bukti yang sudah terkumpul.
//
// Keamanan tetap terjaga penuh: setiap pemanggilan tool lewat Disp.Invoke, yang
// melewati gerbang registry (terdaftar + aktif) -> policy (ALLOW/APPROVAL/DENY) ->
// adapter. Workflow TIDAK bisa memanggil tool yang nonaktif atau ditolak policy.
package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"ainoc/internal/caseengine"
	"ainoc/internal/correlation"
	"ainoc/internal/llm"
	"ainoc/internal/policy"
	"ainoc/internal/workflow"
)

// hasilWorkflow adalah hasil eksekusi satu workflow deterministik.
type hasilWorkflow struct {
	Def        workflow.Definition
	Evidence   *correlation.Set
	Conclusion correlation.Conclusion
	Steps      []Step
	RanAnyTool bool
	Aborted    string // alasan berhenti dini (bila ada)
}

// templateRe mencocokkan placeholder ${var} pada params step.
var templateRe = regexp.MustCompile(`\$\{([a-zA-Z0-9_]+)\}`)

// runWorkflow menjalankan workflow deterministik untuk satu keluhan.
//
// identity = nomor pengirim ternormalisasi (628xxx), target = apa yang disebut
// pengirim (bila ada). Fungsi ini mengembalikan bukti ternormalisasi + kesimpulan
// korelasi; tidak pernah memanggil LLM.
func (e *Engine) runWorkflow(ctx context.Context, identity, target string, def workflow.Definition, add func(Step)) hasilWorkflow {
	h := hasilWorkflow{Def: def, Evidence: correlation.NewSet()}
	vars := map[string]string{"identity_id": identity}
	if target != "" {
		vars["device_id_if_known"] = target
	}

	// record mencatat langkah ke h.Steps sekaligus meneruskannya ke emit (SSE/dashboard).
	record := func(s Step) {
		h.Steps = append(h.Steps, s)
		if add != nil {
			add(s)
		}
	}

	for _, st := range def.Steps {
		if ctx.Err() != nil {
			h.Aborted = "konteks dibatalkan / timeout"
			break
		}

		// Langkah non-tool: resolve_identity (sudah punya identity pengirim) dan
		// correlate (dijalankan setelah semua bukti terkumpul).
		if strings.TrimSpace(st.Tool) == "" {
			record(Step{Kind: "thought", Text: fmt.Sprintf(
				"[workflow %s] langkah %d: %s", def.Name, st.Index, st.Name)})
			continue
		}

		// Cek allowed_tools: bila langkah menyatakan daftar tool yang diizinkan,
		// dan tool ini tidak ada di dalamnya, lewati (deny-by-default).
		if len(st.AllowedTools) > 0 && !strInList(st.AllowedTools, st.Tool) {
			record(Step{Kind: "thought", Text: fmt.Sprintf(
				"[workflow] langkah %d: tool %s tidak diizinkan oleh allowed_tools — dilewati",
				st.Index, st.Tool)})
			continue
		}

		// Resolve params: ${var} -> nilai variabel yang sudah diketahui.
		args := map[string]any{}
		for k, v := range st.Params {
			args[k] = resolveTemplate(fmt.Sprintf("%v", v), vars)
		}

		// Keamanan: tool eksternal WAJIB lewat dispatcher (registry->policy->adapter).
		if e.Disp == nil || e.Reg == nil {
			h.Aborted = fmt.Sprintf("dispatcher tidak tersedia — tool %s tidak bisa dijalankan", st.Tool)
			break
		}
		if _, err := e.Reg.CanInvoke(st.Tool); err != nil {
			// Tool nonaktif/tidak terdaftar: catat sebagai UNKNOWN (jangan tebak),
			// lanjut ke langkah berikutnya.
			record(Step{Kind: "tool", Tool: st.Tool, OK: false,
				Output: "dilewati: " + err.Error()})
			continue
		}

		// ---- FASE 3: gerbang kebijakan SEBELUM eksekusi ----
		// Workflow deterministik pun tidak boleh mengeksekusi aksi berisiko.
		// Hanya ALLOW (read-only LOW) yang diteruskan ke dispatcher; selainnya
		// → eskalasi kebijakan (case ESCALATION), langkah dicatat, TIDAK invoke.
		if e.Policy != nil {
			if auth := e.Policy.Check("", st.Tool, args); auth.Decision != policy.Allow {
				reason := strings.Join(auth.Reasons, "; ")
				if reason == "" {
					reason = "kebijakan menahan aksi — butuh keputusan manusia"
				}
				record(Step{Kind: "policy", Tool: st.Tool, OK: false,
					Output: string(auth.Decision) + ": " + reason})
				e.Policy.Escalate(identity, "", st.Tool, auth)
				h.Aborted = "langkah " + st.Tool + " ditahan policy gate → eskalasi"
				break
			}
		}

		// ---- FASE 4: aksi WRITE lewat Verification Engine ----
		// Aksi yang mengubah state tidak boleh dianggap selesai tanpa verifikasi
		// nyata. Read-only lanjut ke dispatcher biasa; WRITE (bila policy ALLOW,
		// jarang karena deny-by-default) dieksekusi + diverifikasi + ditutup lewat
		// RunAction. Tanpa gate verifikasi, write tidak dijalankan (fail-closed).
		if toolIsWrite(e.Reg, st.Tool) {
			if e.Verify == nil {
				record(Step{Kind: "verify", Tool: st.Tool, OK: false,
					Output: "verification engine tidak tersedia — aksi write tidak dijalankan"})
				h.Aborted = "langkah " + st.Tool + " butuh verification engine (tidak tersedia)"
				break
			}
			rres, vsnap := e.Verify.RunAction(ctx, identity, "", st.Tool, args, nil)
			h.RanAnyTool = true
			record(Step{
				Kind: "tool", Tool: rres.Tool,
				Target: firstString(args, "identity", "device_id", "target", "interface"),
				Output: rres.Output.Text, OK: rres.OK, DurationMS: rres.LatencyMS,
			})
			record(Step{Kind: "verify", Tool: st.Tool,
				OK:     vsnap.CaseState == string(caseengine.StateResolved),
				Output: "verifikasi pasca-aksi → case " + vsnap.CaseState})
			domain := toolDomain(st.Tool)
			if rres.OK {
				h.Evidence.Add(domain, rres.Output.Text)
			} else {
				h.Evidence.Add(domain, "ERROR: "+rres.Error)
			}
			continue
		}

		rres := e.Disp.Invoke(ctx, st.Tool, args)
		h.RanAnyTool = true
		record(Step{
			Kind: "tool", Tool: rres.Tool,
			Target: firstString(args, "identity", "device_id", "target", "interface"),
			Output: rres.Output.Text, OK: rres.OK, DurationMS: rres.LatencyMS,
		})

		// Kumpulkan bukti ternormalisasi per domain.
		domain := toolDomain(st.Tool)
		if rres.OK {
			h.Evidence.Add(domain, rres.Output.Text)
		} else {
			h.Evidence.Add(domain, "ERROR: "+rres.Error)
		}
	}

	// Korelasi bukti lintas sistem (deterministik, UNKNOWN tidak ditebak).
	h.Conclusion = h.Evidence.Correlate("billing", "radius", "mikrotik", "genieacs")
	record(Step{Kind: "thought", Text: fmt.Sprintf(
		"[workflow] korelasi: %s (%.0f%%) area=%s missing=%v",
		h.Conclusion.Diagnosis, h.Conclusion.Confidence*100,
		h.Conclusion.PrimaryArea, h.Conclusion.Missing)})
	return h
}

// buildWorkflowBalasan menyusun prompt bagi LLM untuk menulis balasan manusiawi
// dari bukti yang sudah dikumpulkan secara deterministik. LLM TIDAK memutuskan
// langkah diagnosis — hanya merapikan bahasa untuk pelanggan.
func (e *Engine) buildWorkflowBalasan(identity, query string, h hasilWorkflow) (balasan, teknis string) {
	var b strings.Builder
	b.WriteString("Anda adalah NOC Sentinel. Bukti diagnosis di bawah SUDAH dikumpulkan oleh workflow deterministik — jangan menjalankan langkah baru.\n\n")
	b.WriteString("KELUHAN PELANGGAN: " + query + "\n\n")
	b.WriteString("HASIL KORELASI (dari kode):\n")
	fmt.Fprintf(&b, "- Diagnosis: %s\n", h.Conclusion.Diagnosis)
	fmt.Fprintf(&b, "- Keyakinan: %.0f%%\n", h.Conclusion.Confidence*100)
	if len(h.Conclusion.Missing) > 0 {
		fmt.Fprintf(&b, "- Data belum tersedia: %s\n", strings.Join(h.Conclusion.Missing, ", "))
	}
	b.WriteString("\nBUKTI PER LANGKAH:\n")
	for _, s := range h.Steps {
		if s.Kind == "tool" {
			status := "ok"
			if !s.OK {
				status = "gagal"
			}
			fmt.Fprintf(&b, "- %s [%s]: %s\n", s.Tool, status, oneLine(s.Output))
		}
	}
	b.WriteString("\nTulis jawaban untuk pelanggan dalam Bahasa Indonesia, ringkas, operasional, dan ramah.\n")
	b.WriteString("Jangan menyebut nama tool/model/internal. Jangan mengarang data yang tidak ada di bukti.\n")
	b.WriteString("Format jawaban persis:\n")
	b.WriteString("BALASAN: <teks untuk pelanggan>\n")
	b.WriteString("VERDICT: <SEHAT|DEGRADASI|GANGGUAN|TIDAK DIKETAHUI>\n")
	b.WriteString("KEYAKINAN: <0-100>\n")
	b.WriteString("AKAR_MASALAH: <satu baris>\n")

	prompt := b.String()

	// Panggil LLM untuk menyusun bahasa manusiawi (tanpa tool).
	msg, err := e.LLM.Chat(context.Background(), []llm.Message{
		{Role: "system", Content: e.buildSystemPrompt()},
		{Role: "user", Content: prompt},
	}, nil)
	if err != nil {
		// Fallback deterministik: susun sendiri dari kesimpulan korelasi.
		teknis = fmt.Sprintf("VERDICT: TIDAK DIKETAHUI\nKEYAKINAN: %.0f\nAKAR_MASALAH: %s",
			h.Conclusion.Confidence*100, h.Conclusion.Diagnosis)
		balasan = fallbackBalasan(h.Conclusion)
		return balasan, teknis
	}

	raw := msg.Content
	b2, t2 := PisahBalasan(raw)
	if b2 == "" {
		b2 = fallbackBalasan(h.Conclusion)
	}
	if t2 == "" {
		t2 = fmt.Sprintf("VERDICT: %s\nKEYAKINAN: %.0f\nAKAR_MASALAH: %s",
			conclVerdict(h.Conclusion), h.Conclusion.Confidence*100, h.Conclusion.Diagnosis)
	}
	return b2, t2
}

// conclVerdict memetakan kesimpulan korelasi ke verdict standar.
func conclVerdict(c correlation.Conclusion) string {
	switch c.Diagnosis {
	case "akun/tagihan tidak aktif", "autentikasi RADIUS gagal",
		"CPE/ONT offline (daya/link fisik)", "sesi PPPoE stale/putus":
		return "GANGGUAN"
	case "bukti tidak cukup untuk diagnosis pasti":
		return "TIDAK DIKETAHUI"
	}
	return "TIDAK DIKETAHUI"
}

// fallbackBalasan menyusun balasan manusiawi deterministik bila LLM gagal.
func fallbackBalasan(c correlation.Conclusion) string {
	switch c.PrimaryArea {
	case "billing":
		return "Mohon maaf, akun Anda terdeteksi tidak aktif. Silakan hubungi bagian tagihan untuk konfirmasi status langganan."
	case "radius":
		return "Mohon maaf, ada kendala autentikasi pada akun Anda. Tim kami sedang memeriksa. Terima kasih atas kesabarannya."
	case "genieacs":
		return "Perangkat ONT/CPE Anda terdeteksi offline. Mohon pastikan perangkat menyala dan kabel optik terpasang dengan benar."
	case "mikrotik":
		return "Sesi koneksi Anda terdeteksi putus. Kami akan melakukan perbaikan segera. Terima kasih."
	default:
		return "Mohon maaf atas gangguannya. Tim kami sedang memeriksa koneksi Anda. Terima kasih atas kesabarannya."
	}
}

// toolDomain mengekstrak domain dari nama tool (billing.get_customer -> billing).
func toolDomain(toolName string) string {
	if i := strings.Index(toolName, "."); i > 0 {
		return toolName[:i]
	}
	return toolName
}

// resolveTemplate mengganti ${var} pada string params dengan nilai variabel.
func resolveTemplate(s string, vars map[string]string) string {
	return templateRe.ReplaceAllStringFunc(s, func(m string) string {
		name := m[2 : len(m)-1]
		if v, ok := vars[name]; ok {
			return v
		}
		return "" // tidak diketahui -> kosong (jangan tebak)
	})
}

// strInList memeriksa apakah string ada dalam daftar.
func strInList(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
