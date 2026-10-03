package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func writeNamed(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const baseYAML = `version: 1.0.0
tools:
  - name: billing.get_customer
    domain: billing
    enabled: false
    permission: READ
    risk: LOW
    description: dasar-A
    parameters: {type: object, properties: {identity: {type: string}}, required: [identity]}
  - name: billing.get_history
    domain: billing
    enabled: false
    permission: READ
    risk: LOW
    description: dasar-B
  - name: mikrotik.disconnect_pppoe
    domain: mikrotik
    enabled: false
    permission: WRITE
    risk: MEDIUM
    description: dasar-W
`

func enabledSet(r *Registry) map[string]bool {
	m := map[string]bool{}
	for _, t := range r.Enabled() {
		m[t.Name] = true
	}
	return m
}

// Overlay parsial: hanya satu tool disebut. Tool baru di dasar TIDAK ikut aktif
// (deny-by-default), tapi TERDAFTAR sehingga tinggal diaktifkan operator.
func TestMergedOverlayParsialToolBaruTetapNonaktif(t *testing.T) {
	dir := t.TempDir()
	base := writeNamed(t, dir, "registry.yaml", baseYAML)
	ov := writeNamed(t, dir, "registry.local.yaml", "version: 1.0.0\ntools:\n  - name: billing.get_customer\n    enabled: true\n")
	r := LoadMerged(base, ov)
	on := enabledSet(r)
	if !on["billing.get_customer"] {
		t.Fatal("overlay mengaktifkan billing.get_customer, harus aktif")
	}
	if on["billing.get_history"] || on["mikrotik.disconnect_pppoe"] {
		t.Fatalf("tool yang tak disebut overlay harus nonaktif: %v", on)
	}
	if _, ok := r.Get("billing.get_history"); !ok {
		t.Fatal("tool baru di dasar harus tetap terdaftar")
	}
}

// Definisi tool SELALU dari dasar: overlay lama yang lengkap tidak boleh
// menggantikan deskripsi/parameter terbaru dari rilis.
func TestMergedDefinisiDariDasar(t *testing.T) {
	dir := t.TempDir()
	base := writeNamed(t, dir, "registry.yaml", baseYAML)
	ov := writeNamed(t, dir, "registry.local.yaml", `version: 1.0.0
tools:
  - name: billing.get_customer
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
    description: LAMA-usang
`)
	r := LoadMerged(base, ov)
	got, _ := r.Get("billing.get_customer")
	if got.Description != "dasar-A" {
		t.Fatalf("deskripsi harus dari dasar, dapat %q", got.Description)
	}
	if got.Parameters == nil {
		t.Fatal("parameter dari dasar harus dipertahankan")
	}
	if !got.Enabled {
		t.Fatal("enabled harus dari overlay")
	}
}

// Overlay lengkap lama (semua tool + enabled:true) tetap berfungsi, dan tool
// WRITE yang overlay tulis false tetap false.
func TestMergedOverlayLengkapLamaKompatibel(t *testing.T) {
	dir := t.TempDir()
	base := writeNamed(t, dir, "registry.yaml", baseYAML)
	ov := writeNamed(t, dir, "registry.local.yaml", `version: 1.0.0
tools:
  - name: billing.get_customer
    enabled: true
  - name: billing.get_history
    enabled: true
  - name: mikrotik.disconnect_pppoe
    enabled: false
`)
	on := enabledSet(LoadMerged(base, ov))
	if !on["billing.get_customer"] || !on["billing.get_history"] {
		t.Fatalf("READ yang diaktifkan overlay harus aktif: %v", on)
	}
	if on["mikrotik.disconnect_pppoe"] {
		t.Fatal("WRITE nonaktif di overlay harus tetap nonaktif")
	}
}

// Tool yang HANYA ada di overlay dipertahankan (kompat mundur / tool operator).
func TestMergedToolHanyaDiOverlayDipertahankan(t *testing.T) {
	dir := t.TempDir()
	base := writeNamed(t, dir, "registry.yaml", baseYAML)
	ov := writeNamed(t, dir, "registry.local.yaml", "version: 1.0.0\ntools:\n  - name: custom.operator_tool\n    domain: custom\n    enabled: true\n    permission: READ\n")
	r := LoadMerged(base, ov)
	if got, ok := r.Get("custom.operator_tool"); !ok || !got.Enabled {
		t.Fatalf("tool khusus overlay harus dimuat dan aktif: %+v ok=%v", got, ok)
	}
}

// Overlay hilang / kosong / rusak / sama dengan dasar -> identik dengan Load(base).
func TestMergedOverlayTakValidSamaDenganDasar(t *testing.T) {
	dir := t.TempDir()
	base := writeNamed(t, dir, "registry.yaml", baseYAML)
	rusak := writeNamed(t, dir, "rusak.yaml", "tools: [ ini bukan: yaml valid")
	kosong := writeNamed(t, dir, "kosong.yaml", "")
	want := len(LoadMerged(base, "").All())
	for name, ov := range map[string]string{
		"hilang": filepath.Join(dir, "tidak-ada.yaml"), "rusak": rusak, "kosong": kosong, "sama": base, "path-kosong": "",
	} {
		r := LoadMerged(base, ov)
		if len(r.All()) != want || len(r.Enabled()) != 0 {
			t.Errorf("overlay %s: total=%d aktif=%d, mau total=%d aktif=0", name, len(r.All()), len(r.Enabled()), want)
		}
	}
}

// Dasar hilang tetapi overlay ada: overlay saja yang dimuat (tidak panik).
func TestMergedDasarHilang(t *testing.T) {
	dir := t.TempDir()
	ov := writeNamed(t, dir, "registry.local.yaml", "version: 1.0.0\ntools:\n  - name: a.b\n    enabled: true\n    permission: READ\n")
	r := LoadMerged(filepath.Join(dir, "tidak-ada.yaml"), ov)
	if _, ok := r.Get("a.b"); !ok {
		t.Fatal("overlay harus dimuat walau dasar hilang")
	}
}

// Rilis tidak pernah mengaktifkan tool sendiri: dasar enabled:false + tanpa overlay.
func TestMergedRilisTidakMengaktifkanSendiri(t *testing.T) {
	dir := t.TempDir()
	base := writeNamed(t, dir, "registry.yaml", baseYAML)
	if n := len(LoadMerged(base, "").Enabled()); n != 0 {
		t.Fatalf("tanpa overlay tak boleh ada tool aktif, dapat %d", n)
	}
}
