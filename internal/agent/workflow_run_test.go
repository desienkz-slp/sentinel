package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ainoc/internal/directory"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/tool"
	"ainoc/internal/workflow"
)

// fakeWfAdapter adalah adapter tool uji untuk workflow: return teks yang bisa
// dinormalisasi correlation ke status kanonik per domain.
type fakeWfAdapter struct {
	domain string
	output string // teks yang dikembalikan Invoke
	fail   bool
}

func (f *fakeWfAdapter) Domain() string   { return f.domain }
func (f *fakeWfAdapter) Name() string     { return f.domain }
func (f *fakeWfAdapter) Configured() bool { return true }
func (f *fakeWfAdapter) ToolNames() []string {
	return []string{
		f.domain + ".get_customer",
		f.domain + ".get_session",
		f.domain + ".get_pppoe_status",
		f.domain + ".get_device_state",
	}
}
func (f *fakeWfAdapter) Health(ctx context.Context) (string, error) { return "ok", nil }
func (f *fakeWfAdapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	if f.fail {
		return tool.Output{}, errFake
	}
	return tool.Output{Text: f.output}, nil
}

var errFake = os.ErrInvalid

// TestRunWorkflowDeterministic membuktikan bahwa runWorkflow mengeksekusi
// langkah-langkah workflow secara deterministik lewat dispatcher (registry ->
// policy -> adapter), mengumpulkan bukti per domain, dan menghasilkan kesimpulan
// korelasi — TANPA memanggil LLM.
func TestRunWorkflowDeterministic(t *testing.T) {
	// Registry dengan keempat tool READ enabled:true (mensimulasikan operator
	// yang sudah mengaktifkan tool + memberi kredensial).
	reg := writeWfReg(t, true)
	pol := policy.Load("") // deny-by-default: READ LOW single -> ALLOW
	disp := tool.New(reg, pol, time.Second)

	// Daftarkan adapter fake per domain dengan output yang bisa dinormalisasi.
	disp.Register(&fakeWfAdapter{domain: "billing", output: "status=AKTIF"})
	disp.Register(&fakeWfAdapter{domain: "radius", output: "status=OK AUTH_OK"})
	disp.Register(&fakeWfAdapter{domain: "mikrotik", output: "status=OFFLINE PUTUS"})
	disp.Register(&fakeWfAdapter{domain: "genieacs", output: "status=ONLINE"})

	wf, err := workflow.LoadDir("../../workflows")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	def, ok := wf.ForIntent("COMPLAINT")
	if !ok {
		t.Fatal("workflow COMPLAINT tidak ditemukan")
	}

	e := &Engine{Reg: reg, Disp: disp, Wkf: wf}
	var steps []Step
	h := e.runWorkflow(context.Background(), "628111222333", "", def, func(s Step) {
		steps = append(steps, s)
	})

	if !h.RanAnyTool {
		t.Fatal("workflow harus menjalankan tool, tapi RanAnyTool=false")
	}
	if h.Aborted != "" {
		t.Fatalf("workflow terhenti: %s", h.Aborted)
	}
	// Empat domain (billing/radius/mikrotik/genieacs) harus punya bukti.
	domains := h.Evidence.Domains()
	if len(domains) != 4 {
		t.Fatalf("domain bukti = %v, mau 4 (billing radius mikrotik genieacs)", domains)
	}
	if h.Conclusion.Diagnosis == "" {
		t.Error("kesimpulan korelasi kosong")
	}
	// Evidence mikrotik = OFFLINE, billing AKTIF, radius OK, genieacs ONLINE ->
	// seharusnya "sesi PPPoE stale/putus" (area mikrotik).
	if h.Conclusion.PrimaryArea != "mikrotik" {
		t.Errorf("primary_area = %q, mau mikrotik (PPPoE down)", h.Conclusion.PrimaryArea)
	}
	// Harus ada langkah tool yang tereksekusi.
	toolSteps := 0
	for _, s := range steps {
		if s.Kind == "tool" {
			toolSteps++
		}
	}
	if toolSteps != 4 {
		t.Errorf("langkah tool = %d, mau 4", toolSteps)
	}
}

// TestRunWorkflowSkipsDisabledTool membuktikan deny-by-default: tool nonaktif
// TIDAK dijalankan workflow, tapi workflow tetap berlanjut (bukti = UNKNOWN).
func TestRunWorkflowSkipsDisabledTool(t *testing.T) {
	reg := writeWfReg(t, false) // semua enabled:false
	pol := policy.Load("")
	disp := tool.New(reg, pol, time.Second)
	disp.Register(&fakeWfAdapter{domain: "billing", output: "status=AKTIF"})

	wf, _ := workflow.LoadDir("../../workflows")
	def, _ := wf.ForIntent("COMPLAINT")

	e := &Engine{Reg: reg, Disp: disp, Wkf: wf}
	h := e.runWorkflow(context.Background(), "628111222333", "", def, nil)

	if h.RanAnyTool {
		t.Error("tool nonaktif tidak boleh dijalankan (RanAnyTool harus false)")
	}
	if h.Aborted != "" {
		t.Errorf("workflow seharusnya melewati tool nonaktif, bukan berhenti: %s", h.Aborted)
	}
	// Bukti tetap tercatat sebagai UNKNOWN/error — tidak menebak.
	if h.Conclusion.Confidence >= 0.5 {
		t.Errorf("keyakinan tanpa bukti nyata = %.2f, seharusnya rendah", h.Conclusion.Confidence)
	}
}

// TestRunWorkflowNoDispatcher menandai aborted bila dispatcher tidak tersedia.
func TestRunWorkflowNoDispatcher(t *testing.T) {
	wf, _ := workflow.LoadDir("../../workflows")
	def, _ := wf.ForIntent("COMPLAINT")
	e := &Engine{} // Disp nil
	h := e.runWorkflow(context.Background(), "628111222333", "", def, nil)
	if h.Aborted == "" {
		t.Error("tanpa dispatcher, workflow harus aborted pada langkah tool pertama")
	}
}

// recWfAdapter merekam argumen "identity" yang diterima adapter (untuk
// membuktikan identity hilir = username, bukan nomor telepon).
type recWfAdapter struct {
	domain string
	ids    []string
}

func (f *recWfAdapter) Domain() string   { return f.domain }
func (f *recWfAdapter) Name() string     { return f.domain }
func (f *recWfAdapter) Configured() bool { return true }
func (f *recWfAdapter) ToolNames() []string {
	return []string{
		f.domain + ".get_customer",
		f.domain + ".get_session",
		f.domain + ".get_pppoe_status",
		f.domain + ".get_device_state",
	}
}
func (f *recWfAdapter) Health(ctx context.Context) (string, error) { return "ok", nil }
func (f *recWfAdapter) Invoke(ctx context.Context, name string, args map[string]any) (tool.Output, error) {
	if v, ok := args["identity"]; ok {
		if s, isStr := v.(string); isStr {
			f.ids = append(f.ids, s)
		}
	}
	return tool.Output{Text: "status=ONLINE"}, nil
}

// TestRunWorkflowResolvesIdentityToUsername membuktikan perbaikan resolve_identity:
// bila penelepon sudah teridentifikasi sebagai pelanggan (Caller.Customer.Username
// terisi), langkah hilir (radius/mikrotik/genieacs) menerima USERNAME PPPoE —
// bukan nomor telepon 628xxx. Tanpa ini, adapter RADIUS/MikroTik yang mencocokkan
// username selalu menjawab "tidak ada sesi" walau datanya ada.
func TestRunWorkflowResolvesIdentityToUsername(t *testing.T) {
	reg := writeWfReg(t, true)
	pol := policy.Load("")
	disp := tool.New(reg, pol, time.Second)

	radiusRec := &recWfAdapter{domain: "radius"}
	mikrotikRec := &recWfAdapter{domain: "mikrotik"}
	genieacsRec := &recWfAdapter{domain: "genieacs"}
	disp.Register(&recWfAdapter{domain: "billing"})
	disp.Register(radiusRec)
	disp.Register(mikrotikRec)
	disp.Register(genieacsRec)

	wf, err := workflow.LoadDir("../../workflows")
	if err != nil {
		t.Fatalf("LoadDir: %v", err)
	}
	def, ok := wf.ForIntent("COMPLAINT")
	if !ok {
		t.Fatal("workflow COMPLAINT tidak ditemukan")
	}

	e := &Engine{Reg: reg, Disp: disp, Wkf: wf}

	// Penelepon pelanggan: nomor WA 6281210797235, username PPPoE "jttcitra".
	caller := directory.Caller{
		Number:     "6281210797235",
		Role:       directory.RoleCustomer,
		IsCustomer: true,
		Customer:   &directory.CustomerInfo{Username: "jttcitra", Phone: "6281210797235"},
	}
	ctx := directory.WithCaller(context.Background(), caller)

	h := e.runWorkflow(ctx, caller.Number, "", def, nil)

	if h.Aborted != "" {
		t.Fatalf("workflow terhenti: %s", h.Aborted)
	}

	// Radius + MikroTik + GenieACS harus menerima username "jttcitra", BUKAN nomor.
	for name, rec := range map[string]*recWfAdapter{
		"radius":   radiusRec,
		"mikrotik": mikrotikRec,
		"genieacs": genieacsRec,
	} {
		if len(rec.ids) == 0 {
			t.Errorf("%s: adapter tidak dipanggil", name)
			continue
		}
		for _, id := range rec.ids {
			if id == "6281210797235" {
				t.Errorf("%s: menerima nomor telepon %q — harusnya username \"jttcitra\"", name, id)
			}
			if id != "jttcitra" {
				t.Errorf("%s: identity = %q, mau \"jttcitra\"", name, id)
			}
		}
	}
}

// TestRunWorkflowIdentityFallbackToPhone: tanpa caller (mis. pemanggilan dari
// dashboard /api/ask tanpa identitas pelanggan), identity_id tetap nomor pengirim
// (tidak berubah — backward compatible).
func TestRunWorkflowIdentityFallbackToPhone(t *testing.T) {
	reg := writeWfReg(t, true)
	pol := policy.Load("")
	disp := tool.New(reg, pol, time.Second)
	radiusRec := &recWfAdapter{domain: "radius"}
	disp.Register(&recWfAdapter{domain: "billing"})
	disp.Register(radiusRec)
	disp.Register(&recWfAdapter{domain: "mikrotik"})
	disp.Register(&recWfAdapter{domain: "genieacs"})

	wf, _ := workflow.LoadDir("../../workflows")
	def, _ := wf.ForIntent("COMPLAINT")

	e := &Engine{Reg: reg, Disp: disp, Wkf: wf}
	// context.Background() tanpa caller -> harus fallback ke nomor.
	_ = e.runWorkflow(context.Background(), "6281210797235", "", def, nil)

	for _, id := range radiusRec.ids {
		if id != "6281210797235" {
			t.Errorf("tanpa caller, identity = %q, mau fallback ke nomor 6281210797235", id)
		}
	}
}

// writeWfReg menulis registry dengan keempat tool READ untuk workflow.
func writeWfReg(t *testing.T, enabled bool) *registry.Registry {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "registry.yaml")
	en := "false"
	if enabled {
		en = "true"
	}
	content := "version: 1.0.0\ntools:\n" +
		"  - name: billing.get_customer\n    domain: billing\n    enabled: " + en + "\n    permission: READ\n    risk: LOW\n    scope: single_customer\n" +
		"  - name: radius.get_session\n    domain: radius\n    enabled: " + en + "\n    permission: READ\n    risk: LOW\n    scope: single_customer\n" +
		"  - name: mikrotik.get_pppoe_status\n    domain: mikrotik\n    enabled: " + en + "\n    permission: READ\n    risk: LOW\n    scope: single_customer\n" +
		"  - name: genieacs.get_device_state\n    domain: genieacs\n    enabled: " + en + "\n    permission: READ\n    risk: LOW\n    scope: single_customer\n"
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Load(p)
}
