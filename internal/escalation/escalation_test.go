package escalation

import "testing"

func TestNetworkRoutesToNOCSenior(t *testing.T) {
	if got := Target("network"); got != RoleNOCSenior {
		t.Fatalf("network target=%q", got)
	}
}

func TestNonNetworkRoutesToAdmin(t *testing.T) {
	if got := Target("billing"); got != RoleAdmin {
		t.Fatalf("billing target=%q", got)
	}
}

func TestPayloadMarksUnavailableEvidence(t *testing.T) {
	p := BuildPayload("CASE-20261001-ABC123", "network", map[string]string{"radius": "ONLINE"}, []string{"genieacs"})
	if p.CaseID == "" || p.TargetRole != RoleNOCSenior || p.Evidence["genieacs"] != "UNAVAILABLE" {
		t.Fatalf("payload=%+v", p)
	}
}

func TestEngineRoutesNetworkToAvailableNOCSeniorWithCompleteContext(t *testing.T) {
	directory := NewDirectory(
		Staff{ID: "noc-primary", Role: RoleNOCSenior, Active: true, OnCall: true, Priority: 1},
		Staff{ID: "admin-primary", Role: RoleAdmin, Active: true, OnCall: true, Priority: 1},
	)
	engine := NewEngine(directory, Policy{ResponseTimeout: 10})

	result := engine.Start(RequestInput{
		Payload: Context{
			CaseID:              "CASE-20261001-ABC123",
			Domain:              "network",
			Customer:            "Pelanggan Uji",
			Complaint:           "Internet putus",
			ConversationSummary: "Pelanggan melaporkan internet putus sejak pagi.",
			Timeline:            "mulai pagi ini",
			BillingStatus:       "ACTIVE",
			RadiusStatus:        "UNAVAILABLE",
			RouterStatus:        "PPPoE DOWN",
			GenieACSStatus:      "ONLINE",
			Diagnostics:         []string{"cek sesi RADIUS", "cek PPPoE"},
			Evidence:            []string{"PPPoE tidak aktif"},
			Hypothesis:          "gangguan akses jaringan",
			Confidence:          "MEDIUM",
			ActionsPerformed:    []string{"diagnostik read-only"},
			ActionsNotPerformed: []string{"ubah konfigurasi router"},
			Recommendation:      "periksa jalur akses",
			Urgency:             "SEV-2",
			AffectedScope:       "satu pelanggan",
			Reason:              "memerlukan otoritas NOC Senior",
			HumanNeed:           "validasi dan tindakan jaringan",
		},
		NowUnix: 100,
	})

	if result.Request.TargetRole != RoleNOCSenior || result.AssignedStaff.ID != "noc-primary" {
		t.Fatalf("routing network tidak tepat: %+v", result)
	}
	if missing := result.Request.Payload.MissingRequired(); len(missing) != 0 {
		t.Fatalf("payload handoff belum lengkap: %v", missing)
	}
}

func TestEngineRoutesNonNetworkOnlyToAdmin(t *testing.T) {
	directory := NewDirectory(
		Staff{ID: "noc-primary", Role: RoleNOCSenior, Active: true, OnCall: true, Priority: 1},
		Staff{ID: "admin-primary", Role: RoleAdmin, Active: true, OnCall: true, Priority: 1},
	)
	result := NewEngine(directory, Policy{ResponseTimeout: 10}).Start(RequestInput{
		Payload: Context{CaseID: "CASE-20261001-ABC124", Domain: "billing"},
		NowUnix: 100,
	})
	if result.Request.TargetRole != RoleAdmin || result.AssignedStaff.ID != "admin-primary" {
		t.Fatalf("routing non-network tidak tepat: %+v", result)
	}
}

func TestTimeoutMovesToNextAvailableSameRole(t *testing.T) {
	directory := NewDirectory(
		Staff{ID: "noc-primary", Role: RoleNOCSenior, Active: true, OnCall: true, Priority: 1},
		Staff{ID: "noc-backup", Role: RoleNOCSenior, Active: true, OnCall: true, Priority: 2},
		Staff{ID: "admin-primary", Role: RoleAdmin, Active: true, OnCall: true, Priority: 1},
	)
	engine := NewEngine(directory, Policy{ResponseTimeout: 10})
	started := engine.Start(RequestInput{Payload: Context{CaseID: "CASE-20261001-ABC125", Domain: "network"}, NowUnix: 100})

	early := engine.AdvanceOnTimeout(started.Request, 109)
	if early.AssignedStaff.ID != "noc-primary" || early.Request.Status != StatusPending {
		t.Fatalf("fallback sebelum timeout: %+v", early)
	}
	fallback := engine.AdvanceOnTimeout(early.Request, 110)
	if fallback.AssignedStaff.ID != "noc-backup" || fallback.Request.TargetRole != RoleNOCSenior {
		t.Fatalf("fallback network harus ke NOC Senior backup: %+v", fallback)
	}
}

func TestTimeoutNeverFallsBackAcrossAuthorityRole(t *testing.T) {
	directory := NewDirectory(Staff{ID: "admin-primary", Role: RoleAdmin, Active: true, OnCall: true, Priority: 1})
	engine := NewEngine(directory, Policy{ResponseTimeout: 10})
	result := engine.Start(RequestInput{Payload: Context{CaseID: "CASE-20261001-ABC126", Domain: "network"}, NowUnix: 100})

	if result.Request.Status != StatusExhausted || result.AssignedStaff.ID != "" || result.Request.TargetRole != RoleNOCSenior {
		t.Fatalf("network tanpa NOC Senior tidak boleh dialihkan ke Admin: %+v", result)
	}
}

func TestAcknowledgedRequestDoesNotFallback(t *testing.T) {
	directory := NewDirectory(
		Staff{ID: "admin-primary", Role: RoleAdmin, Active: true, OnCall: true, Priority: 1},
		Staff{ID: "admin-backup", Role: RoleAdmin, Active: true, OnCall: true, Priority: 2},
	)
	engine := NewEngine(directory, Policy{ResponseTimeout: 10})
	started := engine.Start(RequestInput{Payload: Context{CaseID: "CASE-20261001-ABC127", Domain: "billing"}, NowUnix: 100})
	acknowledged := engine.Acknowledge(started.Request, "admin-primary", 105)
	result := engine.AdvanceOnTimeout(acknowledged.Request, 120)

	if result.Request.Status != StatusAcknowledged || result.AssignedStaff.ID != "admin-primary" {
		t.Fatalf("request yang diakui tidak boleh fallback: %+v", result)
	}
}
