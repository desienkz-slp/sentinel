package policy

import (
	"testing"
	"time"
)

func TestExecutionGuardMenolakAksiDiLuarAllowlist(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	guard := NewExecutionGuard(nil, []ActionDefinition{
		{ID: "mikrotik.disconnect_pppoe", Scope: "single_customer", Permission: PermWrite, Risk: RiskLow, AllowedByAI: true, RequiredRole: RoleNOCSenior},
	})

	result := guard.Authorize(ActionRequest{
		ActionID:   "mikrotik.reboot",
		Parameters: map[string]any{"username": "pelanggan-uji"},
	}, now)
	if result.Decision != Deny {
		t.Fatalf("aksi di luar allowlist = %s, mau DENY", result.Decision)
	}
}

func TestExecutionGuardMenegakkanTierRisiko(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	guard := NewExecutionGuard(nil, []ActionDefinition{
		{ID: "diagnostic.ping", Scope: "single_customer", Permission: PermRead, Risk: RiskLow, AllowedByAI: true},
		{ID: "mikrotik.disconnect_pppoe", Scope: "single_customer", Permission: PermWrite, Risk: RiskMedium, AllowedByAI: true, RequiredRole: RoleNOCSenior},
		{ID: "mikrotik.change_firewall", Scope: "single_customer", Permission: PermAdmin, Risk: RiskHigh, AllowedByAI: true, RequiredRole: RoleNOCSenior},
		{ID: "mikrotik.delete_route", Scope: "single_customer", Permission: PermAdmin, Risk: RiskCritical, AllowedByAI: true, RequiredRole: RoleNOCSenior},
	})

	for _, tc := range []struct {
		action string
		want   Decision
	}{
		{action: "diagnostic.ping", want: Allow},
		{action: "mikrotik.disconnect_pppoe", want: ApprovalRequired},
		{action: "mikrotik.change_firewall", want: ApprovalRequired},
		{action: "mikrotik.delete_route", want: ApprovalRequired},
	} {
		t.Run(tc.action, func(t *testing.T) {
			got := guard.Authorize(ActionRequest{ActionID: tc.action, Scope: "single_customer"}, now)
			if got.Decision != tc.want {
				t.Fatalf("tier risiko %s = %s, mau %s", tc.action, got.Decision, tc.want)
			}
		})
	}
}

func TestApprovalMengikatAksiDanParameterSertaKedaluwarsa(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	guard := NewExecutionGuard(nil, []ActionDefinition{
		{ID: "mikrotik.disconnect_pppoe", Scope: "single_customer", Permission: PermWrite, Risk: RiskMedium, AllowedByAI: true, RequiredRole: RoleNOCSenior},
	})
	req := ActionRequest{
		CaseID:       "CASE-20261002-ABC123",
		ActionID:     "mikrotik.disconnect_pppoe",
		Scope:        "single_customer",
		ExecutorRole: RoleNOCSenior,
		Parameters: map[string]any{
			"username": "pelanggan-uji",
			"metadata": map[string]any{"router": "router-uji-01", "session": "sesi-uji"},
		},
	}

	approval, err := guard.Approve(req, RoleNOCSenior, now.Add(time.Hour))
	if err != nil {
		t.Fatalf("approval ditolak: %v", err)
	}
	req.ApprovalID = approval.ID
	if result := guard.Authorize(req, now); result.Decision != Allow {
		t.Fatalf("approval untuk aksi+parameter persis = %s, mau ALLOW: %v", result.Decision, result.Reasons)
	}

	altered := req
	altered.Parameters = map[string]any{"username": "pelanggan-lain"}
	if result := guard.Authorize(altered, now); result.Decision != Deny {
		t.Fatalf("approval dengan parameter berbeda = %s, mau DENY", result.Decision)
	}
	if result := guard.Authorize(req, now.Add(time.Hour)); result.Decision != Deny {
		t.Fatalf("approval kedaluwarsa = %s, mau DENY", result.Decision)
	}
}

func TestApprovalMenolakRoleYangTidakBerwenang(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	guard := NewExecutionGuard(nil, []ActionDefinition{
		{ID: "mikrotik.change_firewall", Scope: "single_customer", Permission: PermAdmin, Risk: RiskHigh, AllowedByAI: true, RequiredRole: RoleNOCSenior},
	})
	_, err := guard.Approve(ActionRequest{ActionID: "mikrotik.change_firewall"}, RoleAdmin, now.Add(time.Hour))
	if err == nil {
		t.Fatal("role Admin tidak boleh mengapprove aksi network risiko tinggi")
	}
}

func TestApprovalMewajibkanCaseDanRoleUntukRisikoMenengah(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	guard := NewExecutionGuard(nil, []ActionDefinition{
		{ID: "invalid.medium", Scope: "single_customer", Permission: PermWrite, Risk: RiskMedium, AllowedByAI: true},
		{ID: "valid.medium", Scope: "single_customer", Permission: PermWrite, Risk: RiskMedium, AllowedByAI: true, RequiredRole: RoleNOCSenior},
	})
	if result := guard.Authorize(ActionRequest{ActionID: "invalid.medium"}, now); result.Decision != Deny {
		t.Fatalf("aksi MEDIUM tanpa role = %s, mau DENY", result.Decision)
	}
	if _, err := guard.Approve(ActionRequest{ActionID: "valid.medium"}, RoleNOCSenior, now.Add(time.Hour)); err == nil {
		t.Fatal("approval tanpa case_id harus ditolak")
	}
}

func TestExecutionGuardMenolakScopeYangBerbedaDariAllowlist(t *testing.T) {
	now := time.Date(2026, time.October, 2, 10, 0, 0, 0, time.UTC)
	guard := NewExecutionGuard(nil, []ActionDefinition{
		{ID: "mikrotik.disconnect_pppoe", Permission: PermWrite, Risk: RiskMedium, AllowedByAI: true, RequiredRole: RoleNOCSenior, Scope: "single_customer"},
	})
	result := guard.Authorize(ActionRequest{ActionID: "mikrotik.disconnect_pppoe", Scope: "multi_customer"}, now)
	if result.Decision != Deny {
		t.Fatalf("scope di luar allowlist = %s, mau DENY", result.Decision)
	}
}

func TestVerificationMembutuhkanSeluruhBuktiWajib(t *testing.T) {
	plan := VerificationPlan{RequiredChecks: []string{"radius_session", "pppoe_active", "gateway_ping"}}

	failed := Verify(plan, []VerificationEvidence{
		{Check: "radius_session", Passed: true},
		{Check: "pppoe_active", Passed: true},
		{Check: "gateway_ping", Passed: false},
	})
	if failed.Passed {
		t.Fatal("verifikasi dengan gateway_ping gagal tidak boleh lulus")
	}
	if len(failed.FailedChecks) != 1 || failed.FailedChecks[0] != "gateway_ping" {
		t.Fatalf("failed checks = %#v, mau [gateway_ping]", failed.FailedChecks)
	}

	conflicting := Verify(VerificationPlan{RequiredChecks: []string{"radius_session"}}, []VerificationEvidence{
		{Check: "radius_session", Passed: true},
		{Check: "radius_session", Passed: false},
	})
	if conflicting.Passed {
		t.Fatal("bukti verifikasi saling bertentangan harus gagal tertutup")
	}

	passed := Verify(plan, []VerificationEvidence{
		{Check: "radius_session", Passed: true},
		{Check: "pppoe_active", Passed: true},
		{Check: "gateway_ping", Passed: true},
	})
	if !passed.Passed {
		t.Fatalf("seluruh bukti lulus harus lulus: %#v", passed)
	}
}
