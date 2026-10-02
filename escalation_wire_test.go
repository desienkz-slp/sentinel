package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/escalation"
)

func TestEscalationTargetMapsDomainToNumber(t *testing.T) {
	s := &Server{cfg: &config.Config{NOCNumber: "628111222333", AdminNumber: "628111222444"}}
	if got := s.escalationTarget("network"); got != "628111222333" {
		t.Fatalf("network target = %q", got)
	}
	if got := s.escalationTarget("billing"); got != "628111222444" {
		t.Fatalf("billing target = %q", got)
	}
}

func TestBuildEscalationContextFillsHandoffFields(t *testing.T) {
	rep := agent.Report{
		CaseID:     "CASE-20261002-ABCDEF",
		CaseState:  "ESCALATION",
		Query:      "internet putus total",
		Verdict:    "TIDAK DIKETAHUI",
		Confidence: 25,
		Answer:     "perlu cek jalur akses",
		StartedAt:  time.Now(),
		Steps: []agent.Step{
			{Kind: "tool", Tool: "mikrotik.get_pppoe_status", Target: "628111", OK: false, Output: "PPPoE down"},
			{Kind: "tool", Tool: "billing.get_customer", OK: true, Output: "ACTIVE"},
		},
	}
	ctx := buildEscalationContext(rep, "628111222333", "network")

	if ctx.CaseID != rep.CaseID {
		t.Fatalf("CaseID = %q", ctx.CaseID)
	}
	if ctx.Customer != "628111222333" {
		t.Fatalf("Customer = %q", ctx.Customer)
	}
	if ctx.Complaint != "internet putus total" {
		t.Fatalf("Complaint = %q", ctx.Complaint)
	}
	if len(ctx.Diagnostics) == 0 || len(ctx.Evidence) == 0 || len(ctx.ActionsPerformed) == 0 {
		t.Fatalf("handoff tidak lengkap: diag=%d evidence=%d performed=%d", len(ctx.Diagnostics), len(ctx.Evidence), len(ctx.ActionsPerformed))
	}
	if ctx.RouterStatus == "" {
		t.Fatalf("RouterStatus harus terisi dari langkah mikrotik")
	}
}

func TestBuildEscalationContextDedupsDuplicateToolSteps(t *testing.T) {
	rep := agent.Report{
		CaseID:     "CASE-20261002-DEDUP",
		CaseState:  "ESCALATION",
		Query:      "internet putus",
		Verdict:    "TIDAK DIKETAHUI",
		Confidence: 20,
		Answer:     "cek lanjut",
		StartedAt:  time.Now(),
		// Batch ganda dipisah langkah "thought" (persis pola workflow deterministik).
		Steps: []agent.Step{
			{Kind: "tool", Tool: "billing.get_customer", OK: false, Output: "dilewati: nonaktif"},
			{Kind: "tool", Tool: "radius.get_session", OK: false, Output: "dilewati: nonaktif"},
			{Kind: "thought", Text: "korelasi"},
			{Kind: "tool", Tool: "billing.get_customer", OK: false, Output: "dilewati: nonaktif"},
			{Kind: "tool", Tool: "radius.get_session", OK: false, Output: "dilewati: nonaktif"},
		},
	}
	ctx := buildEscalationContext(rep, "628111222333", "network")
	if len(ctx.Diagnostics) != 2 {
		t.Fatalf("langkah ganda harus diratakan jadi 2, dapat %d: %v", len(ctx.Diagnostics), ctx.Diagnostics)
	}
	if len(ctx.Evidence) != 2 {
		t.Fatalf("evidence ganda harus diratakan jadi 2, dapat %d", len(ctx.Evidence))
	}
}

func TestDeliverEscalationSkipsNonEscalationState(t *testing.T) {
	s := &Server{
		cfg: &config.Config{NOCNumber: "628111222333"},
		esc: escalation.NewDedup(),
		aud: audit.New("", 10),
		wa:  nil, // nonaktif
	}
	rep := agent.Report{CaseID: "CASE-X", CaseState: "INVESTIGATION"}
	s.deliverEscalation(context.Background(), rep, "628111", "network")
	if s.aud.Len() != 0 {
		t.Fatalf("state non-ESCALATION tidak boleh mencatat audit, dapat %d", s.aud.Len())
	}
}

func TestDeliverEscalationDedupsPerCase(t *testing.T) {
	s := &Server{
		cfg: &config.Config{NOCNumber: "628111222333"},
		esc: escalation.NewDedup(),
		aud: audit.New("", 10),
	}
	rep := agent.Report{CaseID: "CASE-DUP", CaseState: "ESCALATION", Query: "internet putus"}
	// dua kali, same case — hanya satu audit (WA nonaktif -> audit FAILED).
	s.deliverEscalation(context.Background(), rep, "628111", "network")
	s.deliverEscalation(context.Background(), rep, "628111", "network")
	if s.aud.Len() != 1 {
		t.Fatalf("case sama harus di-audit sekali, dapat %d", s.aud.Len())
	}
}

func TestDeliverEscalationRecordsAuditWhenNoTargetNumber(t *testing.T) {
	s := &Server{
		cfg: &config.Config{}, // nomor kosong
		esc: escalation.NewDedup(),
		aud: audit.New("", 10),
	}
	rep := agent.Report{CaseID: "CASE-NONUM", CaseState: "ESCALATION", Query: "internet putus"}
	s.deliverEscalation(context.Background(), rep, "628111", "network")

	entries := s.aud.Recent(5)
	if len(entries) != 1 {
		t.Fatalf("harus ada 1 audit, dapat %d", len(entries))
	}
	e := entries[0]
	if e.EventType != "escalation_handoff" || e.ExecutionStatus != "FAILED" {
		t.Fatalf("audit = %+v", e)
	}
	if !strings.Contains(e.Note, "belum diisi") {
		t.Fatalf("note audit tidak jelas: %q", e.Note)
	}
}
