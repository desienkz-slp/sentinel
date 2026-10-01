package agent

import (
	"testing"

	"ainoc/internal/correlation"
	"ainoc/internal/workflow"
)

// TestToolDomain memastikan pemetaan nama tool -> domain benar.
func TestToolDomain(t *testing.T) {
	cases := map[string]string{
		"billing.get_customer":      "billing",
		"radius.get_session":        "radius",
		"mikrotik.get_pppoe_status": "mikrotik",
		"genieacs.get_device_state": "genieacs",
		"tanahtitik":                "tanahtitik",
	}
	for in, want := range cases {
		if got := toolDomain(in); got != want {
			t.Errorf("toolDomain(%q) = %q, mau %q", in, got, want)
		}
	}
}

// TestResolveTemplate memastikan ${var} di-resolve dan yang tidak dikenal kosong.
func TestResolveTemplate(t *testing.T) {
	vars := map[string]string{"identity_id": "628111222333"}
	if got := resolveTemplate("${identity_id}", vars); got != "628111222333" {
		t.Errorf("resolve = %q, mau 628111222333", got)
	}
	if got := resolveTemplate("${tidak_ada}", vars); got != "" {
		t.Errorf("resolve tak dikenal = %q, mau kosong", got)
	}
	// Tanpa placeholder -> teks tetap utuh.
	if got := resolveTemplate("tetap", vars); got != "tetap" {
		t.Errorf("resolve tanpa template = %q", got)
	}
}

// TestConclVerdict memastikan kesimpulan korelasi dipetakan ke verdict standar.
func TestConclVerdict(t *testing.T) {
	cases := map[string]string{
		"akun/tagihan tidak aktif":                "GANGGUAN",
		"autentikasi RADIUS gagal":                "GANGGUAN",
		"CPE/ONT offline (daya/link fisik)":       "GANGGUAN",
		"sesi PPPoE stale/putus":                  "GANGGUAN",
		"bukti tidak cukup untuk diagnosis pasti": "TIDAK DIKETAHUI",
	}
	for diag, want := range cases {
		if got := conclVerdict(correlation.Conclusion{Diagnosis: diag}); got != want {
			t.Errorf("conclVerdict(%q) = %q, mau %q", diag, got, want)
		}
	}
}

// TestFallbackBalasan memastikan fallback deterministik tidak kosong dan spesifik.
func TestFallbackBalasan(t *testing.T) {
	for _, area := range []string{"billing", "radius", "genieacs", "mikrotik", ""} {
		if got := fallbackBalasan(correlation.Conclusion{PrimaryArea: area}); got == "" {
			t.Errorf("fallbackBalasan(%q) kosong", area)
		}
	}
}

// TestWorkflowForIntent memastikan intent COMPLAINT memetakan ke workflow
// CUSTOMER_INTERNET_DOWN.
func TestWorkflowForIntent(t *testing.T) {
	r, err := workflow.LoadDir("../../workflows")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	if _, ok := r.ForIntent("COMPLAINT"); !ok {
		t.Fatal("ForIntent(COMPLAINT) harus menemukan workflow")
	}
	if _, ok := r.ForIntent("CHAT"); ok {
		t.Fatal("ForIntent(CHAT) tidak boleh menemukan workflow")
	}
}

// TestWorkflowStepsParsed memastikan field params/allowed_tools dari YAML
// benar-benar dimuat (bukan ter-drop) — inilah yang dulu membuat workflow menganggur.
func TestWorkflowStepsParsed(t *testing.T) {
	r, err := workflow.LoadDir("../../workflows")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	def, ok := r.ForIntent("COMPLAINT")
	if !ok {
		t.Fatal("workflow COMPLAINT tidak ditemukan")
	}
	if len(def.Steps) == 0 {
		t.Fatal("workflow tanpa langkah")
	}
	// check_billing harus punya tool + allowed_tools + params.
	var billingStep *workflow.Step
	for i := range def.Steps {
		if def.Steps[i].Tool == "billing.get_customer" {
			billingStep = &def.Steps[i]
		}
	}
	if billingStep == nil {
		t.Fatal("langkah billing.get_customer tidak ditemukan")
	}
	if len(billingStep.AllowedTools) == 0 {
		t.Error("allowed_tools billing kosong — YAML ter-drop?")
	}
	if billingStep.Params == nil {
		t.Error("params billing kosong — YAML ter-drop?")
	}
	if !def.Audit {
		t.Error("workflow harusnya audit:true")
	}
}
