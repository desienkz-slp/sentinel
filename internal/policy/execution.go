package policy

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Role adalah otoritas manusia yang dapat menyetujui atau menjalankan aksi.
type Role string

const (
	RoleNOCSenior Role = "NOC_SENIOR"
	RoleAdmin     Role = "ADMIN"
)

// ActionDefinition mendeklarasikan satu aksi yang boleh melewati execution guard.
// Aksi yang tidak dideklarasikan tidak pernah dapat dieksekusi.
type ActionDefinition struct {
	ID                   string
	Domain               string
	Scope                string
	Permission           Permission
	Risk                 Risk
	RequiredRole         Role
	AllowedByAI          bool
	RequiresApproval     bool
	VerificationRequired bool
	Timeout              time.Duration
	RetryLimit           int
	AuditRequired        bool
}

// ActionRequest adalah aksi konkret beserta parameter yang harus sama dengan
// parameter saat approval dibuat.
type ActionRequest struct {
	CaseID       string
	ActionID     string
	Scope        string
	Parameters   map[string]any
	ApprovalID   string
	ExecutorRole Role
}

// Approval adalah bukti persetujuan yang terikat pada case, aksi, dan digest
// parameter. Nilainya tidak boleh dipakai untuk aksi atau parameter lain.
type Approval struct {
	ID            string
	CaseID        string
	ActionID      string
	ParameterHash string
	ApprovedBy    Role
	ExpiresAt     time.Time
}

// Authorization adalah keluaran gerbang eksekusi yang dapat direkam ke audit.
type Authorization struct {
	Decision Decision
	Reasons  []string
}

// ExecutionGuard menerapkan allowlist, kebijakan, tier risiko, dan approval
// sebelum adapter write diizinkan berjalan.
type ExecutionGuard struct {
	engine    *Engine
	actions   map[string]ActionDefinition
	approvals map[string]Approval
	mu        sync.RWMutex
}

// NewExecutionGuard membangun gerbang dengan allowlist statis. Definisi invalid
// diabaikan sehingga tidak dapat membuka akses secara tidak sengaja.
func NewExecutionGuard(engine *Engine, definitions []ActionDefinition) *ExecutionGuard {
	g := &ExecutionGuard{
		engine:    engine,
		actions:   make(map[string]ActionDefinition, len(definitions)),
		approvals: make(map[string]Approval),
	}
	for _, definition := range definitions {
		definition.ID = strings.TrimSpace(definition.ID)
		definition.Risk = normRisk(definition.Risk)
		definition.Permission = normPermission(definition.Permission)
		definition.Scope = normalizeExplicitScope(definition.Scope)
		if definition.ID == "" || definition.Risk == "" || definition.Permission == PermUnknown || definition.Scope == "" {
			continue
		}
		if (approvalRequired(definition) || definition.Permission != PermRead) && normalizeRole(definition.RequiredRole) == "" {
			continue
		}
		if definition.RetryLimit < 0 {
			definition.RetryLimit = 0
		}
		g.actions[definition.ID] = definition
	}
	return g
}

// Approve menerbitkan approval untuk request persis. Approver harus memiliki
// role yang diwajibkan definisi aksi; HIGH dan CRITICAL selalu NOC Senior.
func (g *ExecutionGuard) Approve(req ActionRequest, approver Role, expiresAt time.Time) (Approval, error) {
	definition, ok := g.action(req.ActionID)
	if !ok {
		return Approval{}, fmt.Errorf("aksi %q tidak ada dalam allowlist", strings.TrimSpace(req.ActionID))
	}
	if !definition.AllowedByAI {
		return Approval{}, fmt.Errorf("aksi %q tidak diizinkan untuk AI", definition.ID)
	}
	if strings.TrimSpace(req.CaseID) == "" {
		return Approval{}, fmt.Errorf("case_id wajib diisi untuk approval")
	}
	if requestScope := normalizeExplicitScope(req.Scope); requestScope == "" || requestScope != definition.Scope {
		return Approval{}, fmt.Errorf("scope aksi tidak cocok dengan allowlist")
	}
	if expiresAt.IsZero() {
		return Approval{}, fmt.Errorf("masa berlaku approval wajib diisi")
	}
	if required := requiredRole(definition); required != "" && normalizeRole(approver) != required {
		return Approval{}, fmt.Errorf("role %s tidak berwenang menyetujui aksi %s", normalizeRole(approver), definition.ID)
	}
	parameterHash, err := hashParameters(req.Parameters)
	if err != nil {
		return Approval{}, err
	}
	id, err := newApprovalID()
	if err != nil {
		return Approval{}, err
	}
	approval := Approval{
		ID:            id,
		CaseID:        strings.TrimSpace(req.CaseID),
		ActionID:      definition.ID,
		ParameterHash: parameterHash,
		ApprovedBy:    normalizeRole(approver),
		ExpiresAt:     expiresAt.UTC(),
	}
	g.mu.Lock()
	g.approvals[approval.ID] = approval
	g.mu.Unlock()
	return approval, nil
}

// Authorize menjalankan seluruh gerbang deterministik. Approval yang tidak ada
// memberi status APPROVAL_REQUIRED, sedangkan approval salah/expired ditolak.
func (g *ExecutionGuard) Authorize(req ActionRequest, now time.Time) Authorization {
	definition, ok := g.action(req.ActionID)
	if !ok {
		return denied("aksi tidak ada dalam allowlist")
	}
	if !definition.AllowedByAI {
		return denied("aksi tidak diizinkan untuk AI")
	}
	if requestScope := normalizeExplicitScope(req.Scope); requestScope == "" || requestScope != definition.Scope {
		return denied("scope aksi tidak cocok dengan allowlist")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}

	policyApprovalRequired := false
	if g.engine != nil {
		policyResult := g.engine.Decide(Request{
			Tool:       definition.ID,
			Permission: definition.Permission,
			Scope:      definition.Scope,
			Risk:       definition.Risk,
		})
		if policyResult.Decision == Deny {
			return Authorization{Decision: Deny, Reasons: append([]string{"ditolak policy engine"}, policyResult.Reasons...)}
		}
		if policyResult.Decision == ApprovalRequired {
			policyApprovalRequired = true
		}
	}

	if approvalRequired(definition) || policyApprovalRequired {
		if strings.TrimSpace(req.ApprovalID) == "" {
			return Authorization{Decision: ApprovalRequired, Reasons: []string{"tier risiko membutuhkan persetujuan manusia"}}
		}
		approval, ok := g.approval(req.ApprovalID)
		if !ok {
			return denied("approval tidak ditemukan")
		}
		if !now.Before(approval.ExpiresAt) {
			return denied("approval telah kedaluwarsa")
		}
		parameterHash, err := hashParameters(req.Parameters)
		if err != nil {
			return denied("parameter aksi tidak dapat divalidasi")
		}
		if approval.CaseID != strings.TrimSpace(req.CaseID) || approval.ActionID != definition.ID || approval.ParameterHash != parameterHash {
			return denied("approval tidak cocok dengan case, aksi, atau parameter")
		}
	}

	if required := requiredRole(definition); required != "" && normalizeRole(req.ExecutorRole) != required {
		return denied(fmt.Sprintf("eksekusi aksi %s membutuhkan role %s", definition.ID, required))
	}
	return Authorization{Decision: Allow}
}

func (g *ExecutionGuard) action(id string) (ActionDefinition, bool) {
	if g == nil {
		return ActionDefinition{}, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	definition, ok := g.actions[strings.TrimSpace(id)]
	return definition, ok
}

func (g *ExecutionGuard) approval(id string) (Approval, bool) {
	if g == nil {
		return Approval{}, false
	}
	g.mu.RLock()
	defer g.mu.RUnlock()
	approval, ok := g.approvals[strings.TrimSpace(id)]
	return approval, ok
}

func approvalRequired(definition ActionDefinition) bool {
	return definition.RequiresApproval || definition.Risk == RiskMedium || definition.Risk == RiskHigh || definition.Risk == RiskCritical
}

func requiredRole(definition ActionDefinition) Role {
	if definition.Risk == RiskHigh || definition.Risk == RiskCritical {
		return RoleNOCSenior
	}
	return normalizeRole(definition.RequiredRole)
}

func normalizeRole(role Role) Role {
	switch strings.ToUpper(strings.TrimSpace(string(role))) {
	case string(RoleNOCSenior):
		return RoleNOCSenior
	case string(RoleAdmin):
		return RoleAdmin
	default:
		return ""
	}
}

func normalizeExplicitScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "single_customer":
		return "single_customer"
	case "multi_customer":
		return "multi_customer"
	default:
		return ""
	}
}

func hashParameters(parameters map[string]any) (string, error) {
	if parameters == nil {
		parameters = map[string]any{}
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return "", fmt.Errorf("parameter aksi tidak dapat diserialisasi: %w", err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func newApprovalID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("buat ID approval: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

func denied(reason string) Authorization {
	return Authorization{Decision: Deny, Reasons: []string{reason}}
}

// VerificationPlan mendefinisikan semua bukti yang wajib lulus setelah aksi.
type VerificationPlan struct {
	RequiredChecks []string
}

// VerificationEvidence adalah hasil satu pemeriksaan sumber kebenaran.
type VerificationEvidence struct {
	Check   string
	Passed  bool
	Summary string
}

// VerificationResult menunjukkan apakah seluruh bukti wajib berhasil. Rencana
// kosong gagal tertutup agar aksi tidak dapat mengklaim selesai tanpa verifikasi.
type VerificationResult struct {
	Passed       bool
	FailedChecks []string
}

// Verify mengevaluasi bukti terhadap rencana verifikasi secara deterministik.
func Verify(plan VerificationPlan, evidence []VerificationEvidence) VerificationResult {
	expected := make([]string, 0, len(plan.RequiredChecks))
	seenExpected := make(map[string]bool, len(plan.RequiredChecks))
	for _, check := range plan.RequiredChecks {
		check = strings.TrimSpace(check)
		if check == "" || seenExpected[check] {
			continue
		}
		seenExpected[check] = true
		expected = append(expected, check)
	}
	if len(expected) == 0 {
		return VerificationResult{Passed: false, FailedChecks: []string{"verification_plan"}}
	}
	type outcome struct {
		passed bool
		failed bool
	}
	outcomes := make(map[string]outcome, len(evidence))
	for _, item := range evidence {
		check := strings.TrimSpace(item.Check)
		if seenExpected[check] {
			outcome := outcomes[check]
			if item.Passed {
				outcome.passed = true
			} else {
				outcome.failed = true
			}
			outcomes[check] = outcome
		}
	}
	result := VerificationResult{Passed: true}
	for _, check := range expected {
		outcome := outcomes[check]
		if !outcome.passed || outcome.failed {
			result.Passed = false
			result.FailedChecks = append(result.FailedChecks, check)
		}
	}
	return result
}
