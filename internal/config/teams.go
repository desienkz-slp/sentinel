package config

import "strings"

// Mode tim CS/NOC (docs/PLAN_DUA_TIM_CS_NOC.md). Semua default OFF: tanpa
// konfigurasi, perilaku sama persis dengan versi sebelumnya.

// TeamMode = pengendali bertingkat sebuah fitur tim.
//
//	off    : fitur mati; jalur lama yang bekerja.
//	shadow : fitur DIHITUNG dan DICATAT, tetapi jalur lama tetap yang membalas.
//	on     : fitur aktif.
type TeamMode string

const (
	TeamOff    TeamMode = "off"
	TeamShadow TeamMode = "shadow"
	TeamOn     TeamMode = "on"
)

// NormalizeTeamMode memetakan nilai sembarang ke mode sah. Nilai kosong atau
// tak dikenal -> off (gagal tertutup: salah ketik tak boleh menyalakan fitur).
func NormalizeTeamMode(v string) TeamMode {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "on", "aktif", "true", "1":
		return TeamOn
	case "shadow", "bayangan":
		return TeamShadow
	default:
		return TeamOff
	}
}

// Valid melaporkan apakah v sudah berupa mode sah (untuk validasi input UI).
func (m TeamMode) Valid() bool { return m == TeamOff || m == TeamShadow || m == TeamOn }

// TeamFlags dikelompokkan agar mudah diteruskan ke router dan wiring.
type TeamFlags struct {
	Routing   TeamMode // router intent memilih tim (Fase 1)
	CSScope   TeamMode // pembatas data-milik-pelanggan untuk CS (Fase 2)
	Handoff   TeamMode // serah-terima CS<->NOC terstruktur (Fase 4)
	Severity  TeamMode // pemetaan P1..P4 (Fase 5)
	Presenter TeamMode // lapisan presentasi/penyaring keluaran (Fase 3)
	NOCTools  TeamMode // agen NOC tool-first (Fase A)
}

// Teams mengembalikan flag yang sudah dinormalisasi.
func (c *Config) Teams() TeamFlags {
	return TeamFlags{
		Routing:   NormalizeTeamMode(c.TeamRouting),
		CSScope:   NormalizeTeamMode(c.TeamCSScope),
		Handoff:   NormalizeTeamMode(c.TeamHandoff),
		Severity:  NormalizeTeamMode(c.TeamSeverity),
		Presenter: NormalizeTeamMode(c.TeamPresenter),
		NOCTools:  NormalizeTeamMode(c.TeamNOCTools),
	}
}
