// escalation_wire.go — FASE 2: menghubungkan Case Engine (ESCALATION) ke alur
// WhatsApp supaya case ber-state ESCALATION benar-benar mengirim handoff lengkap
// ke nomor manusia yang tepat:
//
//   - domain network  -> NOC Senior (cfg.NOCNumber)
//   - domain billing  -> Admin      (cfg.AdminNumber)
//
// Keamanan fase ini:
//   - Hanya terpicu saat rep.CaseState == "ESCALATION".
//   - Dedup per CaseID: satu case hanya mengirim handoff SEKALI (anti spam).
//   - Audit append-only: nomor tujuan, case, domain, kapan, hasil kirim.
//   - Balasan pelanggan TIDAK disentuh — handoff adalah pesan TERPISAH ke NOC/Admin.
package main

import (
	"context"
	"log"
	"strings"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/escalation"
)

// escalationState adalah nilai case yang memicu handoff (harus sama dengan
// caseengine.StateEscalation; disalin sebagai string agar package ini tidak
// mengimpor caseengine lagi).
const escalationState = "ESCALATION"

// buildEscalationContext menyusun Context handoff dari Report agen + identitas
// pengirim. Tidak boleh mengarang: nilai yang tidak tersedia dikosongkan supaya
// FormatHandoff menampilkan "—". Evidence diambil dari langkah-langkah tool yang
// benar-benar dijalankan, tanpa isi internal prompt/kredensial.
func buildEscalationContext(rep agent.Report, identity, domain string) escalation.Context {
	role := escalation.Target(domain)

	ctx := escalation.Context{
		CaseID:              rep.CaseID,
		Domain:              domain,
		Customer:            identity,
		Complaint:           rep.Query,
		ConversationSummary: rep.Query,
		Timeline:            rep.StartedAt.Format("02 Jan 15:04"),
		Hypothesis:          rep.Verdict,
		Confidence:          confidenceLabel(rep.Confidence),
		Recommendation:      rep.Answer,
		Reason:              "verdict tidak pasti / keyakinan rendah — butuh keputusan manusia",
		HumanNeed:           "review hasil diagnosis dan tentukan tindakan lanjutan",
		Urgency:             "NORMAL",
		AffectedScope:       "satu pelanggan",
	}

	// Diagnostik + bukti dari langkah tool yang benar-benar dijalankan.
	var diagnostics []string
	var evidence []string
	var performed []string
	for _, s := range rep.Steps {
		switch s.Kind {
		case "tool":
			line := strings.TrimSpace(s.Tool)
			if s.Target != "" {
				line += " " + s.Target
			}
			if s.OK {
				line += " ✓"
				evidence = append(evidence, oneLineWA(s.Output))
			} else {
				line += " ✗"
				evidence = append(evidence, "gagal: "+oneLineWA(s.Output))
			}
			diagnostics = append(diagnostics, line)
			performed = append(performed, "diagnostik read-only: "+s.Tool)
		case "escalate":
			evidence = append(evidence, "eskalasi analisis otomatis: "+oneLineWA(s.Text))
		}
	}
	ctx.Diagnostics = diagnostics
	ctx.Evidence = evidence
	ctx.ActionsPerformed = performed
	ctx.ActionsNotPerformed = []string{"tindakan perubahan konfigurasi / write (belum diizinkan tanpa approval)"}

	// Status per-sistem bila tersedia di bukti terstruktur (hanya untuk NOC Senior).
	if role == escalation.RoleNOCSenior {
		ctx.BillingStatus = statusFromSteps(rep.Steps, "billing")
		ctx.RadiusStatus = statusFromSteps(rep.Steps, "radius")
		ctx.RouterStatus = statusFromSteps(rep.Steps, "mikrotik")
		ctx.GenieACSStatus = statusFromSteps(rep.Steps, "genieacs")
	}

	return ctx
}

// statusFromSteps mengambil ringkasan hasil tool satu domain dari langkah
// diagnosis, untuk diisi ke field status per sistem pada handoff NOC Senior.
func statusFromSteps(steps []agent.Step, domain string) string {
	for _, s := range steps {
		if s.Kind == "tool" && strings.HasPrefix(s.Tool, domain+".") {
			out := oneLineWA(s.Output)
			if out == "" {
				out = "diperiksa"
			}
			if !s.OK {
				return "GAGAL: " + out
			}
			return out
		}
	}
	return ""
}

// confidenceLabel memetakan confidence (0-100) ke label manusia sederhana.
func confidenceLabel(conf float64) string {
	switch {
	case conf <= 0:
		return "TIDAK DIKETAHUI"
	case conf < 40:
		return "RENDAH"
	case conf < 70:
		return "MEDIUM"
	default:
		return "TINGGI"
	}
}

// escalationTarget memetakan domain eskalasi ke nomor WhatsApp tujuan. Nomor
// kosong berarti authority itu belum dikonfigurasi — handoff tidak dikirim.
func (s *Server) escalationTarget(domain string) string {
	switch escalation.Target(domain) {
	case escalation.RoleNOCSenior:
		return strings.TrimSpace(s.cfg.NOCNumber)
	default:
		return strings.TrimSpace(s.cfg.AdminNumber)
	}
}

// deliverEscalation mengirim handoff ke nomor authority yang tepat, dengan dedup
// per CaseID + audit append-only. Idempotent: dipanggil berulang pun hanya
// mengirim sekali per case. Tidak pernah memblokir balasan pelanggan.
//
// domain adalah domain eskalasi (hasil ClassifyDomain) — authority dihitung ulang
// dari domain (Target), bukan dipercaya dari pemanggil.
func (s *Server) deliverEscalation(ctx context.Context, rep agent.Report, identity, domain string) {
	if rep.CaseState != escalationState {
		return
	}
	if rep.CaseID == "" {
		return
	}
	role := escalation.Target(domain)
	to := s.escalationTarget(domain)
	handoffCtx := buildEscalationContext(rep, identity, domain)

	// Anti spam: satu case hanya satu handoff.
	if !s.esc.Once(rep.CaseID) {
		log.Printf("[escalation] handoff untuk case %s sudah pernah dikirim — dilewati (anti spam)", rep.CaseID)
		return
	}

	if to == "" {
		log.Printf("[escalation] case %s domain=%s role=%s: nomor tujuan belum dikonfigurasi — handoff tidak dikirim",
			rep.CaseID, domain, role)
		s.recordEscalationAudit(rep, identity, domain, role, to, false, "nomor tujuan kosong (noc_number/admin_number belum diisi)")
		return
	}

	msg := escalation.FormatHandoff(handoffCtx, role)

	// Cegah pengiriman WA nyata berulang selama uji: kirim maksimal satu pesan
	// per proses per case (dedup sudah menangani). Bila gateway WA tidak aktif,
	// tetap catat audit gagal — jangan panic / blokir balasan pelanggan.
	if !s.wa.Enabled() {
		log.Printf("[escalation] case %s domain=%s: gateway WA nonaktif — handoff TIDAK terkirim (tujuan %s)",
			rep.CaseID, domain, to)
		s.recordEscalationAudit(rep, identity, domain, role, to, false, "whatsapp gateway tidak aktif")
		return
	}

	sendCtx, cancel := context.WithTimeout(ctx, time.Duration(s.cfg.WATimeout)*time.Second)
	defer cancel()
	if _, err := s.wa.Send(sendCtx, to, msg); err != nil {
		log.Printf("[escalation] GAGAL kirim handoff case %s ke %s (role=%s): %v", rep.CaseID, to, role, err)
		s.recordEscalationAudit(rep, identity, domain, role, to, false, err.Error())
		return
	}

	log.Printf("[escalation] handoff case %s domain=%s terkirim ke %s (role=%s)", rep.CaseID, domain, to, role)
	s.recordEscalationAudit(rep, identity, domain, role, to, true, "")
}

// recordEscalationAudit mencatat kejadian eskalasi ke audit append-only.
func (s *Server) recordEscalationAudit(rep agent.Report, identity, domain string, role escalation.Role, to string, sent bool, note string) {
	if s.aud == nil {
		return
	}
	status := "SENT"
	if !sent {
		status = "FAILED"
	}
	s.aud.Record(audit.Entry{
		EventType:       "escalation_handoff",
		Actor:           "agent",
		CaseID:          rep.CaseID,
		EntityType:      "escalation",
		EntityID:        rep.CaseID,
		ExecutionStatus: status,
		After: map[string]any{
			"domain":       domain,
			"target_role":  string(role),
			"target_phone": to,
			"customer":     strings.TrimSpace(identity),
			"sent":         sent,
		},
		Note: note,
	})
}

// oneLineWA merapikan satu baris output (hapus newline) untuk pesan WA.
func oneLineWA(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " | ")
	s = strings.ReplaceAll(s, "\n", " | ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 500 {
		return s[:500] + "…"
	}
	return s
}
