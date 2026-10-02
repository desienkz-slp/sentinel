// verifywire_test.go — FASE 4: tes yang mengunci Verification Engine.
//
// Kontrak (docs/ORCHESTRATOR_WIRING_PLAN.md + master spec §13, §53 rule 7):
//
//  1. RESOLVED TIDAK PERNAH tercapai tanpa RecordVerification (invariant).
//  2. Alur aksi + verifikasi lulus: EXECUTING → VERIFYING → RESOLVED.
//  3. Verifikasi gagal → FAILED → ESCALATION.
//  4. Aksi WRITE tanpa rencana verifikasi / verifier → fail-closed (eskalasi).
//  5. Aksi WRITE tanpa verification gate TIDAK dieksekusi.
package agent

import (
	"context"
	"os"
	"testing"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/standard"
	"ainoc/internal/tool"
)

// verifyReg menulis registry dengan tool WRITE yang punya rencana verifikasi
// (mikrotik.disconnect_pppoe -> mikrotik.get_pppoe_status) + tool READ-nya.
func verifyReg(t *testing.T) *registry.Registry {
	t.Helper()
	content := "version: 1.0.0\ntools:\n" +
		"  - name: mikrotik.disconnect_pppoe\n    domain: mikrotik\n    enabled: true\n    permission: WRITE\n    risk: MEDIUM\n    scope: single_customer\n    approval: required\n    verification: mikrotik.get_pppoe_status\n" +
		"  - name: mikrotik.get_pppoe_status\n    domain: mikrotik\n    enabled: true\n    permission: READ\n    risk: LOW\n    scope: single_customer\n"
	dir := t.TempDir()
	p := dir + "/registry.yaml"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Load(p)
}

// execOK adalah executor fake: aksi selalu sukses.
func execOK() Executor {
	return func(ctx context.Context, name string, args map[string]any) tool.Result {
		return tool.Result{Tool: name, OK: true, Output: tool.Output{Text: "aksi selesai"}}
	}
}

// TestResolvedTanpaRecordVerificationDitolak adalah invariant dasar (sudah ada
// di caseengine); dikunci lagi dari sisi agent supaya kontrak fase 4 utuh.
func TestResolvedTanpaRecordVerificationDitolak(t *testing.T) {
	w := NewCaseWire()
	w.begin("628111222333", "whatsapp")
	w.onDiagnosis("628111222333", standard.IntentComplaint)

	// Dorong sampai jalur aksi, lalu tutup TANPA verifikasi lulus.
	w.onActionStarted("628111222333", "mikrotik.disconnect_pppoe")
	snap := w.onActionCompleted("628111222333", "mikrotik.disconnect_pppoe",
		caseengine.Verification{Passed: false, Source: "none"})
	if snap.CaseState == string(caseengine.StateResolved) {
		t.Fatal("tanpa verifikasi lulus, case tidak boleh RESOLVED")
	}
}

// TestActionVerifyPassedResolves mengunci alur lulus:
// EXECUTING → VERIFYING → RESOLVED.
func TestActionVerifyPassedResolves(t *testing.T) {
	reg := verifyReg(t)
	casew := NewCaseWire()
	aud := audit.New("", 200)
	gate := NewVerificationGate(casew, reg, nil, aud, nil)

	identity := "628111222333"
	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)

	gate.verifier = func(ctx context.Context, name string, args map[string]any, rres tool.Result) caseengine.Verification {
		return caseengine.Verification{Passed: true, Source: "mikrotik.get_pppoe_status", Summary: "PPPoE aktif kembali"}
	}

	_, snap := gate.RunAction(context.Background(), identity, "", "mikrotik.disconnect_pppoe",
		map[string]any{"identity": identity}, execOK())

	if snap.CaseState != string(caseengine.StateResolved) {
		t.Fatalf("setelah verifikasi lulus case = %s, mau RESOLVED", snap.CaseState)
	}
	found := false
	for _, e := range aud.Recent(50) {
		if e.EventType == "verification" && e.ExecutionStatus == "PASSED" {
			found = true
		}
	}
	if !found {
		t.Fatal("audit tidak memuat verification PASSED")
	}
}

// TestActionVerifyFailedEscalates mengunci alur gagal:
// EXECUTING → VERIFYING → FAILED → ESCALATION.
func TestActionVerifyFailedEscalates(t *testing.T) {
	reg := verifyReg(t)
	casew := NewCaseWire()
	gate := NewVerificationGate(casew, reg, nil, audit.New("", 200), nil)

	identity := "628111222333"
	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)

	gate.verifier = func(ctx context.Context, name string, args map[string]any, rres tool.Result) caseengine.Verification {
		return caseengine.Verification{Passed: false, Source: "mikrotik.get_pppoe_status", Summary: "PPPoE masih putus"}
	}

	_, snap := gate.RunAction(context.Background(), identity, "", "mikrotik.disconnect_pppoe",
		map[string]any{"identity": identity}, execOK())

	if snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("setelah verifikasi gagal case = %s, mau ESCALATION", snap.CaseState)
	}
}

// TestVerifyTanpaRencanaVerifikasiFailClosed mengunci bahwa aksi WRITE tanpa
// field `verification` di registry tidak boleh dianggap selesai (fail-closed).
func TestVerifyTanpaRencanaVerifikasiFailClosed(t *testing.T) {
	content := "version: 1.0.0\ntools:\n" +
		"  - name: mikrotik.disconnect_pppoe\n    domain: mikrotik\n    enabled: true\n    permission: WRITE\n    risk: MEDIUM\n    scope: single_customer\n"
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/registry.yaml", []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.Load(dir + "/registry.yaml")
	casew := NewCaseWire()
	gate := NewVerificationGate(casew, reg, nil, audit.New("", 200), nil)

	identity := "628111222333"
	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)

	_, snap := gate.RunAction(context.Background(), identity, "", "mikrotik.disconnect_pppoe",
		map[string]any{"identity": identity}, execOK())

	if snap.CaseState == string(caseengine.StateResolved) {
		t.Fatal("WRITE tanpa rencana verifikasi tidak boleh RESOLVED (fail-closed)")
	}
	if snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("case = %s, mau ESCALATION (fail-closed)", snap.CaseState)
	}
}

// TestToolIsWrite mengunci pemetaan READ vs WRITE (write wajib verifikasi).
func TestToolIsWrite(t *testing.T) {
	reg := verifyReg(t)
	if !toolIsWrite(reg, "mikrotik.disconnect_pppoe") {
		t.Fatal("WRITE harus dikenali sebagai write")
	}
	if toolIsWrite(reg, "mikrotik.get_pppoe_status") {
		t.Fatal("READ tidak boleh dikenali sebagai write")
	}
	if !toolIsWrite(reg, "tool.tidak_ada") {
		t.Fatal("tool tak dikenal harus dianggap write (fail-closed)")
	}
	if !toolIsWrite(nil, "apa.saja") {
		t.Fatal("registry nil harus dianggap write (fail-closed)")
	}
}

// TestRunActionTanpaGateTidakEksekusi mengunci fail-closed di jalur agent:
// aksi WRITE tanpa verification gate tidak dijalankan.
func TestRunActionTanpaGateTidakEksekusi(t *testing.T) {
	var gate *VerificationGate
	rres, _ := gate.RunAction(context.Background(), "628111222333", "", "mikrotik.disconnect_pppoe", nil, nil)
	if rres.Error == "" {
		t.Fatal("gate nil harus mengembalikan error, bukan mengeksekusi")
	}
}

// TestRunActionDenganDefaultVerifyQueryUlangSistem mengunci verifier bawaan:
// setelah aksi sukses, sistem di-query ulang via tool `verification`, dan
// hanya state kanonik "pulih" yang membuka RESOLVED.
func TestRunActionDenganDefaultVerifyQueryUlangSistem(t *testing.T) {
	reg := verifyReg(t)
	pol := policy.Load("")
	disp := tool.New(reg, pol, time.Second)
	disp.Register(&fakeMikrotikAdapter{pppoeText: "PPPoE AKTIF status=RUNNING"})

	casew := NewCaseWire()
	gate := NewVerificationGate(casew, reg, disp, audit.New("", 200), nil)
	identity := "628111222333"
	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)

	_, snap := gate.RunAction(context.Background(), identity, "", "mikrotik.disconnect_pppoe",
		map[string]any{"identity": identity}, execOK())

	if snap.CaseState != string(caseengine.StateResolved) {
		t.Fatalf("verifikasi bawaan (query ulang PPPoE aktif) = %s, mau RESOLVED", snap.CaseState)
	}
}

// TestRunActionDefaultVerifyBelumPulih mengunci fail: query ulang mengembalikan
// state belum pulih → tidak RESOLVED (FAILED → ESCALATION).
func TestRunActionDefaultVerifyBelumPulih(t *testing.T) {
	reg := verifyReg(t)
	pol := policy.Load("")
	disp := tool.New(reg, pol, time.Second)
	disp.Register(&fakeMikrotikAdapter{pppoeText: "PPPoE OFFLINE status=PUTUS"})

	casew := NewCaseWire()
	gate := NewVerificationGate(casew, reg, disp, audit.New("", 200), nil)
	identity := "628111222333"
	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)

	_, snap := gate.RunAction(context.Background(), identity, "", "mikrotik.disconnect_pppoe",
		map[string]any{"identity": identity}, execOK())

	if snap.CaseState == string(caseengine.StateResolved) {
		t.Fatal("sistem belum pulih tidak boleh RESOLVED")
	}
	if snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("case = %s, mau ESCALATION (belum pulih)", snap.CaseState)
	}
}

// fakeMikrotikAdapter memenuhi tool.Adapter untuk query ulang PPPoE.
type fakeMikrotikAdapter struct{ pppoeText string }

func (f *fakeMikrotikAdapter) Domain() string      { return "mikrotik" }
func (f *fakeMikrotikAdapter) Name() string        { return "mikrotik" }
func (f *fakeMikrotikAdapter) Configured() bool    { return true }
func (f *fakeMikrotikAdapter) ToolNames() []string { return []string{"mikrotik.get_pppoe_status"} }
func (f *fakeMikrotikAdapter) Health(ctx context.Context) (string, error) {
	return "ok", nil
}
func (f *fakeMikrotikAdapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	return tool.Output{Text: f.pppoeText}, nil
}
