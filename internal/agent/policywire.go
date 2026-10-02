// policywire.go — FASE 3: menghubungkan Policy/Risk Engine (internal/policy)
// ke jalur eksekusi runtime (LLM loop + workflow deterministik).
//
// Aturan yang ditegakkan DI KODE (bukan prompt):
//
//   - Setiap usulan aksi AI lewat gerbang ini SEBELUM eksekusi apa pun.
//   - Deny-by-default: aksi di luar allowlist / ditolak policy TIDAK dieksekusi.
//   - Hanya keputusan ALLOW (read-only berisiko LOW) yang boleh jalan.
//   - APPROVAL_REQUIRED / DENY (MEDIUM/HIGH/CRITICAL atau write tanpa approval)
//     → case pindah ke ESCALATION (handoff manusia, fase 2), TIDAK dieksekusi
//     otomatis dan TIDAK didelegasikan ke model lain (phase 0).
//
// Alur case sesuai master spec: ACTION_PROPOSED → POLICY_CHECK → EXECUTING
// (ALLOW) | ESCALATION (selain ALLOW). Pembacaan diagnostik LOW yang dieksekusi
// inline tetap bagian REASONING dan tidak menyentuh state aksi.
package agent

import (
	"strings"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
)

// PolicyGate adalah satu-satunya jalur keputusan eksekusi untuk aksi yang
// diusulkan AI/workflow. Ia memegang ExecutionGuard (allowlist + approval
// binding), registry (metadata tool), CaseWire (state machine), dan audit.
type PolicyGate struct {
	guard *policy.ExecutionGuard
	reg   *registry.Registry
	casew *CaseWire
	aud   *audit.Store
}

// NewPolicyGate membangun gerbang. Guard nil berarti deny-by-default total:
// Check mengembalikan DENY untuk SEMUA usulan (aman, tidak ada yang jalan).
func NewPolicyGate(guard *policy.ExecutionGuard, reg *registry.Registry, casew *CaseWire, aud *audit.Store) *PolicyGate {
	return &PolicyGate{guard: guard, reg: reg, casew: casew, aud: aud}
}

// ActionDefinitions membangun allowlist aksi ExecutionGuard dari:
//   - registry tools (semua terdaftar, aktif atau tidak — keputusan aktif tetap
//     di gerbang registry dispatcher),
//   - tool diagnostik bawaan (read-only LOW).
//
// Pemetaan peran: tool non-READ wajib punya RequiredRole. Domain jaringan
// (mikrotik/radius/genieacs) → NOC_SENIOR, sisanya (billing) → ADMIN.
// HIGH/CRITICAL selalu dipaksa NOC_SENIOR oleh ExecutionGuard sendiri.
func ActionDefinitions(regTools []registry.Tool, readOnlyBuiltins []string) []policy.ActionDefinition {
	defs := make([]policy.ActionDefinition, 0, len(regTools)+len(readOnlyBuiltins))
	for _, t := range regTools {
		perm := policy.Permission(string(t.Permission))
		risk := policy.Risk(string(t.Risk))
		scope := strings.TrimSpace(t.Scope)
		if scope == "" {
			scope = "single_customer"
		}
		def := policy.ActionDefinition{
			ID:         t.Name,
			Domain:     t.Domain,
			Scope:      scope,
			Permission: perm,
			Risk:       risk,
			// AI boleh MENGUSULKAN aksi yang dideklarasikan — keputusan eksekusi
			// tetap mutlak di Authorize (policy + tier risiko + approval).
			AllowedByAI:          true,
			RequiresApproval:     strings.EqualFold(strings.TrimSpace(t.Approval), "required"),
			VerificationRequired: strings.TrimSpace(t.Verification) != "",
			AuditRequired:        true,
		}
		if perm != policy.PermRead || risk == policy.RiskMedium || risk == policy.RiskHigh || risk == policy.RiskCritical {
			def.RequiredRole = actionRole(t.Domain)
		}
		defs = append(defs, def)
	}
	for _, name := range readOnlyBuiltins {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		defs = append(defs, policy.ActionDefinition{
			ID:            name,
			Domain:        "diag",
			Scope:         "single_customer",
			Permission:    policy.PermRead,
			Risk:          policy.RiskLow,
			AllowedByAI:   true,
			AuditRequired: true,
		})
	}
	return defs
}

// actionRole memetakan domain tool ke otoritas yang berwenang menyetujui.
// Domain jaringan → NOC Senior; selain itu → Admin (selaras escalation.Target).
func actionRole(domain string) policy.Role {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "network", "networking", "internet", "radius", "mikrotik", "genieacs", "diag":
		return policy.RoleNOCSenior
	default:
		return policy.RoleAdmin
	}
}

// Check mengevaluasi satu usulan aksi AI secara deterministik. TIDAK PERNAH
// mengeksekusi apa pun — hanya keputusan. Decision ALLOW hanya untuk aksi
// read-only berisiko LOW yang dinyatakan ALLOW oleh policy engine; segala
// sisanya (APPROVAL_REQUIRED / DENY) wajib di-eskalasi ke manusia.
func (g *PolicyGate) Check(caseID, tool string, args map[string]any) policy.Authorization {
	if g == nil || g.guard == nil {
		return policy.Authorization{Decision: policy.Deny, Reasons: []string{"policy gate belum siap — deny-by-default"}}
	}
	scope := "single_customer"
	if g.reg != nil {
		if t, ok := g.reg.Get(tool); ok && strings.TrimSpace(t.Scope) != "" {
			scope = t.Scope
		}
	}
	return g.guard.Authorize(policy.ActionRequest{
		CaseID:     strings.TrimSpace(caseID),
		ActionID:   tool,
		Scope:      scope,
		Parameters: args,
		// AI tidak pernah punya peran otoritas manusia; executor role hanya
		// terisi pada eksekusi operator dengan approval yang sah.
	}, time.Now().UTC())
}

// Escalate mendorong case lewat ACTION_PROPOSED → POLICY_CHECK → ESCALATION
// dan mencatat keputusan kebijakan ke audit append-only. Idempotent: transisi
// ilegal hanya dicatat ke log, tidak pernah panic. Mengembalikan snapshot case
// SETELAH transisi supaya pemanggil bisa menyegarkan Report.
func (g *PolicyGate) Escalate(identity, caseID, tool string, auth policy.Authorization) CaseSnapshot {
	status := string(auth.Decision)
	reasons := strings.Join(auth.Reasons, "; ")
	if reasons == "" {
		reasons = "kebijakan menahan aksi — butuh keputusan manusia"
	}
	if g == nil {
		return CaseSnapshot{CaseID: strings.TrimSpace(caseID)}
	}
	snap := CaseSnapshot{CaseID: strings.TrimSpace(caseID)}
	if g.casew != nil {
		snap = g.casew.Snapshot(identity)
		if snap.CaseID != "" {
			caseID = snap.CaseID
		}
		g.casew.onPolicyBlock(identity, tool, status, reasons)
		snap = g.casew.Snapshot(identity)
	}
	if g.aud != nil {
		g.aud.Record(audit.Entry{
			EventType:       "policy_decision",
			Actor:           "policy_gate",
			CaseID:          strings.TrimSpace(caseID),
			Tool:            tool,
			PolicyDecision:  status,
			ExecutionStatus: "NOT_EXECUTED",
			After: map[string]any{
				"reasons": auth.Reasons,
				"route":   "escalation",
			},
			Note: "aksi AI ditahan policy gate — eskalasi ke manusia, tidak dieksekusi",
		})
	}
	if g.casew != nil {
		return snap
	}
	return CaseSnapshot{CaseID: strings.TrimSpace(caseID)}
}

// RequiresHuman melaporkan apakah report memuat langkah kebijakan yang ditahan
// (APPROVAL_REQUIRED/DENY) — sinyal bahwa eskalasi harus ke manusia.
func (g *PolicyGate) RequiresHuman(steps []Step) bool {
	if g == nil {
		return false
	}
	for _, s := range steps {
		if s.Kind == "policy" && !s.OK {
			return true
		}
	}
	return false
}
