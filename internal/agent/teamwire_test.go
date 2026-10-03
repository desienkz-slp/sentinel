package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/directory"
	"ainoc/internal/llm"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/teamscope"
	"ainoc/internal/tool"
	"ainoc/internal/workflow"
)

// ---- LLM palsu (server HTTP kompatibel OpenAI chat/completions) -------------

// fakeLLM memutar skrip tanggapan: tiap panggilan Chat mengambil satu langkah.
// Langkah berisi tool call (model "jahat"/"patuh") atau jawaban akhir.
type fakeLLM struct {
	mu        sync.Mutex
	steps     []fakeStep
	i         int
	toolsSeen [][]string // nama tool yang DIPAPARKAN pada tiap panggilan
}

type fakeStep struct {
	calls  []fakeCall // bila kosong -> jawaban akhir
	answer string
}
type fakeCall struct {
	name string
	args map[string]any
}

func (f *fakeLLM) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Tools []struct {
				Function struct {
					Name string `json:"name"`
				} `json:"function"`
			} `json:"tools"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		names := []string{}
		for _, t := range req.Tools {
			names = append(names, t.Function.Name)
		}
		f.mu.Lock()
		f.toolsSeen = append(f.toolsSeen, names)
		var st fakeStep
		if f.i < len(f.steps) {
			st = f.steps[f.i]
			f.i++
		} else {
			st = fakeStep{answer: "selesai"}
		}
		f.mu.Unlock()

		msg := map[string]any{"role": "assistant", "content": st.answer}
		if len(st.calls) > 0 {
			tcs := []map[string]any{}
			for n, c := range st.calls {
				b, _ := json.Marshal(c.args)
				tcs = append(tcs, map[string]any{
					"id": "call_" + string(rune('a'+n)), "type": "function",
					"function": map[string]any{"name": c.name, "arguments": string(b)},
				})
			}
			msg["tool_calls"] = tcs
			msg["content"] = ""
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": msg}}})
	})
}

// recAdapter merekam setiap pemanggilan tool (nama + argumen) untuk dibuktikan.
type recAdapter struct {
	domain string
	tools  []string
	mu     sync.Mutex
	calls  []recCall
}
type recCall struct {
	tool string
	args map[string]any
}

func (a *recAdapter) Domain() string                         { return a.domain }
func (a *recAdapter) Name() string                           { return a.domain }
func (a *recAdapter) Configured() bool                       { return true }
func (a *recAdapter) ToolNames() []string                    { return a.tools }
func (a *recAdapter) Health(context.Context) (string, error) { return "ok", nil }
func (a *recAdapter) Invoke(_ context.Context, n string, args map[string]any) (tool.Output, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cp := map[string]any{}
	for k, v := range args {
		cp[k] = v
	}
	a.calls = append(a.calls, recCall{n, cp})
	return tool.Output{Text: "status=AKTIF untuk " + n}, nil
}
func (a *recAdapter) invoked(tool string) []recCall {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []recCall
	for _, c := range a.calls {
		if c.tool == tool {
			out = append(out, c)
		}
	}
	return out
}

// scopeRig merakit Engine nyata + registry nyata + dispatcher + adapter perekam.
type scopeRig struct {
	eng    *Engine
	llm    *fakeLLM
	adapts map[string]*recAdapter
	events []ScopeEvent
	mu     sync.Mutex
}

func newScopeRig(t *testing.T, mode string, steps []fakeStep) *scopeRig {
	t.Helper()
	fl := &fakeLLM{steps: steps}
	srv := httptest.NewServer(fl.handler())
	t.Cleanup(srv.Close)

	reg := registry.Load("../../tools/registry.yaml")
	// Aktifkan semua READ (seperti produksi); tool tulis tetap nonaktif.
	all := map[string]bool{}
	for _, tl := range reg.All() {
		if tl.Permission == registry.PermRead {
			all[tl.Name] = true
		}
	}
	reg = mergedEnabled(t, reg, all)

	disp := tool.New(reg, policy.Load("../../policies/default-policy.yaml"), 2*time.Second)
	adapts := map[string]*recAdapter{}
	for _, tl := range reg.All() {
		d := tl.Domain
		if adapts[d] == nil {
			adapts[d] = &recAdapter{domain: d}
		}
		adapts[d].tools = append(adapts[d].tools, tl.Name)
	}
	for _, a := range adapts {
		disp.Register(a)
	}

	cfg := config.Default()
	cfg.TeamCSScope = mode
	client := llm.New(srv.URL, "k", "m", 5)
	eng := New(cfg, client, diag.New(5), nil, nil, nil, nil)
	eng.Reg, eng.Disp = reg, disp
	rig := &scopeRig{eng: eng, llm: fl, adapts: adapts}
	eng.ScopeHook = func(ev ScopeEvent) { rig.mu.Lock(); rig.events = append(rig.events, ev); rig.mu.Unlock() }
	return rig
}

// mergedEnabled membuat registry dengan tool READ aktif lewat overlay sementara.
func mergedEnabled(t *testing.T, base *registry.Registry, on map[string]bool) *registry.Registry {
	t.Helper()
	var b strings.Builder
	b.WriteString("version: 1.0.0\ntools:\n")
	for n := range on {
		b.WriteString("  - name: " + n + "\n    enabled: true\n")
	}
	p := t.TempDir() + "/overlay.yaml"
	if err := writeFile(p, b.String()); err != nil {
		t.Fatal(err)
	}
	return registry.LoadMerged("../../tools/registry.yaml", p)
}

func (r *scopeRig) run(c directory.Caller, query string) Report {
	ctx := directory.WithCaller(context.Background(), c)
	return r.eng.Run(ctx, c.Number, query, "")
}

var (
	pelangganUji = directory.Caller{Number: "628111000111", Role: directory.RoleCustomer, IsCustomer: true, Name: "Pelanggan Uji",
		Customer: &directory.CustomerInfo{Name: "Pelanggan Uji", Username: "pelanggan-uji", Phone: "628111000111"}}
	nocUji = directory.Caller{Number: "628111222333", Role: directory.RoleNOCSenior, IsStaff: true, Name: "NOC Uji",
		Perms: directory.PermsFor(directory.RoleNOCSenior)}
)

// ---- Skenario ---------------------------------------------------------------

const keluhan = "internet saya mati dari pagi, tolong dicek"

// MODEL JAHAT, mode ON: CS meminta daftar pelanggan dan data pelanggan lain.
func TestScopeOnModelJahatTidakBisaMembacaDataLain(t *testing.T) {
	rig := newScopeRig(t, "on", []fakeStep{
		{calls: []fakeCall{
			{"billing.list_customers", map[string]any{"filter": "isolir"}},
			{"billing.get_customer", map[string]any{"identity": "pelanggan-orang-lain"}},
			{"radius.get_session", map[string]any{"username": "korban-02"}},
			{"mikrotik.disconnect_pppoe", map[string]any{"identity": "pelanggan-uji"}},
			{"radius.get_system_stats", map[string]any{}},
		}},
		{answer: "BALASAN: sudah kami cek"},
	})
	rig.run(pelangganUji, keluhan)

	if n := len(rig.adapts["billing"].invoked("billing.list_customers")); n != 0 {
		t.Errorf("billing.list_customers dieksekusi %d kali untuk CS", n)
	}
	if n := len(rig.adapts["radius"].invoked("radius.get_system_stats")); n != 0 {
		t.Errorf("radius.get_system_stats dieksekusi %d kali untuk CS", n)
	}
	if n := len(rig.adapts["mikrotik"].invoked("mikrotik.disconnect_pppoe")); n != 0 {
		t.Errorf("TOOL TULIS dieksekusi %d kali untuk CS", n)
	}
	// Yang boleh (data pelanggan): identitas HARUS pelanggan-uji, bukan milik orang lain.
	for _, c := range rig.adapts["billing"].invoked("billing.get_customer") {
		if c.args["identity"] != "pelanggan-uji" {
			t.Errorf("billing.get_customer dipanggil dengan identity %v, harus pelanggan-uji", c.args["identity"])
		}
		for _, k := range []string{"username", "name", "search", "phone"} {
			if _, ada := c.args[k]; ada {
				t.Errorf("kunci %q lolos ke adapter", k)
			}
		}
	}
	for _, c := range rig.adapts["radius"].invoked("radius.get_session") {
		if c.args["identity"] != "pelanggan-uji" {
			t.Errorf("radius.get_session dipanggil dengan %v, harus pelanggan-uji", c.args)
		}
		if _, ada := c.args["username"]; ada {
			t.Errorf("username dari model lolos ke adapter: %v", c.args)
		}
	}
	// Penolakan dan penimpaan tercatat.
	if len(rig.events) == 0 {
		t.Error("penolakan/penimpaan harus tercatat lewat ScopeHook")
	}
}

// Daftar tool yang DIPAPARKAN ke model untuk CS pada mode ON harus disaring.
func TestScopeOnToolDipaparkanDisaring(t *testing.T) {
	rig := newScopeRig(t, "on", []fakeStep{{answer: "BALASAN: ok"}})
	rig.run(pelangganUji, keluhan)
	if len(rig.llm.toolsSeen) == 0 {
		t.Fatal("LLM tidak dipanggil")
	}
	for _, name := range rig.llm.toolsSeen[0] {
		if !teamscopeAllows(name) {
			t.Errorf("tool %q dipaparkan ke model untuk CS", name)
		}
	}
	for _, bad := range []string{"billing.list_customers", "radius.get_system_stats", "mikrotik.get_interface_stats", "genieacs.get_devices", "mikrotik.disconnect_pppoe", "traceroute", "interface", "system"} {
		for _, name := range rig.llm.toolsSeen[0] {
			if name == bad {
				t.Errorf("tool terlarang %q dipaparkan ke model CS", bad)
			}
		}
	}
}

// NOC tidak dibatasi pada mode ON: semua tool terpapar dan argumen tak diubah.
func TestScopeOnNOCTidakDibatasi(t *testing.T) {
	rig := newScopeRig(t, "on", []fakeStep{
		{calls: []fakeCall{{"billing.list_customers", map[string]any{"filter": "isolir"}},
			{"billing.get_customer", map[string]any{"identity": "pelanggan-lain"}}}},
		{answer: "BALASAN: ok"},
	})
	rig.run(nocUji, "tolong cek kenapa internet pelanggan-lain mati dari pagi")
	// NOC: intent mungkin tidak boleh probe; yang diuji adalah TIDAK ADA penolakan batas tim.
	for _, ev := range rig.events {
		t.Errorf("NOC tidak boleh memicu batas tim: %+v", ev)
	}
	for _, c := range rig.adapts["billing"].invoked("billing.get_customer") {
		if c.args["identity"] != "pelanggan-lain" {
			t.Errorf("argumen NOC diubah: %v", c.args)
		}
	}
}

// Mode SHADOW: tidak mengubah apa pun, tetapi MENCATAT apa yang akan dilakukan.
func TestScopeShadowMencatatTanpaMenegakkan(t *testing.T) {
	rig := newScopeRig(t, "shadow", []fakeStep{
		{calls: []fakeCall{{"billing.get_customer", map[string]any{"identity": "pelanggan-orang-lain"}}}},
		{answer: "BALASAN: ok"},
	})
	rig.run(pelangganUji, keluhan)
	calls := rig.adapts["billing"].invoked("billing.get_customer")
	if len(calls) == 0 {
		t.Skip("intent tidak memicu tool pada skenario ini")
	}
	if calls[0].args["identity"] != "pelanggan-orang-lain" {
		t.Errorf("shadow tidak boleh mengubah argumen: %v", calls[0].args)
	}
	if len(rig.events) == 0 {
		t.Error("shadow harus mencatat percobaan lintas pelanggan")
	}
	for _, ev := range rig.events {
		if ev.Mode != "shadow" {
			t.Errorf("mode event = %q, mau shadow", ev.Mode)
		}
	}
}

// Mode OFF: perilaku lama persis — tidak ada penyaringan, tidak ada pencatatan.
func TestScopeOffPerilakuLama(t *testing.T) {
	rig := newScopeRig(t, "off", []fakeStep{
		{calls: []fakeCall{{"billing.get_customer", map[string]any{"identity": "pelanggan-orang-lain"}}}},
		{answer: "BALASAN: ok"},
	})
	rig.run(pelangganUji, keluhan)
	if len(rig.events) != 0 {
		t.Errorf("off tidak boleh mencatat apa pun: %+v", rig.events)
	}
	for _, c := range rig.adapts["billing"].invoked("billing.get_customer") {
		if c.args["identity"] != "pelanggan-orang-lain" {
			t.Errorf("off tidak boleh mengubah argumen: %v", c.args)
		}
	}
}

// INVARIAN TERHADAP MODEL: tiga "model" berbeda perilaku, hasil keamanan sama.
func TestScopeInvarianTerhadapModel(t *testing.T) {
	perilaku := map[string][]fakeStep{
		"patuh": {{calls: []fakeCall{{"billing.get_customer", map[string]any{"identity": "pelanggan-uji"}}}}, {answer: "BALASAN: ok"}},
		"ngawur": {{calls: []fakeCall{
			{"billing.list_customers", map[string]any{}}, {"radius.get_user", map[string]any{"username": "x"}},
			{"billing.get_customer", map[string]any{"identity": "orang-lain", "phone": "628999"}}}}, {answer: "BALASAN: ok"}},
		"jahat": {{calls: []fakeCall{
			{"mikrotik.disconnect_pppoe", map[string]any{"identity": "pelanggan-uji"}},
			{"genieacs.get_devices", map[string]any{}},
			{"billing.get_history", map[string]any{"search": "'; DROP TABLE x;--"}}}}, {answer: "BALASAN: ok"}},
	}
	for nama, steps := range perilaku {
		rig := newScopeRig(t, "on", steps)
		rig.run(pelangganUji, keluhan)
		for _, a := range rig.adapts {
			for _, c := range a.calls {
				switch c.tool {
				case "billing.list_customers", "radius.get_user", "radius.get_system_stats",
					"genieacs.get_devices", "mikrotik.disconnect_pppoe":
					t.Errorf("[%s] tool terlarang dieksekusi: %s", nama, c.tool)
				}
				if id, ada := c.args["identity"]; ada && id != "pelanggan-uji" {
					t.Errorf("[%s] %s identity=%v bukan pelanggan-uji", nama, c.tool, id)
				}
				for _, k := range []string{"username", "phone", "search", "name", "customer", "device_id"} {
					if _, ada := c.args[k]; ada {
						t.Errorf("[%s] %s: kunci %q lolos", nama, c.tool, k)
					}
				}
			}
		}
	}
}

func writeFile(p, s string) error { return os.WriteFile(p, []byte(s), 0o644) }

func teamscopeAllows(name string) bool { return teamscope.CSAllowsTool(name) }

// Jalur WORKFLOW deterministik juga lewat pembatas tim. Workflow nyata
// dijalankan dengan identity di parameter (${identity_id}); untuk CS, apa yang
// sampai ke adapter harus selalu identitas pelanggan terverifikasi.
func TestScopeOnWorkflowCSIdentitasDipaksa(t *testing.T) {
	rig := newScopeRig(t, "on", nil)
	wf, err := workflow.LoadDir("../../workflows")
	if err != nil {
		t.Fatal(err)
	}
	def, ok := wf.ForIntent("COMPLAINT")
	if !ok {
		t.Fatal("workflow COMPLAINT tidak ada")
	}
	rig.eng.Wkf = wf
	ctx := directory.WithCaller(context.Background(), pelangganUji)
	// identity pengirim sengaja BERBEDA dari username terverifikasi.
	h := rig.eng.runWorkflow(ctx, "628999888777", "", def, func(Step) {})
	if !h.RanAnyTool {
		t.Fatal("workflow seharusnya menjalankan tool")
	}
	total := 0
	for _, a := range rig.adapts {
		for _, c := range a.calls {
			total++
			if id, ada := c.args["identity"]; ada && id != "pelanggan-uji" {
				t.Errorf("workflow CS: %s identity=%v, harus pelanggan-uji", c.tool, id)
			}
			for _, k := range []string{"device_id", "customer_id", "username"} {
				if v, ada := c.args[k]; ada && v != "pelanggan-uji" {
					t.Errorf("workflow CS: %s %s=%v lolos dari pembatas", c.tool, k, v)
				}
			}
		}
	}
	if total == 0 {
		t.Fatal("tidak ada pemanggilan tool tercatat")
	}
}

// Workflow untuk NOC/operator (tanpa caller) tidak berubah.
func TestScopeOnWorkflowNOCTidakDiubah(t *testing.T) {
	rig := newScopeRig(t, "on", nil)
	wf, _ := workflow.LoadDir("../../workflows")
	def, _ := wf.ForIntent("COMPLAINT")
	rig.eng.Wkf = wf
	ctx := directory.WithCaller(context.Background(), nocUji)
	rig.eng.runWorkflow(ctx, "628999888777", "", def, func(Step) {})
	for _, ev := range rig.events {
		t.Errorf("NOC tidak boleh memicu batas tim: %+v", ev)
	}
	found := false
	for _, c := range rig.adapts["billing"].invoked("billing.get_customer") {
		found = true
		if c.args["identity"] != "628999888777" {
			t.Errorf("identity NOC diubah: %v", c.args["identity"])
		}
	}
	if !found {
		t.Error("billing.get_customer tidak dipanggil di workflow NOC")
	}
}
