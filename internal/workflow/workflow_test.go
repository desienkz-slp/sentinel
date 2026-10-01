package workflow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSingle(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w.yaml")
	os.WriteFile(p, []byte(`name: customer-internet-down
description: alur investigasi
version: 1.0.0
trigger: CUSTOMER_INTERNET_DOWN
mode: COPILOT
steps:
  - index: 0
    name: resolve_identity
    required_evidence: [identity]
  - index: 1
    name: check_billing
    tool: billing.get_customer
    permission: READ
    risk_level: LOW
`), 0o644)

	r, err := Load(p)
	if err != nil {
		t.Fatal(err)
	}
	d, ok := r.Get("customer-internet-down")
	if !ok {
		t.Fatal("workflow tidak ditemukan")
	}
	if len(d.Steps) != 2 {
		t.Errorf("steps = %d, mau 2", len(d.Steps))
	}
	if d.Steps[1].Tool != "billing.get_customer" || d.Steps[1].Permission != "READ" {
		t.Errorf("step[1] = %+v", d.Steps[1])
	}
	// Trigger lookup.
	if d2, ok := r.ForTrigger("CUSTOMER_INTERNET_DOWN"); !ok || d2.Name != d.Name {
		t.Errorf("ForTrigger gagal: %+v ok=%v", d2, ok)
	}
	// Trigger case-insensitive.
	if _, ok := r.ForTrigger("customer_internet_down"); !ok {
		t.Error("trigger harus case-insensitive")
	}
}

func TestLoadDir(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.yaml"), []byte(`name: a
trigger: INTENT_A
steps: []
`), 0o644)
	os.WriteFile(filepath.Join(dir, "b.yaml"), []byte(`name: b
trigger: INTENT_B
steps: []
`), 0o644)
	// File non-yaml diabaikan.
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte(`x`), 0o644)

	r, err := LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if r.Len() != 2 {
		t.Errorf("Len = %d, mau 2", r.Len())
	}
}

func TestLoadMissingFileEmpty(t *testing.T) {
	r, err := Load(filepath.Join(t.TempDir(), "tidak-ada.yaml"))
	if err != nil {
		t.Fatalf("file hilang harus aman, dapat error: %v", err)
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d, mau 0", r.Len())
	}
}

func TestLoadInvalidRejectsMissingName(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "w.yaml")
	os.WriteFile(p, []byte(`steps: []`), 0o644)
	if _, err := Load(p); err == nil {
		t.Error("workflow tanpa name harus error")
	}
}
