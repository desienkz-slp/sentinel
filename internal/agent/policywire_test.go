// policywire_test.go — FASE 3: tes yang mengunci perilaku Policy gate untuk
// execute. Kontrak yang dikunci (sesuai docs/ORCHESTRATOR_WIRING_PLAN.md):
//
//  1. AI TIDAK PERNAH mengeksekusi action berisiko tanpa policy check.
//  2. Deny-by-default: aksi di luar allowlist / ditolak policy → DENY.
//  3. MEDIUM+ (WRITE tanpa approval) → eskalasi, BUKAN eksekusi otomatis.
//  4. LOW read-only tetap jalan (ALLOW → dieksekusi lewat dispatcher).
package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/memory"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/session"
	"ainoc/internal/standard"
	"ainoc/internal/tool"
	"ainoc/internal/workflow"
)

// policyFixture membangun gerbang kebijakan fase 3 lengkap: policy file asli
// (deny-by-default write) + ExecutionGuard dari registry + casewire + audit.
func policyFixture(t *testing.T) (*PolicyGate, *CaseWire, *registry.Registry, *audit.Store) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "registry.yaml")
	content := "version: 1.0.0\ntools:\n" +
		"  - name: billing.get_customer\n    domain: billing\n    enabled: true\n    permission: READ\n    risk: LOW\n    scope: single_customer\n" +
		"  - name: mikrotik.disconnect_pppoe\n    domain: mikrotik\n    enabled: true\n    permission: WRITE\n    risk: MEDIUM\n    scope: single_customer\n    approval: required\n" +
		"  - name: mikrotik.reboot_router\n    domain: mikrotik\n    enabled: true\n    permission: ADMIN\n    risk: HIGH\n    scope: single_customer\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	reg := registry.Load(p)
	pol := policy.Load(filepath.Join("..", "..", "policies", "default-policy.yaml"))
	guard := policy.NewExecutionGuard(pol, ActionDefinitions(reg.All(), []string{"ping", "dns"}))
	casew := NewCaseWire()
	aud := audit.New("", 200)
	return NewPolicyGate(guard, reg, casew, aud), casew, reg, aud
}

// policyLoad memuat file kebijakan asli (sama dengan policyFixture).
func policyLoad() *policy.Engine {
	return policy.Load(filepath.Join("..", "..", "policies", "default-policy.yaml"))
}

// countingAdapter menghitung berapa kali Invoke dipanggil — bukti eksekusi.
type countingAdapter struct{ calls int }

func (c *countingAdapter) Domain() string      { return "mikrotik" }
func (c *countingAdapter) Name() string        { return "mikrotik" }
func (c *countingAdapter) Configured() bool    { return true }
func (c *countingAdapter) ToolNames() []string { return []string{"mikrotik.disconnect_pppoe"} }
func (c *countingAdapter) Health(ctx context.Context) (string, error) {
	return "ok", nil
}
func (c *countingAdapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	c.calls++
	return tool.Output{Text: "sesi diputus (seharusnya TIDAK terjadi)"}, nil
}

// billingAdapter adalah adapter READ nyata untuk jalur LOW read-only.
type billingAdapter struct{ calls int }

func (b *billingAdapter) Domain() string      { return "billing" }
func (b *billingAdapter) Name() string        { return "billing" }
func (b *billingAdapter) Configured() bool    { return true }
func (b *billingAdapter) ToolNames() []string { return []string{"billing.get_customer"} }
func (b *billingAdapter) Health(ctx context.Context) (string, error) {
	return "ok", nil
}
func (b *billingAdapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	b.calls++
	return tool.Output{Text: "status=AKTIF"}, nil
}

// TestPolicyGateWriteTanpaApprovalTidakDieksekusi mengunci kontrak #1 dan #3:
// aksi WRITE MEDIUM tanpa approval → APPROVAL_REQUIRED/DENY (tidak ALLOW),
// sehingga tidak pernah sampai ke adapter.
func TestPolicyGateWriteTanpaApprovalTidakDieksekusi(t *testing.T) {
	gate, _, reg, _ := policyFixture(t)

	auth := gate.Check("CASE-20261003-ABC123", "mikrotik.disconnect_pppoe",
		map[string]any{"identity": "628111222333", "idempotency_key": "k-1"})
	if auth.Decision == policy.Allow {
		t.Fatalf("WRITE MEDIUM tanpa approval = ALLOW, mau APPROVAL_REQUIRED/DENY: %v", auth.Reasons)
	}

	// Bahkan lewat dispatcher pun tidak boleh sampai ke adapter.
	disp := tool.New(reg, policyLoad(), time.Second)
	ad := &countingAdapter{}
	disp.Register(ad)
	rres := disp.Invoke(context.Background(), "mikrotik.disconnect_pppoe",
		map[string]any{"identity": "628111222333"})
	if rres.OK {
		t.Fatal("dispatcher mengeksekusi aksi WRITE tanpa approval")
	}
	if ad.calls != 0 {
		t.Fatalf("adapter dipanggil %dx — aksi berisiko TIDAK boleh dieksekusi", ad.calls)
	}
}

// TestPolicyGateDenyByDefault mengunci kontrak #2: tool di luar allowlist dan
// ADMIN (deny-admin-actions) selalu DENY.
func TestPolicyGateDenyByDefault(t *testing.T) {
	gate, _, _, _ := policyFixture(t)

	if auth := gate.Check("", "mikrotik.hapus_konfigurasi", nil); auth.Decision != policy.Deny {
		t.Fatalf("tool tidak terdaftar = %s, mau DENY", auth.Decision)
	}
	if auth := gate.Check("", "mikrotik.reboot_router", nil); auth.Decision != policy.Deny {
		t.Fatalf("ADMIN = %s, mau DENY (deny-admin-actions)", auth.Decision)
	}
}

// TestPolicyGateLOWReadTetapJalan mengunci kontrak #4: READ LOW → ALLOW dan
// benar-benar dieksekusi lewat dispatcher.
func TestPolicyGateLOWReadTetapJalan(t *testing.T) {
	gate, _, reg, _ := policyFixture(t)

	if auth := gate.Check("", "billing.get_customer", map[string]any{"identity": "628111222333"}); auth.Decision != policy.Allow {
		t.Fatalf("READ LOW = %s, mau ALLOW: %v", auth.Decision, auth.Reasons)
	}

	disp := tool.New(reg, policyLoad(), time.Second)
	ad := &billingAdapter{}
	disp.Register(ad)
	rres := disp.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "628111222333"})
	if !rres.OK {
		t.Fatalf("READ LOW gagal dieksekusi: %s", rres.Error)
	}
	if ad.calls != 1 {
		t.Fatalf("adapter READ dipanggil %dx, mau 1 (LOW read-only harus tetap jalan)", ad.calls)
	}
}

// TestPolicyGateEscalateMendorongCaseKeEscalation mengunci alur state:
// REASONING → ACTION_PROPOSED → POLICY_CHECK → ESCALATION (bukan eksekusi).
func TestPolicyGateEscalateMendorongCaseKeEscalation(t *testing.T) {
	gate, casew, _, aud := policyFixture(t)
	identity := "628111222333"

	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)
	if snap := casew.Snapshot(identity); snap.CaseState != string(caseengine.StateReasoning) {
		t.Fatalf("state awal = %s, mau REASONING", snap.CaseState)
	}

	auth := gate.Check("", "mikrotik.disconnect_pppoe", map[string]any{"identity": identity})
	snap := gate.Escalate(identity, "", "mikrotik.disconnect_pppoe", auth)

	if snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("state setelah aksi ditahan = %s, mau ESCALATION", snap.CaseState)
	}
	// Audit append-only harus memuat keputusan kebijakan.
	found := false
	for _, e := range aud.Recent(50) {
		if e.EventType == "policy_decision" && e.Tool == "mikrotik.disconnect_pppoe" {
			found = true
			if e.ExecutionStatus != "NOT_EXECUTED" {
				t.Fatalf("audit execution_status = %s, mau NOT_EXECUTED", e.ExecutionStatus)
			}
		}
	}
	if !found {
		t.Fatal("audit tidak memuat policy_decision untuk aksi yang ditahan")
	}
}

// TestShouldEscalateBerbasisPolicy mengunci bahwa keputusan eskalasi fase 3
// didasari policy gate (bukan hardcode false), dan eskalasi TIDAK memanggil
// model lain (Codex) — melainkan handoff manusia.
func TestShouldEscalateBerbasisPolicy(t *testing.T) {
	gate, _, _, _ := policyFixture(t)
	e := &Engine{Policy: gate}

	if e.shouldEscalate(Report{Verdict: "TIDAK DIKETAHUI", Confidence: 0}, nil) {
		t.Fatal("tanpa aksi yang ditahan kebijakan, shouldEscalate harus false")
	}
	rep := Report{Steps: []Step{
		{Kind: "tool", Tool: "billing.get_customer", OK: true},
		{Kind: "policy", Tool: "mikrotik.disconnect_pppoe", OK: false,
			Output: "APPROVAL_REQUIRED: risiko MEDIUM membutuhkan persetujuan"},
	}}
	if !e.shouldEscalate(rep, map[string]int{"mikrotik.disconnect_pppoe": 1}) {
		t.Fatal("aksi ditahan kebijakan harus memicu eskalasi")
	}
	// escalate() pada kasus ini TIDAK boleh menyentuh Codex (nil → panic bila dipanggil).
	e.escalate(context.Background(), &rep)
	if !rep.Escalated || !strings.Contains(rep.Escalation, "policy") {
		t.Fatalf("laporan tidak ditandai eskalasi kebijakan: %+v", rep)
	}
}

// TestRunWorkflowPolicyMenahanLangkahWrite mengunci deny-by-default di jalur
// workflow deterministik: langkah WRITE ditahan, adapter tidak dipanggil.
func TestRunWorkflowPolicyMenahanLangkahWrite(t *testing.T) {
	gate, casew, reg, _ := policyFixture(t)
	disp := tool.New(reg, policyLoad(), time.Second)
	ad := &countingAdapter{}
	disp.Register(ad)

	def := workflow.Definition{
		Name: "tes-aksi-berisiko", Trigger: "COMPLAINT", Mode: "COPILOT",
		Steps: []workflow.Step{
			{Index: 0, Name: "read", Tool: "billing.get_customer",
				Params: map[string]any{"identity": "${identity_id}"}},
			{Index: 1, Name: "write", Tool: "mikrotik.disconnect_pppoe",
				Params: map[string]any{"identity": "${identity_id}"}},
		},
	}
	identity := "628111222333"
	casew.begin(identity, "whatsapp")
	casew.onDiagnosis(identity, standard.IntentComplaint)

	e := &Engine{Reg: reg, Disp: disp, Policy: gate, Case: casew}
	h := e.runWorkflow(context.Background(), identity, "", def, nil)

	if ad.calls != 0 {
		t.Fatalf("adapter WRITE dipanggil %dx — langkah berisiko harus ditahan policy gate", ad.calls)
	}
	if !strings.Contains(h.Aborted, "policy gate") {
		t.Fatalf("workflow tidak menandai penahanan kebijakan: %q", h.Aborted)
	}
	var policySteps int
	for _, s := range h.Steps {
		if s.Kind == "policy" {
			policySteps++
		}
	}
	if policySteps != 1 {
		t.Fatalf("langkah policy = %d, mau 1", policySteps)
	}
	if snap := casew.Snapshot(identity); snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("case setelah langkah ditahan = %s, mau ESCALATION", snap.CaseState)
	}
}

// TestRunWithMenahanToolCallWriteDariLLM adalah integrasi end-to-end: LLM
// mengusulkan aksi WRITE → policy gate menahan (tidak dieksekusi), case
// berakhir ESCALATION, dan laporan membawa jejak kebijakan.
func TestRunWithMenahanToolCallWriteDariLLM(t *testing.T) {
	gate, casew, reg, _ := policyFixture(t)

	// Fake LLM: respons pertama = tool call WRITE, respons kedua = jawaban akhir.
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		if calls == 1 {
			_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","model":"t","choices":[{"index":0,"message":{"role":"assistant","content":"saya usulkan putuskan sesi","tool_calls":[{"id":"call_1","type":"function","function":{"name":"mikrotik.disconnect_pppoe","arguments":"{\"identity\":\"628111222333\",\"idempotency_key\":\"k-1\"}"}}]},"finish_reason":"tool_calls"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"y","object":"chat.completion","model":"t","choices":[{"index":0,"message":{"role":"assistant","content":"BALASAN: Sesi sedang kami periksa, tim kami menindaklanjuti.\nVERDICT: GANGGUAN\nKEYAKINAN: 85\nAKAR_MASALAH: sesi PPPoE stale"},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	client := llm.New(srv.URL, "", "model-uji", 10)
	disp := tool.New(reg, policyLoad(), time.Second)
	ad := &countingAdapter{}
	disp.Register(ad)

	e := &Engine{
		Cfg:    &config.Config{MaxSteps: 4},
		LLM:    client,
		Diag:   diag.New(5),
		Reg:    reg,
		Disp:   disp,
		Sesi:   session.New(session.DefaultConfig()),
		Mem:    memory.New(memory.Config{}),
		Learn:  learning.New(),
		Case:   casew,
		Policy: gate,
	}

	rep := e.Run(context.Background(), "628111222333",
		"internet saya putus total sejak pagi, tolong putuskan sesi PPPoE yang nyangkut", "")

	if ad.calls != 0 {
		t.Fatalf("adapter WRITE dipanggil %dx — MEDIUM+ tidak boleh dieksekusi otomatis", ad.calls)
	}
	if rep.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("case state = %s, mau ESCALATION (aksi MEDIUM+ ditahan)", rep.CaseState)
	}
	if !rep.Escalated || !strings.Contains(rep.Escalation, "policy") {
		t.Fatalf("laporan tidak merekam eskalasi kebijakan: escalated=%v escalation=%q", rep.Escalated, rep.Escalation)
	}
	policySteps := 0
	for _, s := range rep.Steps {
		if s.Kind == "policy" && s.Tool == "mikrotik.disconnect_pppoe" {
			policySteps++
			if s.OK {
				t.Error("langkah policy yang ditahan tidak boleh OK=true")
			}
		}
		if s.Kind == "tool" && s.Tool == "mikrotik.disconnect_pppoe" {
			t.Error("aksi WRITE tidak boleh tercatat sebagai langkah tool tereksekusi")
		}
	}
	if policySteps == 0 {
		t.Fatal("tidak ada langkah policy — gerbang fase 3 tidak terpicu")
	}
}

// TestActionDefinitionsMemetakanPeranDanAllowlist mengunci bahwa definisi aksi
// hasil registry valid untuk ExecutionGuard (READ LOW tanpa role; WRITE wajib
// punya role dari domain).
func TestActionDefinitionsMemetakanPeranDanAllowlist(t *testing.T) {
	defs := ActionDefinitions([]registry.Tool{
		{Name: "billing.get_customer", Domain: "billing", Permission: registry.PermRead, Risk: registry.RiskLow},
		{Name: "mikrotik.disconnect_pppoe", Domain: "mikrotik", Permission: registry.PermWrite, Risk: registry.RiskMedium, Scope: "single_customer"},
	}, []string{"ping"})

	guard := policy.NewExecutionGuard(policy.Load(""), defs)
	if auth := guard.Authorize(policy.ActionRequest{ActionID: "billing.get_customer", Scope: "single_customer"}, time.Now()); auth.Decision != policy.Allow {
		t.Fatalf("READ LOW valid = %s, mau ALLOW: %v", auth.Decision, auth.Reasons)
	}
	auth := guard.Authorize(policy.ActionRequest{ActionID: "mikrotik.disconnect_pppoe", Scope: "single_customer"}, time.Now())
	if auth.Decision == policy.Allow {
		t.Fatalf("WRITE MEDIUM tanpa approval = ALLOW: %v", auth.Reasons)
	}
	if auth := guard.Authorize(policy.ActionRequest{ActionID: "ping", Scope: "single_customer"}, time.Now()); auth.Decision != policy.Allow {
		t.Fatalf("probe diag bawaan = %s, mau ALLOW (read-only): %v", auth.Decision, auth.Reasons)
	}
}
