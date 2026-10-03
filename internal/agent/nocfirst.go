package agent

import (
	"context"
	"strings"

	"ainoc/internal/claimguard"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/llm"
	"ainoc/internal/registry"
	"ainoc/internal/router"
)

// Fase A: agen NOC tool-first (docs/PLAN_LANJUTAN_DB_DAN_FASE.md).
//
// Masalah: aturan konteks lama melarang tool untuk pesan non-keluhan. Untuk staf
// NOC itu salah: pertanyaan seperti "kenapa billing X begini?" butuh data nyata.
// Aturan baru, hanya untuk tim NOC/CS Lead (staf), dan hanya saat mode on:
//  1. tool READ yang diizinkan jabatan staf dipaparkan walau pesan bukan keluhan;
//  2. jawaban yang memuat klaim fakta sistem TANPA tool call di giliran itu
//     diganti kode (claimguard) menjadi "belum diperiksa".
//
// Mode shadow: klaim dihitung dan dicatat, jawaban TIDAK diubah.
// Pelanggan tidak terpengaruh sama sekali.

// NOCToolEvent = satu kejadian penjaga klaim (tanpa isi jawaban).
type NOCToolEvent struct {
	Team     string
	Mode     string
	Replaced bool
	Reasons  []string
}

func (e *Engine) nocToolMode() config.TeamMode {
	if e == nil || e.Cfg == nil {
		return config.TeamOff
	}
	return e.Cfg.Teams().NOCTools
}

// nocStaff: pengirim staf (bukan pelanggan) dari context. Tanpa caller
// (dashboard operator) -> false: perilaku lama.
func nocStaff(ctx context.Context) (directory.Caller, bool) {
	c, ok := directory.CallerFrom(ctx)
	if !ok || !c.IsStaff {
		return directory.Caller{}, false
	}
	if router.TeamFor(c) == router.TeamCS {
		return directory.Caller{}, false
	}
	return c, true
}

// nocReadTools: tool READ registry yang boleh dipakai jabatan staf ini.
// Tool tulis TIDAK PERNAH dipaparkan lewat jalur ini.
func (e *Engine) nocReadTools(c directory.Caller) []llm.Tool {
	if e.Reg == nil {
		return nil
	}
	all := e.Reg.LLMTools()
	out := make([]llm.Tool, 0, len(all))
	for _, t := range all {
		name := strings.TrimSpace(t.Function.Name)
		rt, ok := e.Reg.Get(name)
		if !ok || strings.ToUpper(string(rt.Permission)) != string(registry.PermRead) {
			continue
		}
		if directory.Authorize(c, name, false).Decision != directory.Allow {
			continue
		}
		out = append(out, t)
	}
	return out
}

// nocToolsFor: tool tambahan untuk pesan non-keluhan dari staf. Kosong bila
// mode bukan on, bukan staf, atau tanpa caller.
func (e *Engine) nocToolsFor(ctx context.Context) []llm.Tool {
	if e.nocToolMode() != config.TeamOn {
		return nil
	}
	c, ok := nocStaff(ctx)
	if !ok {
		return nil
	}
	return e.nocReadTools(c)
}

// guardNOCAnswer menerapkan claimguard pada jawaban akhir staf.
func (e *Engine) guardNOCAnswer(ctx context.Context, answer string, toolUsed bool) string {
	mode := e.nocToolMode()
	if mode == config.TeamOff {
		return answer
	}
	c, ok := nocStaff(ctx)
	if !ok {
		return answer
	}
	out, replaced := claimguard.Guard(answer, toolUsed)
	if replaced && e.NOCHook != nil {
		e.NOCHook(NOCToolEvent{Team: string(router.TeamFor(c)), Mode: string(mode),
			Replaced: mode == config.TeamOn, Reasons: claimguard.Detect(answer).Reasons})
	}
	if mode == config.TeamOn {
		return out
	}
	return answer // shadow: hanya dicatat
}
