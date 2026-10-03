package policy

import (
	"os"
	"path/filepath"
	"testing"
)

func writePolicyOverlay(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadMergedOverlayReplaceAndAdd(t *testing.T) {
	dir := t.TempDir()
	base := writePolicyOverlay(t, dir, "base.yaml", `
version: 1.0.0
mode: COPILOT
default:
  read: allow
  write: deny
  admin: deny
  unknown: deny
rules:
  - id: readonly
    description: read allow
    match:
      permission: READ
    decision: ALLOW
`)
	ov := writePolicyOverlay(t, dir, "overlay.yaml", `
version: 1.0.0
rules:
  - id: readonly
    description: diganti
    match:
      permission: READ
    decision: DENY
  - id: extra
    description: baru
    match:
      tool: custom.x
    decision: ALLOW
`)

	e := LoadMerged(base, ov)
	rules := e.Rules()
	byID := map[string]Rule{}
	for _, r := range rules {
		byID[r.ID] = r
	}
	if len(rules) != 2 {
		t.Fatalf("mau 2 rule, dapat %d", len(rules))
	}
	if byID["readonly"].Decision != "DENY" {
		t.Fatalf("overlay harus mengganti readonly jadi DENY")
	}
	if byID["readonly"].Description != "diganti" {
		t.Fatalf("deskripsi tidak ikut diganti")
	}
	if byID["extra"].Decision != "ALLOW" {
		t.Fatalf("rule baru harus ada")
	}
}

func TestLoadMergedOverlayRemove(t *testing.T) {
	dir := t.TempDir()
	base := writePolicyOverlay(t, dir, "base.yaml", `
rules:
  - id: keep
    decision: ALLOW
  - id: drop
    decision: ALLOW
`)
	ov := writePolicyOverlay(t, dir, "overlay.yaml", `
rules:
  - id: drop
    decision: REMOVE
`)
	e := LoadMerged(base, ov)
	rules := e.Rules()
	if len(rules) != 1 || rules[0].ID != "keep" {
		t.Fatalf("mau sisa 1 rule keep, dapat %+v", rules)
	}
}

func TestWriteOverlayRoundTrip(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "policy.local.yaml")
	rules := []Rule{
		{ID: "a", Decision: "DENY", Tool: "x.y", Permission: "WRITE"},
		{ID: "b", Decision: "ALLOW", Requires: []string{"r1", "r2"}},
	}
	if err := WriteOverlay(p, rules); err != nil {
		t.Fatal(err)
	}
	// Muat ulang dan pastikan isinya sama.
	e := LoadMerged("", p)
	got := e.Rules()
	if len(got) != 2 {
		t.Fatalf("mau 2 rule, dapat %d", len(got))
	}
	m := map[string]Rule{}
	for _, r := range got {
		m[r.ID] = r
	}
	if m["a"].Permission != "WRITE" || m["a"].Tool != "x.y" {
		t.Fatalf("roundtrip a salah: %+v", m["a"])
	}
	if len(m["b"].Requires) != 2 {
		t.Fatalf("requires b hilang: %+v", m["b"])
	}
}
