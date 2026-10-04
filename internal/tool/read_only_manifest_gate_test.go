package tool_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"ainoc/internal/billing"
	"ainoc/internal/capability"
	"ainoc/internal/genieacs"
	"ainoc/internal/mikrotik"
	"ainoc/internal/policy"
	"ainoc/internal/radius"
	"ainoc/internal/registry"
	gateway "ainoc/internal/tool"
)

type manifestAdapter struct{ domain string }

func (a manifestAdapter) Domain() string   { return a.domain }
func (a manifestAdapter) Name() string     { return a.domain }
func (a manifestAdapter) Configured() bool { return true }
func (a manifestAdapter) ToolNames() []string {
	return []string{a.domain + ".get_interface_live", a.domain + ".get_customer"}
}
func (a manifestAdapter) Health(context.Context) (string, error) { return "ok", nil }
func (a manifestAdapter) Invoke(context.Context, string, map[string]any) (gateway.Output, error) {
	return gateway.Output{Text: "ok"}, nil
}

func canonicalManifest(t *testing.T) *capability.Manifest {
	t.Helper()
	manifest, err := capability.Load(filepath.Join("..", "..", "tools", "read_only_external_api_manifest.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	return manifest
}

func enabledRegistry(t *testing.T, content string) *registry.Registry {
	t.Helper()
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte("version: 1.0.0\ntools:\n"+content), 0o600); err != nil {
		t.Fatal(err)
	}
	return registry.Load(path)
}

func TestManifestGateAllowsBoundedBillingCustomerIdentity(t *testing.T) {
	reg := enabledRegistry(t, `  - name: billing.get_customer
    domain: billing
    enabled: true
    permission: READ
    risk: LOW
    parameters:
      type: object
      properties:
        identity: {type: string}
      required: [identity]
`)
	d := gateway.New(reg, policy.Load(""), time.Second)
	d.SetCapabilityManifest(canonicalManifest(t))
	d.Register(manifestAdapter{domain: "billing"})

	allowed := d.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "customer-a"})
	if !allowed.OK || allowed.Decision != policy.Allow {
		t.Fatalf("bounded billing customer lookup must be allowed: %+v", allowed)
	}
	blocked := d.Invoke(context.Background(), "billing.get_customer", nil)
	if blocked.OK || blocked.Decision != policy.Deny {
		t.Fatalf("billing lookup without identity must be denied: %+v", blocked)
	}
}

func TestManifestGateRequiresDeclaredBoundedArgument(t *testing.T) {
	reg := enabledRegistry(t, `  - name: mikrotik.get_interface_live
    domain: mikrotik
    enabled: true
    permission: READ
    risk: LOW
    parameters:
      type: object
      properties:
        interface: {type: string}
      required: [interface]
`)
	d := gateway.New(reg, policy.Load(""), time.Second)
	d.SetCapabilityManifest(canonicalManifest(t))
	d.Register(manifestAdapter{domain: "mikrotik"})

	blocked := d.Invoke(context.Background(), "mikrotik.get_interface_live", map[string]any{})
	if blocked.OK || blocked.Decision != policy.Deny {
		t.Fatalf("missing bounded interface must be denied: %+v", blocked)
	}
	allowed := d.Invoke(context.Background(), "mikrotik.get_interface_live", map[string]any{"interface": "ether1"})
	if !allowed.OK || allowed.Decision != policy.Allow {
		t.Fatalf("bounded interface capability should pass manifest gate: %+v", allowed)
	}
}

func TestManifestRejectsSensitiveAndWriteEntries(t *testing.T) {
	for _, manifest := range []string{
		`version: 1.0.0
capabilities:
  - tool: radius.get_user
    domain: radius
    transport: http_get
    operation: read
    required_bounded_argument: identity
    sensitivity: credential
    max_rows: 1
    max_bytes: 1024
    schema_notes: unsafe
`,
		`version: 1.0.0
capabilities:
  - tool: mikrotik.disconnect_pppoe
    domain: mikrotik
    transport: routeros_sentence
    operation: write
    required_bounded_argument: identity
    sensitivity: none
    max_rows: 1
    max_bytes: 1024
    schema_notes: write is forbidden
`,
	} {
		path := filepath.Join(t.TempDir(), "manifest.yaml")
		if err := os.WriteFile(path, []byte(manifest), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := capability.Load(path); err == nil {
			t.Fatalf("unsafe manifest must be rejected: %s", manifest)
		}
	}
}

func TestManifestParityCoversEnabledToolsAndBillingHistoryList(t *testing.T) {
	reg := registry.Load(filepath.Join("..", "..", "tools", "registry.yaml"))
	manifest := canonicalManifest(t)
	adapters := []capability.Adapter{
		billing.New("", ""), radius.New("", ""), mikrotik.New(mikrotik.Config{}), genieacs.New("", ""),
	}
	if err := capability.ValidateRegistryAdapterParity(reg.All(), adapters); err != nil {
		t.Fatal(err)
	}
	if err := capability.ValidateEnabledReadTools(reg.Enabled(), manifest, adapters); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{billing.ToolHistory, billing.ToolList} {
		entry, ok := reg.Get(name)
		if !ok || entry.Permission != registry.PermRead {
			t.Fatalf("billing tool %q must remain a READ registry entry", name)
		}
		if !capability.AdapterImplements(name, adapters) {
			t.Fatalf("billing tool %q must map to exactly one current adapter", name)
		}
		if manifest.Has(name) {
			t.Fatalf("unbounded/sensitive billing tool %q must not be manifest-enabled", name)
		}
	}
}

func TestEveryEnabledReadToolRequiresExactlyOneAdapterAndManifestEntry(t *testing.T) {
	reg := enabledRegistry(t, `  - name: radius.get_system_stats
    domain: radius
    enabled: true
    permission: READ
    risk: LOW
    parameters: {type: object, properties: {}, required: []}
  - name: mikrotik.get_interface_live
    domain: mikrotik
    enabled: true
    permission: READ
    risk: LOW
    parameters:
      type: object
      properties: {interface: {type: string}}
      required: [interface]
`)
	adapters := []capability.Adapter{
		radius.New("", ""), mikrotik.New(mikrotik.Config{}),
	}
	if err := capability.ValidateEnabledReadTools(reg.Enabled(), canonicalManifest(t), adapters); err != nil {
		t.Fatal(err)
	}
}

func TestRadiusBulkSessionAndUserToolsAreAbsentFromCanonicalRegistryAndManifest(t *testing.T) {
	reg := registry.Load(filepath.Join("..", "..", "tools", "registry.yaml"))
	manifest := canonicalManifest(t)
	for _, name := range []string{"radius.get_session", "radius.get_user"} {
		if _, exists := reg.Get(name); exists {
			t.Errorf("bulk PII capability %q must be removed from canonical registry", name)
		}
		if manifest.Has(name) {
			t.Errorf("bulk PII capability %q must not be manifest-enabled", name)
		}
	}
}

func TestManifestIncludesOnlyBoundedPPPoETrafficCapability(t *testing.T) {
	reg := registry.Load(filepath.Join("..", "..", "tools", "registry.yaml"))
	manifest := canonicalManifest(t)
	for _, entry := range reg.All() {
		if entry.Permission != registry.PermRead && manifest.Has(entry.Name) {
			t.Errorf("non-READ tool %q must not be manifest-enabled", entry.Name)
		}
	}
	traffic, ok := reg.Get("mikrotik.get_customer_traffic")
	if !ok {
		t.Fatal("customer traffic must remain a registry tool")
	}
	if !manifest.Has(traffic.Name) {
		t.Fatal("bounded PPPoE interface traffic must be manifest-enabled")
	}
	if err := manifest.Allows(traffic, map[string]any{"identity": "pelanggan-satu"}); err != nil {
		t.Fatalf("bounded PPPoE traffic must pass manifest gate: %v", err)
	}
	if err := manifest.Allows(traffic, map[string]any{}); err == nil {
		t.Fatal("PPPoE traffic without identity must be denied")
	}
	deviceState, ok := reg.Get("genieacs.get_device_state")
	if !ok || !manifest.Has(deviceState.Name) {
		t.Fatal("bounded GenieACS device-state lookup must be manifest-enabled")
	}
	if err := manifest.Allows(deviceState, map[string]any{"device_id": "ONT-001"}); err != nil {
		t.Fatalf("bounded GenieACS device-state lookup must pass manifest gate: %v", err)
	}
	if err := manifest.Allows(deviceState, map[string]any{}); err == nil {
		t.Fatal("GenieACS device-state lookup without device_id must be denied")
	}
	for _, name := range []string{
		billing.ToolHistory,
		billing.ToolList,
		"radius.get_session",
		"radius.get_user",
		"genieacs.get_devices",
	} {
		if manifest.Has(name) {
			t.Errorf("unbounded or sensitive capability %q must not be manifest-enabled", name)
		}
	}
	if _, exists := reg.Get("mikrotik.get_pppoe_interface_traffic"); exists {
		t.Error("future PPPoE interface-traffic capability must stay unimplemented and deny-by-default")
	}
}
