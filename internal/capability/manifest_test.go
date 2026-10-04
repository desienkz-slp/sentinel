package capability_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"
)

type manifestDocument struct {
	Version      string          `yaml:"version"`
	Capabilities []manifestEntry `yaml:"capabilities"`
}

type manifestEntry struct {
	Tool            string `yaml:"tool"`
	Domain          string `yaml:"domain"`
	Transport       string `yaml:"transport"`
	RequiredBounded string `yaml:"required_bounded_argument"`
	Sensitivity     string `yaml:"sensitivity"`
	MaxRows         int    `yaml:"max_rows"`
	MaxBytes        int    `yaml:"max_bytes"`
	SchemaNotes     string `yaml:"schema_notes"`
}

func TestManifestContainsOnlyBoundedNonSensitiveReadCapabilities(t *testing.T) {
	path := filepath.Join("..", "..", "tools", "read_only_external_api_manifest.yaml")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var doc manifestDocument
	if err := yaml.Unmarshal(body, &doc); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if doc.Version == "" || len(doc.Capabilities) == 0 {
		t.Fatalf("manifest must declare a version and at least one safe capability: %+v", doc)
	}
	seen := map[string]bool{}
	for _, entry := range doc.Capabilities {
		if entry.Tool == "" || entry.Domain == "" || entry.Transport == "" || entry.RequiredBounded == "" || entry.SchemaNotes == "" {
			t.Errorf("manifest entry missing required contract fields: %+v", entry)
		}
		if entry.Sensitivity != "none" {
			t.Errorf("sensitive capability %q must not be manifest-enabled", entry.Tool)
		}
		if entry.MaxRows < 1 || entry.MaxBytes < 1 {
			t.Errorf("capability %q must have positive row and byte bounds", entry.Tool)
		}
		if seen[entry.Tool] {
			t.Errorf("capability %q appears more than once", entry.Tool)
		}
		seen[entry.Tool] = true
	}
}
