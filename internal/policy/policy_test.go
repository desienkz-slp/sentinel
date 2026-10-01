package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultDenyByDefault(t *testing.T) {
	e := Load("")
	if e.Mode() != ModeCopilot {
		t.Errorf("mode default = %s, mau COPILOT", e.Mode())
	}
	// WRITE tanpa aturan -> deny
	if r := e.Decide(Request{Tool: "mikrotik.reboot", Permission: PermWrite, Risk: RiskHigh, Scope: "multi_customer"}); r.Decision != Deny {
		t.Errorf("WRITE multi_customer harus DENY, dapat %s", r.Decision)
	}
	// ADMIN -> deny
	if r := e.Decide(Request{Tool: "mikrotik.routing", Permission: PermAdmin, Risk: RiskHigh}); r.Decision != Deny {
		t.Errorf("ADMIN harus DENY, dapat %s", r.Decision)
	}
	// READ -> allow
	if r := e.Decide(Request{Tool: "billing.get_customer", Permission: PermRead, Risk: RiskLow, Scope: "single_customer"}); r.Decision != Allow {
		t.Errorf("READ single_customer harus ALLOW, dapat %s", r.Decision)
	}
}

func TestLoadFileRules(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.yaml")
	content := `version: 1.0.0
mode: COPILOT
default:
  read: allow
  write: deny
  admin: deny
  unknown: deny
rules:
  - id: pppoe-reset
    description: reset PPPoE terkontrol
    match:
      tool: mikrotik.disconnect_pppoe
      permission: WRITE
      scope: single_customer
      risk: MEDIUM
    decision: APPROVAL_REQUIRED
    requires: [incident_id, operator_approval]
  - id: deny-mass
    description: tolak aksi banyak pelanggan
    match:
      scope: multi_customer
      permission: WRITE
    decision: DENY
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	e := Load(path)
	if e.Mode() != ModeCopilot {
		t.Errorf("mode = %s, mau COPILOT", e.Mode())
	}

	// Aturan eksplisit menang.
	r := e.Decide(Request{Tool: "mikrotik.disconnect_pppoe", Permission: PermWrite, Risk: RiskMedium, Scope: "single_customer"})
	if r.Decision != ApprovalRequired {
		t.Errorf("PPPoE reset = %s, mau APPROVAL_REQUIRED", r.Decision)
	}
	if r.RuleID != "pppoe-reset" {
		t.Errorf("rule_id = %q, mau pppoe-reset", r.RuleID)
	}
	// Aksi massal ditolak.
	r = e.Decide(Request{Tool: "mikrotik.disconnect_pppoe", Permission: PermWrite, Risk: RiskMedium, Scope: "multi_customer"})
	if r.Decision != Deny {
		t.Errorf("multi_customer WRITE = %s, mau DENY", r.Decision)
	}
	if r.RuleID != "deny-mass" {
		t.Errorf("rule_id = %q, mau deny-mass", r.RuleID)
	}
}

func TestCopilotForcesApprovalOnWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	// Aturan yang secara eksplisit ALLOW write, tapi mode COPILOT memaksa approval.
	os.WriteFile(path, []byte(`mode: COPILOT
default: {read: allow, write: allow, admin: deny, unknown: deny}
rules:
  - id: allow-single-write
    match: {permission: WRITE, scope: single_customer}
    decision: ALLOW
`), 0o644)
	e := Load(path)
	r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskLow, Scope: "single_customer"})
	if r.Decision != ApprovalRequired {
		t.Errorf("COPILOT + WRITE allow -> harus APPROVAL_REQUIRED, dapat %s", r.Decision)
	}
}

func TestReadOnlyModeDeniesAllMutation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	os.WriteFile(path, []byte(`mode: READ_ONLY
default: {read: allow, write: allow, admin: allow, unknown: deny}
`), 0o644)
	e := Load(path)
	if r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskLow, Scope: "single_customer"}); r.Decision != Deny {
		t.Errorf("READ_ONLY WRITE = %s, mau DENY", r.Decision)
	}
	if r := e.Decide(Request{Tool: "x", Permission: PermRead, Risk: RiskLow}); r.Decision != Allow {
		t.Errorf("READ_ONLY READ = %s, mau ALLOW", r.Decision)
	}
}

func TestRiskGate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	os.WriteFile(path, []byte(`mode: AUTONOMOUS
default: {read: allow, write: allow, admin: deny, unknown: deny}
risk:
  HIGH: {max_scope: 0, approval_required: true}
  MEDIUM: {max_scope: 1, approval_required: true}
  LOW: {max_scope: 1, approval_required: false}
`), 0o644)
	e := Load(path)
	if r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskLow, Scope: "single_customer"}); r.Decision != Allow {
		t.Errorf("AUTONOMOUS LOW = %s, mau ALLOW", r.Decision)
	}
	if r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskMedium, Scope: "single_customer"}); r.Decision != ApprovalRequired {
		t.Errorf("AUTONOMOUS MEDIUM = %s, mau APPROVAL_REQUIRED", r.Decision)
	}
	if r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskHigh, Scope: "single_customer"}); r.Decision != ApprovalRequired {
		t.Errorf("AUTONOMOUS HIGH = %s, mau APPROVAL_REQUIRED", r.Decision)
	}
}

func TestMissingFileDenyByDefault(t *testing.T) {
	e := Load(filepath.Join(t.TempDir(), "tidak-ada.yaml"))
	if r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskLow}); r.Decision != Deny {
		t.Errorf("file hilang + WRITE = %s, mau DENY", r.Decision)
	}
}

func TestUnknownDecisionStringDenies(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "p.yaml")
	os.WriteFile(path, []byte(`mode: AUTONOMOUS
default: {read: allow, write: maybe, admin: deny, unknown: deny}
`), 0o644)
	e := Load(path)
	if r := e.Decide(Request{Tool: "x", Permission: PermWrite, Risk: RiskLow}); r.Decision != Deny {
		t.Errorf("decision string tak dikenal harus DENY, dapat %s", r.Decision)
	}
}
