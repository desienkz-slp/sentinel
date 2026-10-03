package main

import (
	"log"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/presentation"
)

// presentReply menyaring balasan sebelum dikirim, sesuai mode presenter
// (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 3):
//
//	off    : teks dikembalikan apa adanya.
//	shadow : disaring DAN dicatat, tetapi teks ASLI yang dikirim.
//	on     : teks yang sudah disunting yang dikirim.
//
// Hanya balasan ke PELANGGAN (bukan staf) yang disaring: staf memang berhak
// melihat data internal. Satu-satunya pintu keluar balasan LLM — dipakai di
// jalur sinkron dan async supaya tak ada jalur yang lolos tanpa penyaring.
func (s *Server) presentReply(caller directory.Caller, teks string) string {
	mode := config.TeamOff
	if s.cfg != nil {
		mode = s.cfg.Teams().Presenter
	}
	if mode == config.TeamOff || caller.IsStaff || teks == "" {
		return teks
	}

	opt := presentation.Opsi{NomorBoleh: []string{caller.Number}}
	if caller.Customer != nil && caller.Customer.Phone != "" {
		opt.NomorBoleh = append(opt.NomorBoleh, caller.Customer.Phone)
	}
	if s.cfg != nil {
		for _, m := range s.cfg.StaffMembers {
			opt.NamaStaf = append(opt.NamaStaf, m.Name, m.Number)
		}
	}
	h := presentation.SaringPelanggan(teks, opt)
	if !h.Diubah {
		return teks
	}

	kat := ""
	for i, f := range h.Temuan {
		if i > 0 {
			kat += ","
		}
		kat += f.Kategori
	}
	// Log dan audit hanya memuat KATEGORI — tidak pernah isi yang disunting.
	log.Printf("[presentasi] mode=%s penyuntingan kategori=%s", mode, kat)
	s.teams.RecordRedaction(len(h.Temuan))
	if s.aud != nil {
		s.aud.Record(audit.Entry{
			EventType:  "reply_redaction",
			OccurredAt: time.Now().UTC(),
			Actor:      caller.Number + " (" + string(caller.Role) + ")",
			Note:       "mode=" + string(mode) + " kategori=" + kat,
		})
	}
	if mode == config.TeamShadow {
		return teks
	}
	return h.Teks
}
