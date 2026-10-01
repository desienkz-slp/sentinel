package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func writeReg(t *testing.T, content string) string {
	dir := t.TempDir()
	p := filepath.Join(dir, "registry.yaml")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAndFilterEnabled(t *testing.T) {
	p := writeReg(t, `version: 1.0.0
tools:
  - name: billing.get_customer
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
  - name: mikrotik.disconnect_pppoe
    domain: mikrotik
    enabled: false
    permission: WRITE
    risk: MEDIUM
    scope: single_customer
`)
	r := Load(p)
	total, active := r.Count()
	if total != 2 || active != 1 {
		t.Fatalf("count = (%d,%d), mau (2,1)", total, active)
	}
	en := r.Enabled()
	if len(en) != 1 || en[0].Name != "billing.get_customer" {
		t.Errorf("Enabled() = %+v, mau hanya billing.get_customer", en)
	}
}

func TestCanInvokeGatesDisabled(t *testing.T) {
	p := writeReg(t, `version: 1.0.0
tools:
  - name: radius.get_session
    domain: radius
    enabled: false
    permission: READ
    risk: LOW
`)
	r := Load(p)
	if _, err := r.CanInvoke("radius.get_session"); err == nil {
		t.Error("tool nonaktif harus ditolak CanInvoke")
	}
	if _, err := r.CanInvoke("tool.tidak.ada"); err == nil {
		t.Error("tool tak terdaftar harus ditolak CanInvoke")
	}
}

func TestLoadMissingFileEmpty(t *testing.T) {
	r := Load(filepath.Join(t.TempDir(), "tidak-ada.yaml"))
	if total, active := r.Count(); total != 0 || active != 0 {
		t.Errorf("file hilang -> count (%d,%d), mau (0,0)", total, active)
	}
	if _, err := r.CanInvoke("apa.saja"); err == nil {
		t.Error("registry kosong harus menolak semua pemanggilan")
	}
}

func TestAllPreservesDisabled(t *testing.T) {
	p := writeReg(t, `version: 1.0.0
tools:
  - name: genieacs.get_device_state
    domain: genieacs
    enabled: false
    permission: READ
    risk: LOW
`)
	r := Load(p)
	all := r.All()
	if len(all) != 1 || all[0].Enabled {
		t.Errorf("All() harus mengembalikan tool nonaktif juga: %+v", all)
	}
}
