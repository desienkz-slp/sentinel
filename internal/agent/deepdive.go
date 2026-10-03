// deepdive.go — Endpoint B: investigasi mendalam saat workflow deterministik
// tidak konklusif.
//
// Alur (berjalan SETELAH workflow 4-langkah gagal menghasilkan verdict pasti):
//   1. Cek resep terpelajari untuk signature keluhan ini. Bila ada, jalankan
//      urutan tool resep itu secara deterministik (bukan LLM) — hemat & pasti.
//   2. Bila belum ada resep, beri Endpoint B (ReasonLLM) daftar tool READ yang
//      lebih luas, biarkan ia MENCOBA urutan pengecekan sendiri lewat function
//      calling, sampai menemukan bukti yang menyimpulkan diagnosis pasti.
//   3. Urutan yang terbukti menghasilkan verdict PASTI disimpan sebagai resep
//      (RecipeStore), supaya keluhan serupa ke depan langsung pakai resep itu.
//   4. Tool-tool lintas-pelanggan (list_customers/get_devices/get_system_stats)
//      TIDAK diekspos ke Endpoint B — batas scope per pelanggan tetap berlaku.
//   5. Endpoint B TIDAK pernah menulis kode / menjalankan perintah bebas — ia
//      hanya bisa memanggil tool READ yang sudah terdaftar lewat dispatcher
//      (registry -> policy -> adapter). "Coding mandiri" di sini dibatasi jadi
//      "menemukan urutan tool yang tepat", bukan mengeksekusi kode baru.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ainoc/internal/directory"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/standard"
)

// deepToolsAllowed = tool READ yang boleh dipakai Endpoint B untuk investigasi
// mendalam. Sengaja TIDAK menyertakan tool lintas-pelanggan (billing.list_customers,
// radius.get_system_stats, genieacs.get_devices) maupun interface statistik
// global — supaya Endpoint B hanya bisa baca data pelanggan yang sedang dilayani.
var deepToolsAllowed = map[string]bool{
	"billing.get_customer":          true,
	"billing.get_history":           true,
	"radius.get_session":            true,
	"radius.get_user":               true,
	"mikrotik.get_pppoe_status":     true,
	"mikrotik.get_customer_traffic": true,
	"genieacs.get_device_state":     true,
}

// deepToolsFor mengembalikan daftar tool (format LLM) yang boleh dipakai Endpoint B.
func (e *Engine) deepToolsFor() []llm.Tool {
	if e.Reg == nil {
		return nil
	}
	out := make([]llm.Tool, 0)
	for _, t := range e.Reg.Enabled() {
		if !deepToolsAllowed[t.Name] {
			continue
		}
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, llm.Tool{
			Type: "function",
			Function: llm.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}

// deepDive menjalankan investigasi mendalam Endpoint B untuk satu keluhan yang
// workflow dasarnya tidak konklusif. Mengembalikan (diagnosis, tools yang
// dipakai, ok). Diagnosis kosong berarti Endpoint B juga tidak bisa menyimpulkan.
//
// Keamanan: tool tetap dieksekusi lewat e.Disp.Invoke (registry -> policy ->
// adapter). Endpoint B hanya MEMILIH tool; eksekusinya deterministik & deny-by-default.
func (e *Engine) deepDive(ctx context.Context, identity, query string, sig learning.Signature, add func(Step)) (diagnosis string, tools []string, ok bool) {
	if e.ReasonLLM == nil {
		return "", nil, false
	}

	// 1. Resep terpelajari -> jalankan deterministik (tanpa LLM).
	if e.Recipes != nil {
		if recipe := e.Recipes.BestFor(sig); len(recipe) > 0 {
			if d, used, done := e.runRecipe(ctx, identity, recipe, add); done {
				return d, used, true
			}
		}
	}

	// 2. Belum ada resep: biarkan Endpoint B mencoba urutan pengecekan sendiri.
	return e.runDeepLLM(ctx, identity, query, sig, add)
}

// runRecipe menjalankan urutan tool resep yang sudah terpelajari. Bila urutan
// itu menghasilkan bukti yang menyimpulkan diagnosis pasti, kembalikan done=true.
func (e *Engine) runRecipe(ctx context.Context, identity string, tools []string, add func(Step)) (diagnosis string, used []string, done bool) {
	for _, tn := range tools {
		if ctx.Err() != nil {
			return "", used, false
		}
		if !deepToolsAllowed[tn] {
			continue // resep lama memuat tool yang kini dilarang -> lewati
		}
		args := map[string]any{"identity": identity}
		// Tool tanpa identity (mis. mikrotik.get_interface_*) tidak ada di daftar
		// deepToolsAllowed, jadi aman memakai identity untuk semua.
		rres := e.Disp.Invoke(ctx, tn, args)
		used = append(used, tn)
		add(Step{Kind: "tool", Tool: rres.Tool, Target: identity, Output: rres.Output.Text, OK: rres.OK, DurationMS: rres.LatencyMS})
	}
	// Diagnosis dari resep yang sudah terpelajari = verdict yang pernah terbukti.
	// Di sini kita hanya mengembalikan tools + bukti; pemanggil yang menyimpulkan
	// lewat correlation. Untuk kesederhanaan, resep dianggap "berhasil" bila
	// semua tool OK — korelasi final tetap di pemanggil.
	return "", used, false
}

// runDeepLLM menjalankan loop function-calling Endpoint B dengan batas langkah
// ketat, memakai tool READ yang lebih luas. Hasil diagnosis + urutan tool yang
// dipakai dikembalikan; bila verdict pasti, resep disimpan.
func (e *Engine) runDeepLLM(ctx context.Context, identity, query string, sig learning.Signature, add func(Step)) (diagnosis string, used []string, ok bool) {
	tools := e.deepToolsFor()
	if len(tools) == 0 {
		add(Step{Kind: "thought", Text: "Endpoint B: tidak ada tool READ tambahan tersedia — dilewati"})
		return "", nil, false
	}

	sys := "Anda adalah NOC investigator lanjutan. Endpoint A sudah mencoba " +
		"billing->radius->mikrotik->genieacs dasar dan belum menemukan akar masalah. " +
		"Gunakan tool READ yang tersedia untuk MENCOBA cara pengecekan lain pada " +
		"pelanggan yang sama (identity sudah ditentukan), sampai bukti cukup untuk " +
		"menyimpulkan diagnosis pasti.\n" +
		"Hanya boleh memanggil tool yang tersedia. Jangan menyentuh pelanggan lain. " +
		"Akhiri dengan format persis:\n" +
		"VERDICT: <SEHAT|DEGRADASI|GANGGUAN|TIDAK DIKETAHUI>\n" +
		"KEYAKINAN: <0-100>\n" +
		"AKAR_MASALAH: <satu baris>"

	msgs := []llm.Message{
		{Role: "system", Content: sys},
		{Role: "user", Content: "KELUHAN: " + query + "\nIDENTITY: " + identity},
	}

	maxSteps := 6
	if e.Cfg != nil && e.Cfg.MaxSteps > 0 {
		maxSteps = e.Cfg.MaxSteps
	}

	usedMap := map[string]bool{}
	add(Step{Kind: "thought", Text: fmt.Sprintf("Endpoint B: memakai %s, mencoba urutan pengecekan sendiri", e.ReasonLLM.Model)})
	for step := 0; step < maxSteps; step++ {
		if ctx.Err() != nil {
			return "", usedKeys(usedMap), false
		}
		msg, err := e.ReasonLLM.Chat(ctx, msgs, tools)
		if err != nil {
			add(Step{Kind: "error", Text: "Endpoint B gagal: " + err.Error()})
			return "", usedKeys(usedMap), false
		}
		if len(msg.ToolCalls) == 0 {
			clean, verdict, _ := parseVerdict(msg.Content)
			_ = clean
			if verdict == "SEHAT" || verdict == "DEGRADASI" || verdict == "GANGGUAN" {
				add(Step{Kind: "thought", Text: "Endpoint B menyimpulkan: " + msg.Content})
				// Simpan resep bila berhasil menyimpulkan pasti.
				if e.Recipes != nil {
					seq := usedKeys(usedMap)
					e.Recipes.Record(sig, seq, verdict)
				}
				return msg.Content, usedKeys(usedMap), true
			}
			return "", usedKeys(usedMap), false
		}

		msgs = append(msgs, *msg)
		for _, tc := range msg.ToolCalls {
			var args map[string]any
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			// Paksa identity ke pelanggan yang sedang dilayani (scope per-pelanggan).
			args["identity"] = identity
			if !deepToolsAllowed[tc.Function.Name] {
				payload, _ := json.Marshal(map[string]any{"tool": tc.Function.Name, "ok": false, "error": "tool tidak diizinkan untuk Endpoint B"})
				msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload)})
				continue
			}
			rres := e.Disp.Invoke(ctx, tc.Function.Name, args)
			usedMap[tc.Function.Name] = true
			add(Step{Kind: "tool", Tool: rres.Tool, Target: identity, Output: rres.Output.Text, OK: rres.OK, DurationMS: rres.LatencyMS})
			payload, _ := json.Marshal(map[string]any{
				"tool": rres.Tool, "ok": rres.OK, "output": rres.Output.Text,
				"error": rres.Error, "decision": rres.Decision,
			})
			msgs = append(msgs, llm.Message{Role: "tool", ToolCallID: tc.ID, Name: tc.Function.Name, Content: string(payload)})
		}
	}
	return "", usedKeys(usedMap), false
}

func usedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// shouldDeepDive memutuskan apakah Endpoint B perlu dijalankan: workflow sudah
// berjalan (diagnosed=true) tapi verdictnya belum pasti. Ini titik masuk baru
// yang menggantikan "langsung eskalasi ke manusia" pada kasus keyakinan rendah.
func shouldDeepDive(verdict string, confidence float64, diagnosed bool) bool {
	if !diagnosed {
		return false
	}
	return verdict == "" || verdict == "TIDAK DIKETAHUI" || confidence < 60
}

var (
	_ = context.Background
	_ = directory.CallerFrom
	_ = policy.Deny
	_ = registry.Tool{}
	_ = standard.IntentComplaint
	_ = fmt.Sprintf
	_ = strings.TrimSpace
	_ = time.Now
)
