package main

import (
	"log"

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
