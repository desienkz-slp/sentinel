package main

import (
	"log"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/router"
)

// Mode router (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 1):
//
//	off    : tidak menghitung apa pun di luar jalur lama.
//	shadow : router dihitung untuk SETIAP pesan dan dibandingkan dengan jalur
//	         yang BENAR-BENAR menangani; selisih dicatat. Tidak mengubah balasan.
//	on     : seperti shadow, dan tim hasil router dicatat sebagai tim penentu.
//
// Jalur staf-kode sudah memakai router.Route di handleStaffCommand pada semua
// mode (perilaku identik, dikunci golden test). Perubahan perilaku untuk
// pelanggan baru terjadi di Fase 2.

// routerMode mengembalikan mode router saat ini (nil-aman untuk uji).
func (s *Server) routerMode() config.TeamMode {
	if s.cfg == nil {
		return config.TeamOff
	}
	return s.cfg.Teams().Routing
}

// actualPath = jalur yang sebenarnya menangani sebuah pesan.
type actualPath string

const (
	pathCode actualPath = "code" // jalur kode (handleStaffCommand menangani)
	pathLLM  actualPath = "llm"  // diteruskan ke agen LLM
)

// compareRouter membandingkan keputusan router dengan jalur nyata. Dipanggil
// SETELAH pesan ditangani; tidak pernah memengaruhi balasan. Mengembalikan
// true bila keduanya sepakat.
func (s *Server) compareRouter(caller directory.Caller, msg string, actual actualPath) bool {
	mode := s.routerMode()
	if mode == config.TeamOff {
		return true
	}
	dec := router.Route(caller, msg)
	want := pathLLM
	if dec.HandledByCode() {
		want = pathCode
	}
	if want == actual {
		return true
	}
	s.teams.RecordMismatch()
	// Log hanya label — tanpa isi pesan, nomor, atau nama.
	log.Printf("[router] SELISIH mode=%s tim=%s handler=%s router=%s nyata=%s",
		mode, dec.Team, dec.Handler, want, actual)
	return false
}

// recordScopeEvent mencatat penolakan/penimpaan identitas oleh pembatas tim.
// Audit memuat peran, tim, nama tool, dan keputusan — TIDAK memuat argumen,
// nomor pelanggan, atau isi pesan.
func (s *Server) recordScopeEvent(ev agent.ScopeEvent) {
	decision := "ALLOW_REWRITTEN"
	if !ev.Allowed {
		decision = "DENY"
	}
	log.Printf("[batas-tim] mode=%s tim=%s tool=%s keputusan=%s", ev.Mode, ev.Team, ev.Tool, decision)
	s.teams.RecordScope(ev.Allowed, ev.Rewritten)
	if s.aud == nil {
		return
	}
	s.aud.Record(audit.Entry{
		EventType:      "team_scope",
		OccurredAt:     time.Now().UTC(),
		Actor:          ev.Actor,
		Tool:           ev.Tool,
		PolicyDecision: decision,
		Note:           "tim=" + ev.Team + " mode=" + ev.Mode + " " + ev.Reason,
	})
}
