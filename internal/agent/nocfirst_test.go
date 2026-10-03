package agent

import (
	"strings"
	"testing"

	"ainoc/internal/claimguard"
	"ainoc/internal/directory"
	"ainoc/internal/llm"
)

var adminUji = directory.Caller{Number: "628111444555", Role: directory.RoleAdmin, IsStaff: true, Name: "Admin Uji",
	Perms: directory.PermsFor(directory.RoleAdmin)}

func rigNOC(t *testing.T, mode string, steps []fakeStep) *scopeRig {
	r := newScopeRig(t, "off", steps)
	r.eng.Cfg.TeamNOCTools = mode
	return r
}

// Pesan non-keluhan dari staf: tool read dipaparkan hanya saat on.
func TestNOCToolFirstMemaparkanToolHanyaSaatOn(t *testing.T) {
	for _, tc := range []struct {
		mode string
		want bool
	}{{"off", false}, {"shadow", false}, {"on", true}} {
		r := rigNOC(t, tc.mode, []fakeStep{{answer: "baik"}})
		r.run(nocUji, "kenapa tagihan pelanggan budi123 mahal?")
		got := len(r.llm.toolsSeen) > 0 && len(r.llm.toolsSeen[0]) > 0
		if got != tc.want {
			t.Errorf("[%s] tool dipaparkan=%v, mau %v", tc.mode, got, tc.want)
		}
	}
}

// Tool TULIS tidak pernah dipaparkan lewat jalur ini, walau tool tulis AKTIF di
// registry (jadi yang menahannya filter READ, bukan registry yang kebetulan nonaktif).
func TestNOCToolFirstTidakMemaparkanTulis(t *testing.T) {
	r := rigNOC(t, "on", []fakeStep{{answer: "baik"}})
	on := map[string]bool{}
	for _, tl := range r.eng.Reg.All() {
		on[tl.Name] = true // termasuk WRITE
	}
	r.eng.Reg = mergedEnabled(t, r.eng.Reg, on)
	if !strings.Contains(strings.Join(toolNames(r.eng.Reg.LLMTools()), ","), "mikrotik.disconnect_pppoe") {
		t.Fatal("prasyarat: tool tulis harus aktif di registry uji")
	}
	r.run(nocUji, "tolong lihat status budi123")
	seen := strings.Join(r.llm.toolsSeen[0], ",")
	if strings.Contains(seen, "disconnect") {
		t.Fatalf("tool tulis dipaparkan: %s", seen)
	}
	if len(r.llm.toolsSeen[0]) == 0 {
		t.Fatal("tool read harus ada")
	}
}

func toolNames(ts []llm.Tool) []string {
	out := []string{}
	for _, x := range ts {
		out = append(out, x.Function.Name)
	}
	return out
}

// Model mengarang angka tanpa tool -> diganti (on), dibiarkan tapi dicatat (shadow).
func TestNOCToolFirstKlaimKarangan(t *testing.T) {
	karangan := "Pelanggan budi123 tertunggak 3 bulan, total Rp 450.000, sesi radius offline."
	for _, tc := range []struct {
		mode string
		same bool
	}{{"off", true}, {"shadow", true}, {"on", false}} {
		r := rigNOC(t, tc.mode, []fakeStep{{answer: karangan}})
		var ev []NOCToolEvent
		r.eng.NOCHook = func(e NOCToolEvent) { ev = append(ev, e) }
		rep := r.run(nocUji, "kenapa tagihan budi123 mahal?")
		got := rep.Answer
		if tc.same && !strings.Contains(got, "450.000") {
			t.Errorf("[%s] jawaban tidak boleh diubah: %q", tc.mode, got)
		}
		if !tc.same && (strings.Contains(got, "450.000") || !strings.Contains(got, "Belum diperiksa")) {
			t.Errorf("[%s] harus diganti: %q", tc.mode, got)
		}
		switch tc.mode {
		case "off":
			if len(ev) != 0 {
				t.Error("off: tidak boleh ada event")
			}
		case "shadow", "on":
			if len(ev) != 1 || ev[0].Replaced != (tc.mode == "on") {
				t.Errorf("[%s] event: %+v", tc.mode, ev)
			}
		}
	}
}

// Dengan tool call di giliran itu, angka dari hasil tool dibiarkan.
func TestNOCToolFirstDenganToolDibiarkan(t *testing.T) {
	r := rigNOC(t, "on", []fakeStep{
		{calls: []fakeCall{{name: "billing.get_customer", args: map[string]any{"identity": "budi123"}}}},
		{answer: "Billing budi123: tertunggak 2 bulan."},
	})
	rep := r.run(nocUji, "kenapa tagihan budi123 mahal?")
	if !strings.Contains(rep.Answer, "tertunggak 2 bulan") || strings.Contains(rep.Answer, "Belum diperiksa") {
		t.Fatalf("jawaban berbukti tidak boleh diganti: %q", rep.Answer)
	}
	if len(r.adapts["billing"].invoked("billing.get_customer")) != 1 {
		t.Fatal("tool harus benar-benar dipanggil")
	}
}

// Hak peran tetap berlaku: Admin tidak dapat tool jaringan tulis; semua tetap lewat Authorize.
func TestNOCToolFirstHakPeranBerlaku(t *testing.T) {
	r := rigNOC(t, "on", []fakeStep{{answer: "baik"}})
	r.run(adminUji, "lihat status budi123")
	if len(r.llm.toolsSeen) == 0 || len(r.llm.toolsSeen[0]) == 0 {
		t.Fatal("Admin tetap mendapat tool read sesuai hak")
	}
	for _, n := range r.llm.toolsSeen[0] {
		if directory.Authorize(adminUji, n, false).Decision != directory.Allow {
			t.Errorf("tool %s tidak diizinkan untuk Admin tetapi dipaparkan", n)
		}
	}
}

// Pelanggan tidak terpengaruh: tidak dapat tool pada pesan non-keluhan, jawaban tidak disaring.
func TestNOCToolFirstPelangganTidakTerpengaruh(t *testing.T) {
	r := rigNOC(t, "on", []fakeStep{{answer: "Ada 3 pelanggan offline."}})
	rep := r.run(pelangganUji, "halo, apa kabar?")
	if len(r.llm.toolsSeen) > 0 && len(r.llm.toolsSeen[0]) > 0 {
		t.Fatalf("pelanggan tidak boleh dapat tool: %v", r.llm.toolsSeen[0])
	}
	if strings.Contains(rep.Answer, "Belum diperiksa") {
		t.Fatal("penjaga klaim NOC tidak boleh menyentuh jawaban pelanggan")
	}
}

// Pemanggilan operator/dashboard tanpa caller: perilaku lama.
func TestNOCToolFirstTanpaCallerPerilakuLama(t *testing.T) {
	r := rigNOC(t, "on", []fakeStep{{answer: "Ada 3 pelanggan offline."}})
	rep := r.eng.Run(t.Context(), "dashboard", "halo", "")
	if strings.Contains(rep.Answer, "Belum diperiksa") {
		t.Fatal("tanpa caller tidak boleh disentuh")
	}
	_ = claimguard.Notice
}
