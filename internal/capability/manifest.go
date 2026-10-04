// Package capability loads and enforces the static external read-only contract.
package capability

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"ainoc/internal/registry"

	"gopkg.in/yaml.v3"
)

const fixedAggregate = "none_fixed_aggregate"

// Adapter is the minimal adapter contract used for static registry parity.
type Adapter interface {
	Domain() string
	ToolNames() []string
}

// Entry is one explicitly approved external read capability.
type Entry struct {
	Tool            string `yaml:"tool"`
	Domain          string `yaml:"domain"`
	Transport       string `yaml:"transport"`
	Operation       string `yaml:"operation"`
	RequiredBounded string `yaml:"required_bounded_argument"`
	Sensitivity     string `yaml:"sensitivity"`
	MaxRows         int    `yaml:"max_rows"`
	MaxBytes        int    `yaml:"max_bytes"`
	SchemaNotes     string `yaml:"schema_notes"`
}

type document struct {
	Version      string  `yaml:"version"`
	Capabilities []Entry `yaml:"capabilities"`
}

// Manifest is an immutable, deny-by-default capability allowlist.
type Manifest struct {
	entries map[string]Entry
}

// DenyAll returns an explicit empty manifest for fail-closed startup failures.
func DenyAll() *Manifest {
	return &Manifest{entries: map[string]Entry{}}
}

// Load validates a machine-readable manifest. Invalid or unsafe entries reject
// the complete manifest rather than being silently ignored.
func Load(path string) (*Manifest, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read capability manifest: %w", err)
	}
	var doc document
	if err := yaml.Unmarshal(body, &doc); err != nil {
		return nil, fmt.Errorf("parse capability manifest: %w", err)
	}
	if strings.TrimSpace(doc.Version) == "" {
		return nil, fmt.Errorf("capability manifest version is required")
	}
	manifest := &Manifest{entries: make(map[string]Entry, len(doc.Capabilities))}
	for _, entry := range doc.Capabilities {
		if err := validateEntry(entry); err != nil {
			return nil, err
		}
		if _, exists := manifest.entries[entry.Tool]; exists {
			return nil, fmt.Errorf("duplicate capability manifest tool %q", entry.Tool)
		}
		manifest.entries[entry.Tool] = entry
	}
	return manifest, nil
}

func validateEntry(entry Entry) error {
	if strings.TrimSpace(entry.Tool) == "" || strings.TrimSpace(entry.Domain) == "" || strings.TrimSpace(entry.Transport) == "" || strings.TrimSpace(entry.RequiredBounded) == "" || strings.TrimSpace(entry.SchemaNotes) == "" {
		return fmt.Errorf("capability manifest entry has missing required fields")
	}
	if entry.Operation != "read" {
		return fmt.Errorf("capability %q operation must be read", entry.Tool)
	}
	if entry.Sensitivity != "none" {
		return fmt.Errorf("capability %q is sensitive and cannot be manifest-enabled", entry.Tool)
	}
	if entry.MaxRows < 1 || entry.MaxBytes < 1 {
		return fmt.Errorf("capability %q must declare positive max_rows and max_bytes", entry.Tool)
	}
	return nil
}

// Has reports whether the capability is explicitly allowlisted.
func (m *Manifest) Has(tool string) bool {
	if m == nil {
		return false
	}
	_, ok := m.entries[tool]
	return ok
}

// Allows validates registry metadata and concrete invocation arguments. A nil
// manifest, absent entry, non-READ tool, or missing bounded argument is denied.
func (m *Manifest) Allows(tool registry.Tool, args map[string]any) error {
	if m == nil {
		return fmt.Errorf("read-only capability manifest is not loaded")
	}
	entry, ok := m.entries[tool.Name]
	if !ok {
		return fmt.Errorf("tool %q is absent from the read-only capability manifest", tool.Name)
	}
	if tool.Permission != registry.PermRead {
		return fmt.Errorf("tool %q is %s, not READ", tool.Name, tool.Permission)
	}
	if tool.Domain != entry.Domain {
		return fmt.Errorf("tool %q domain %q does not match manifest domain %q", tool.Name, tool.Domain, entry.Domain)
	}
	if entry.RequiredBounded == fixedAggregate {
		return nil
	}
	if !registryRequires(tool, entry.RequiredBounded) {
		return fmt.Errorf("tool %q must require bounded argument %q in its registry schema", tool.Name, entry.RequiredBounded)
	}
	value, ok := args[entry.RequiredBounded].(string)
	if !ok || strings.TrimSpace(value) == "" {
		return fmt.Errorf("tool %q requires bounded argument %q", tool.Name, entry.RequiredBounded)
	}
	return nil
}

func registryRequires(tool registry.Tool, name string) bool {
	required, ok := tool.Parameters["required"].([]any)
	if !ok {
		// yaml.v3 commonly decodes []string when the field is loaded directly.
		if names, ok := tool.Parameters["required"].([]string); ok {
			for _, candidate := range names {
				if candidate == name {
					return true
				}
			}
		}
		return false
	}
	for _, candidate := range required {
		if value, ok := candidate.(string); ok && value == name {
			return true
		}
	}
	return false
}

// ValidateRegistryAdapterParity requires every READ registry entry to have one
// and only one adapter implementation in its declared domain.
func ValidateRegistryAdapterParity(tools []registry.Tool, adapters []Adapter) error {
	for _, tool := range tools {
		if tool.Permission != registry.PermRead {
			continue
		}
		count := adapterCount(tool.Name, tool.Domain, adapters)
		if count != 1 {
			return fmt.Errorf("READ tool %q must map to exactly one %q adapter, got %d", tool.Name, tool.Domain, count)
		}
	}
	return nil
}

// ValidateEnabledReadTools requires each enabled READ tool to be manifest-safe
// and implemented by exactly one adapter.
func ValidateEnabledReadTools(tools []registry.Tool, manifest *Manifest, adapters []Adapter) error {
	for _, tool := range tools {
		if tool.Permission != registry.PermRead {
			continue
		}
		if err := manifest.Allows(tool, boundedSchemaArguments(tool)); err != nil {
			return err
		}
		if count := adapterCount(tool.Name, tool.Domain, adapters); count != 1 {
			return fmt.Errorf("enabled READ tool %q must map to exactly one adapter, got %d", tool.Name, count)
		}
	}
	return nil
}

func boundedSchemaArguments(tool registry.Tool) map[string]any {
	args := map[string]any{}
	for _, name := range requiredNames(tool) {
		args[name] = "schema-bound"
	}
	return args
}

func requiredNames(tool registry.Tool) []string {
	var out []string
	if values, ok := tool.Parameters["required"].([]string); ok {
		return append(out, values...)
	}
	if values, ok := tool.Parameters["required"].([]any); ok {
		for _, value := range values {
			if name, ok := value.(string); ok {
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// AdapterImplements reports whether exactly one adapter in the supplied list
// advertises a tool name. It intentionally rejects duplicate ownership.
func AdapterImplements(name string, adapters []Adapter) bool {
	return adapterCount(name, "", adapters) == 1
}

func adapterCount(name, domain string, adapters []Adapter) int {
	count := 0
	for _, adapter := range adapters {
		if domain != "" && adapter.Domain() != domain {
			continue
		}
		for _, advertised := range adapter.ToolNames() {
			if advertised == name {
				count++
			}
		}
	}
	return count
}
