package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/escalation"
	"ainoc/internal/handoff"
)

// Serah-terima CS <-> NOC (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 4).
//
//	off    : tidak ada buku serah-terima; perilaku eskalasi lama.
//	shadow : handoff DICATAT saat eskalasi dan perintah staf dikenali, tetapi
//	         TIDAK ada pesan ke pelanggan.
//	on     : seperti shadow, dan pembaruan staf yang membawa `untuk_pelanggan`
//	         dikirim ke pelanggan (disaring penyaring presentasi).
//
// Pelanggan HANYA menerima kalimat `untuk_pelanggan` yang ditulis staf. Isi
// teknis, nama staf, dan bukti per sistem tidak pernah ikut.

func (s *Server) handoffMode() config.TeamMode {
	if s.cfg == nil {
		return config.TeamOff
	}
	return s.cfg.Teams().Handoff
}

// openHandoff mencatat handoff saat kasus dieskalasi. Idempoten per CaseID.
// Tidak pernah memblokir atau mengubah balasan/eskalasi lama.
func (s *Server) openHandoff(rep agent.Report, identity, domain string) {
	if s.handoffMode() == config.TeamOff || s.ho == nil || rep.CaseState != escalationState || rep.CaseID == "" {
		return
	}
	ev := map[string]string{}
	for _, sys := range handoff.Systems {
		if v := statusFromSteps(rep.Steps, sys); v != "" {
			ev[sys] = v
		}
	}
	h := handoff.Handoff{
		CaseID:    rep.CaseID,
		Customer:  identity,
		Domain:    domain,
		Complaint: rep.Query,
		Evidence:  ev,
	}
	opened, err := s.ho.Open(h)
	if err != nil {
		log.Printf("[handoff] GAGAL simpan case %s: %v", rep.CaseID, err)
	}
	if !opened {
		return
	}
	log.Printf("[handoff] dibuka case=%s domain=%s mode=%s", rep.CaseID, domain, s.handoffMode())
	s.teams.RecordHandoff("opened")
	s.auditHandoff("handoff_open", "agent", rep.CaseID, "domain="+domain)
}

func (s *Server) auditHandoff(event, actor, caseID, note string) {
	if s.aud == nil {
		return
	}
	s.aud.Record(audit.Entry{EventType: event, OccurredAt: time.Now().UTC(), Actor: actor, CaseID: caseID, Note: note})
}

// canUpdateHandoff: siapa yang boleh memperbarui/menutup kasus. Hanya staf.
// Domain network -> NOC Senior atau Super Admin; selain itu -> Admin atau
// Super Admin (aturan penerima eskalasi yang sama dengan escalation.Target).
func canUpdateHandoff(c directory.Caller, domain string) bool {
	if !c.IsStaff {
		return false
	}
	if c.Role == directory.RoleSuperAdmin {
		return true
	}
	if escalation.Target(domain) == escalation.RoleNOCSenior {
		return c.Role == directory.RoleNOCSenior
	}
	return c.Role == directory.RoleAdmin
}

// handoffCommandReply menangani "tutup/update CASE-... [pesan]" dari staf.
// Mengembalikan balasan untuk STAF (bukan pelanggan).
func (s *Server) handoffCommandReply(ctx context.Context, caller directory.Caller, cmd directory.HandoffCmd) string {
	if s.handoffMode() == config.TeamOff || s.ho == nil {
		return "Fitur serah-terima kasus belum diaktifkan (Pengaturan → Mode Tim → handoff)."
	}
	actor := caller.Number + " (" + string(caller.Role) + ")"
	h, ok := s.ho.Get(cmd.CaseID)
	if !ok {
		s.auditHandoff("handoff_update_denied", actor, cmd.CaseID, "kasus tidak ditemukan")
		return "Kasus " + cmd.CaseID + " tidak ditemukan. Periksa ID-nya."
	}
	if !canUpdateHandoff(caller, h.Domain) {
		s.auditHandoff("handoff_update_denied", actor, cmd.CaseID, "peran tidak berwenang untuk domain "+h.Domain)
		return fmt.Sprintf("Peran %s tidak berwenang memperbarui kasus domain %s.", caller.Role.Label(), h.Domain)
	}

	upd, idx, err := s.ho.Apply(cmd.CaseID, string(caller.Role), handoff.Status(cmd.Status), cmd.Untuk)
	switch {
	case errors.Is(err, handoff.ErrNeedMessage):
		return "Penutupan kasus wajib menyertakan pesan untuk pelanggan.\nContoh: tutup " + cmd.CaseID + " Gangguan sudah diperbaiki, silakan dicoba kembali."
	case errors.Is(err, handoff.ErrClosed):
		return "Kasus " + cmd.CaseID + " sudah ditutup."
	case errors.Is(err, handoff.ErrDuplicate):
		return "Pembaruan yang sama sudah tercatat; tidak dikirim ulang ke pelanggan."
	case err != nil:
		return "Gagal memperbarui kasus: " + oneLineWA(err.Error())
	}
	s.teams.RecordHandoff("updated")
	s.auditHandoff("handoff_update", actor, cmd.CaseID, "status="+cmd.Status)

	if cmd.Untuk == "" {
		return fmt.Sprintf("Kasus %s diperbarui: %s. Tidak ada pesan untuk pelanggan.", cmd.CaseID, cmd.Status)
	}
	if s.handoffMode() != config.TeamOn {
		return fmt.Sprintf("Kasus %s diperbarui: %s. (mode bayangan: pesan ke pelanggan TIDAK dikirim)", cmd.CaseID, cmd.Status)
	}
	if err := s.notifyCustomer(ctx, upd, idx, cmd.Untuk); err != nil {
		return fmt.Sprintf("Kasus %s diperbarui: %s, tetapi pesan ke pelanggan GAGAL terkirim (%s).", cmd.CaseID, cmd.Status, oneLineWA(err.Error()))
	}
	return fmt.Sprintf("Kasus %s diperbarui: %s. Pesan sudah dikirim ke pelanggan.", cmd.CaseID, cmd.Status)
}

// notifyCustomer mengirim HANYA kalimat untuk_pelanggan, disaring penyaring
// presentasi (nomor/host/kredensial/nama staf tersunting). Sekali per Update.
func (s *Server) notifyCustomer(ctx context.Context, h handoff.Handoff, idx int, untuk string) error {
	if s.wa == nil || !s.wa.Enabled() {
		_ = s.ho.MarkNotified(h.CaseID, idx, "gateway nonaktif")
		s.teams.RecordHandoff("notify_failed")
		return errors.New("gateway WhatsApp nonaktif")
	}
	cust := directory.Caller{Number: h.Customer, Role: directory.RoleCustomer, IsCustomer: true}
	txt := s.presentReplyForce(cust, untuk)

	sendCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.WATimeout)*time.Second)
	defer cancel()
	if _, err := s.wa.Send(sendCtx, h.Customer, txt); err != nil {
		_ = s.ho.MarkNotified(h.CaseID, idx, err.Error())
		s.teams.RecordHandoff("notify_failed")
		s.auditHandoff("handoff_notify", "system", h.CaseID, "GAGAL")
		log.Printf("[handoff] GAGAL kabari pelanggan case=%s: %v", h.CaseID, err)
		return err
	}
	_ = s.ho.MarkNotified(h.CaseID, idx, "")
	s.teams.RecordHandoff("notified")
	s.auditHandoff("handoff_notify", "system", h.CaseID, "terkirim")
	log.Printf("[handoff] pelanggan dikabari case=%s", h.CaseID)
	return nil
}
