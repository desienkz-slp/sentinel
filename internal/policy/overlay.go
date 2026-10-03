// overlay.go — policy overlay lokal yang bisa diedit operator via UI.
//
// Baseline `policies/default-policy.yaml` (ikut rilis) TETAP deny-by-default dan
// tidak pernah diubah oleh UI. Perubahan operator ditulis ke
// `policies/policy.local.yaml` (gitignored) sebagai OVERLAY: aturan yang ID-nya
// sama menggantikan aturan dasar, aturan baru ditambahkan, aturan yang disebut
// "hapus" (decision: "" / REMOVE) dihilangkan.
//
// Ini menjaga kontrak keamanan: rilis baru tidak pernah mengaktifkan/melunakkan
// kebijakan sendiri; hanya operator (superadmin + PIN) yang mengubah overlay.
package policy

import (
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// overlayDoc adalah bentuk penyimpanan overlay (hanya rules yang diubah).
type overlayDoc struct {
	Version string `yaml:"version"`
	Rules   []Rule `yaml:"rules"`
}

// LoadMerged memuat baseline lalu menerapkan overlay (bila ada). Perilaku sama
// dengan Load bila overlay kosong/hilang/rusak.
func LoadMerged(base, overlay string) *Engine {
	e := Load(base)
	if overlay == "" || overlay == base {
		return e
	}
	b, err := os.ReadFile(overlay)
	if err != nil {
		return e
	}
	var d overlayDoc
	if yaml.Unmarshal(b, &d) != nil {
		return e
	}
	applyOverlay(e, d.Rules)
	return e
}

// applyOverlay menggabungkan aturan overlay ke engine:
//   - ID cocok -> ganti aturan dasar.
//   - ID baru -> tambah.
//   - decision kosong/"REMOVE" -> hapus aturan dengan ID itu.
func applyOverlay(e *Engine, rules []Rule) {
	for _, r := range rules {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		if r.Decision == "" || strings.EqualFold(r.Decision, "REMOVE") {
			e.removeRule(id)
			continue
		}
		e.upsertRule(r.toInternal())
	}
}

func (e *Engine) removeRule(id string) {
	kept := e.rules[:0]
	for _, r := range e.rules {
		if !strings.EqualFold(r.ID, id) {
			kept = append(kept, r)
		}
	}
	e.rules = kept
}

func (e *Engine) upsertRule(r rule) {
	for i, ex := range e.rules {
		if strings.EqualFold(ex.ID, r.ID) {
			e.rules[i] = r
			return
		}
	}
	e.rules = append(e.rules, r)
}

// WriteOverlay menulis aturan overlay (bentuk publik) ke path overlay secara
// atomik. Ini cara UI mengubah rules: ia mengambil Rules() saat ini, mengubah
// yang diinginkan, lalu memanggil WriteOverlay dengan SELURUH daftar rule yang
// berbeda dari baseline.
func WriteOverlay(path string, rules []Rule) error {
	doc := overlayDoc{Version: "1.0.0", Rules: rules}
	b, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
