package agent

import (
	"context"
	"strings"

	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/llm"
	"ainoc/internal/router"
	"ainoc/internal/teamscope"
)

// ScopeEvent = satu kejadian pembatas tim (ditolak / identitas ditimpa),
// untuk metrik dan audit. Tidak memuat isi pesan.
type ScopeEvent struct {
	Actor     string // "<nomor> (<peran>)"
	Team      string
	Tool      string
	Mode      string // shadow | on
	Allowed   bool   // false = ditolak
	Rewritten bool   // true = identitas dari model ditimpa
	Reason    string
}

// scopeMode membaca mode cs_scope (nil-aman).
func (e *Engine) scopeMode() config.TeamMode {
	if e == nil || e.Cfg == nil {
		return config.TeamOff
	}
	return e.Cfg.Teams().CSScope
}

// scopeCaller mengembalikan tim dan identitas pelanggan terverifikasi dari
// context. Tanpa caller (pemanggilan operator dari dashboard) -> TeamNOC:
// tidak dibatasi, sama seperti perilaku sebelum fase ini.
func scopeCaller(ctx context.Context) (router.Team, teamscope.Customer, string) {
	c, ok := directory.CallerFrom(ctx)
	if !ok {
		return router.TeamNOC, teamscope.Customer{}, ""
	}
	cust := teamscope.Customer{}
	if c.Customer != nil {
		cust = teamscope.Customer{Username: c.Customer.Username, Phone: c.Customer.Phone}
	}
	return router.TeamFor(c), cust, c.Number + " (" + string(c.Role) + ")"
}

// scopeCheck menerapkan batas tim pada satu pemanggilan tool.
//
//	off    : lolos apa adanya.
//	shadow : dihitung dan dicatat lewat ScopeHook, tetapi LOLOS apa adanya.
//	on     : ditegakkan — ditolak, atau identitas ditimpa.
func (e *Engine) scopeCheck(ctx context.Context, tool string, args map[string]any) (map[string]any, bool, string) {
	mode := e.scopeMode()
	if mode == config.TeamOff {
		return args, true, ""
	}
	team, cust, actor := scopeCaller(ctx)
	if team != router.TeamCS {
		return args, true, ""
	}
	v := teamscope.Check(team, tool, args, cust)
	if (!v.Allowed || v.Rewritten) && e.ScopeHook != nil {
		e.ScopeHook(ScopeEvent{
			Actor: actor, Team: string(team), Tool: tool, Mode: string(mode),
			Allowed: v.Allowed, Rewritten: v.Rewritten, Reason: v.Reason,
		})
	}
	if mode == config.TeamShadow {
		return args, true, ""
	}
	if !v.Allowed {
		return nil, false, v.Reason
	}
	return v.Args, true, ""
}

// filterToolsForCaller menyaring tool yang DIPAPARKAN ke model. Hanya aktif
// pada mode "on" untuk tim CS; selain itu daftar tidak berubah.
func (e *Engine) filterToolsForCaller(ctx context.Context, tools []llm.Tool) []llm.Tool {
	if e.scopeMode() != config.TeamOn {
		return tools
	}
	team, _, _ := scopeCaller(ctx)
	if team != router.TeamCS {
		return tools
	}
	out := make([]llm.Tool, 0, len(tools))
	for _, t := range tools {
		if teamscope.CSAllowsTool(strings.TrimSpace(t.Function.Name)) {
			out = append(out, t)
		}
	}
	return out
}
