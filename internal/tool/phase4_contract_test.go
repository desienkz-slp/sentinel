package tool_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"ainoc/internal/billing"
	"ainoc/internal/genieacs"
	"ainoc/internal/mikrotik"
	"ainoc/internal/policy"
	"ainoc/internal/radius"
	"ainoc/internal/registry"
	gateway "ainoc/internal/tool"
)

// TestPhase4ReadOnlyAdaptersRegisteredAndDisabled memastikan semua adapter
// eksternal Phase 4 hanya dapat dijangkau melalui registry dan policy. Registry
// produksi harus tetap deny-by-default sampai operator menyalakan tool secara
// eksplisit setelah kontrak staging tervalidasi.
func TestPhase4ReadOnlyAdaptersRegisteredAndDisabled(t *testing.T) {
	reg := registry.Load(filepath.Join("..", "..", "tools", "registry.yaml"))
	pol := policy.Load(filepath.Join("..", "..", "policies", "default-policy.yaml"))
	dispatcher := gateway.New(reg, pol, time.Second)

	adapters := []gateway.Adapter{
		billing.New("", ""),
		radius.New("", ""),
		mikrotik.New(mikrotik.Config{}),
		genieacs.New("", ""),
	}

	for _, a := range adapters {
		dispatcher.Register(a)
		for _, toolName := range a.ToolNames() {
			entry, ok := reg.Get(toolName)
			if !ok {
				t.Errorf("tool %q dari adapter %s belum tercatat di registry", toolName, a.Domain())
				continue
			}
			if entry.Domain != a.Domain() {
				t.Errorf("domain registry tool %q = %q, mau %q", toolName, entry.Domain, a.Domain())
			}
			if entry.Permission != registry.PermRead {
				t.Errorf("tool staged %q permission = %s, mau READ", toolName, entry.Permission)
			}
			if entry.Enabled {
				t.Errorf("tool staged %q tidak boleh aktif secara default", toolName)
			}

			result := dispatcher.Invoke(context.Background(), toolName, map[string]any{})
			if result.OK || result.Decision != policy.Deny {
				t.Errorf("tool nonaktif %q harus berhenti pada gateway: %+v", toolName, result)
			}
		}
	}
}
