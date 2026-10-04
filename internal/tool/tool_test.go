package tool

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ainoc/internal/capability"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
)

// fakeAdapter adalah adapter uji dengan tool read-only.
type fakeAdapter struct {
	domain     string
	configured bool
	fail       bool
}

func (f *fakeAdapter) Domain() string      { return f.domain }
func (f *fakeAdapter) Name() string        { return f.domain }
func (f *fakeAdapter) Configured() bool    { return f.configured }
func (f *fakeAdapter) ToolNames() []string { return []string{f.domain + ".get_status"} }
func (f *fakeAdapter) Health(ctx context.Context) (string, error) {
	if f.fail {
		return "", errors.New("down")
	}
	return "ok", nil
}
func (f *fakeAdapter) Invoke(ctx context.Context, tool string, args map[string]any) (Output, error) {
	if f.fail {
		return Output{}, errors.New("gagal")
	}
	return Output{Data: map[string]any{"status": "ACTIVE"}, Text: "active"}, nil
}

func writeReg(t *testing.T, tools string) *registry.Registry {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "registry.yaml")
	content := "version: 1.0.0\ntools:\n" + tools
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return registry.Load(p)
}

func testCapabilityManifest(t *testing.T, tool, domain string) *capability.Manifest {
	t.Helper()
	path := filepath.Join(t.TempDir(), "manifest.yaml")
	content := "version: 1.0.0\ncapabilities:\n  - tool: " + tool + "\n    domain: " + domain + "\n    transport: test_fixed_aggregate\n    operation: read\n    required_bounded_argument: none_fixed_aggregate\n    sensitivity: none\n    max_rows: 1\n    max_bytes: 1024\n    schema_notes: synthetic test capability\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, err := capability.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func TestInvokeThroughGates(t *testing.T) {
	reg := writeReg(t, `  - name: billing.get_status
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
    scope: single_customer
`)
	pol := policy.Load("") // deny-by-default: READ single_customer LOW -> ALLOW
	d := New(reg, pol, time.Second)
	d.SetCapabilityManifest(testCapabilityManifest(t, "billing.get_status", "billing"))
	d.Register(&fakeAdapter{domain: "billing", configured: true})

	res := d.Invoke(context.Background(), "billing.get_status", nil)
	if res.OK != true {
		t.Fatalf("invoke seharusnya OK: %+v", res)
	}
	if res.Decision != policy.Allow {
		t.Errorf("decision = %s, mau ALLOW", res.Decision)
	}
	if res.RequestID == "" {
		t.Error("request_id kosong")
	}
}

func TestInvokeDisabledToolDenied(t *testing.T) {
	reg := writeReg(t, `  - name: mikrotik.disconnect
    domain: mikrotik
    enabled: false
    permission: WRITE
    risk: MEDIUM
    scope: single_customer
`)
	pol := policy.Load("")
	d := New(reg, pol, time.Second)
	d.Register(&fakeAdapter{domain: "mikrotik"})

	res := d.Invoke(context.Background(), "mikrotik.disconnect", nil)
	if res.OK {
		t.Error("tool nonaktif harus ditolak")
	}
	if res.Decision != policy.Deny {
		t.Errorf("decision = %s, mau DENY", res.Decision)
	}
}

func TestInvokeUnregisteredDenied(t *testing.T) {
	reg := writeReg(t, `  - name: billing.get_status
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
`)
	pol := policy.Load("")
	d := New(reg, pol, time.Second)

	res := d.Invoke(context.Background(), "tool.tidak.ada", nil)
	if res.OK {
		t.Error("tool tak terdaftar harus ditolak")
	}
	if res.Decision != policy.Deny {
		t.Errorf("decision = %s, mau DENY", res.Decision)
	}
}

// TestInvokeWithoutRegistryDenied memastikan gateway gagal tertutup. Adapter
// tidak boleh dapat dieksekusi hanya karena registry belum berhasil dimuat.
func TestInvokeWithoutRegistryDenied(t *testing.T) {
	d := New(nil, policy.Load(""), time.Second)
	d.Register(&fakeAdapter{domain: "billing", configured: true})

	res := d.Invoke(context.Background(), "billing.get_status", nil)
	if res.OK {
		t.Fatal("tanpa registry, adapter harus ditolak")
	}
	if res.Decision != policy.Deny {
		t.Errorf("decision = %s, mau DENY", res.Decision)
	}
	if res.Error == "" {
		t.Error("penolakan harus memiliki alasan untuk audit")
	}
}

// TestInvokeWithoutPolicyDenied memastikan kebijakan juga wajib tersedia.
func TestInvokeWithoutPolicyDenied(t *testing.T) {
	reg := writeReg(t, `  - name: billing.get_status
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
`)
	d := New(reg, nil, time.Second)
	d.Register(&fakeAdapter{domain: "billing", configured: true})

	res := d.Invoke(context.Background(), "billing.get_status", nil)
	if res.OK {
		t.Fatal("tanpa policy, adapter harus ditolak")
	}
	if res.Decision != policy.Deny {
		t.Errorf("decision = %s, mau DENY", res.Decision)
	}
}

func TestInvokePolicyDeniesWriteMultiCustomer(t *testing.T) {
	// Tool terdaftar & aktif, tapi policy menolak WRITE multi_customer.
	reg := writeReg(t, `  - name: mikrotik.reboot
    domain: mikrotik
    enabled: true
    permission: WRITE
    risk: HIGH
    scope: multi_customer
`)
	pol := policy.Load("") // default WRITE = deny
	d := New(reg, pol, time.Second)
	d.Register(&fakeAdapter{domain: "mikrotik"})

	res := d.Invoke(context.Background(), "mikrotik.reboot", nil)
	if res.OK {
		t.Error("WRITE multi_customer harus ditolak policy")
	}
	if res.Decision != policy.Deny {
		t.Errorf("decision = %s, mau DENY", res.Decision)
	}
}

func TestInvokeAdapterError(t *testing.T) {
	reg := writeReg(t, `  - name: billing.get_status
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
`)
	pol := policy.Load("")
	d := New(reg, pol, time.Second)
	d.Register(&fakeAdapter{domain: "billing", configured: true, fail: true})

	res := d.Invoke(context.Background(), "billing.get_status", nil)
	if res.OK {
		t.Error("adapter gagal harus OK=false")
	}
	if res.Error == "" {
		t.Error("error harus terisi")
	}
}

func TestNoAdapterError(t *testing.T) {
	reg := writeReg(t, `  - name: billing.get_status
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
`)
	pol := policy.Load("")
	d := New(reg, pol, time.Second)
	// Tidak Register adapter -> invoke harus gagal dengan pesan jelas.

	res := d.Invoke(context.Background(), "billing.get_status", nil)
	if res.OK {
		t.Error("tanpa adapter harus gagal")
	}
	if res.Decision != policy.Deny {
		t.Errorf("decision = %s, mau DENY", res.Decision)
	}
}

func TestAdaptersList(t *testing.T) {
	d := New(nil, nil, time.Second)
	d.Register(&fakeAdapter{domain: "billing"})
	d.Register(&fakeAdapter{domain: "radius"})
	got := d.Adapters()
	if len(got) != 2 || got[0] != "billing" || got[1] != "radius" {
		t.Errorf("Adapters = %v, mau [billing radius] terurut", got)
	}
}
