// Package discovery memvalidasi artefak hasil inspeksi dan baseline Phase 0.
package discovery

import (
	"os"
	"path/filepath"
	"strings"
)

type Problem struct {
	Path    string
	Message string
}

// Artifact mendefinisikan dokumen discovery beserta bagian minimum yang wajib ada.
type Artifact struct {
	Path     string
	Required []string
}

// RequiredArtifacts membuat exit-gate discovery eksplisit dan dapat diuji ulang.
var RequiredArtifacts = []Artifact{
	{
		Path: "docs/discovery/ARCHITECTURE_MAP.md",
		Required: []string{
			"# Peta Arsitektur", "## Diagram", "## Komponen", "## Alur Runtime",
		},
	},
	{
		Path: "docs/discovery/API_CAPABILITY_INVENTORY.md",
		Required: []string{
			"# Inventaris API dan Kapabilitas", "## NOC Sentinel", "## WhatsApp",
			"## Billing", "## RADIUS", "## GenieACS", "## MikroTik", "## Hermes", "## n8n",
		},
	},
	{
		Path: "docs/discovery/GAP_ANALYSIS.md",
		Required: []string{
			"# Analisis Gap", "## Ringkasan", "## Gap", "## Asumsi", "## Risiko",
		},
	},
}

// Validate memeriksa baseline kode yang sudah ada serta kelengkapan dokumen discovery.
func Validate(root string) []Problem {
	required := []string{
		"internal/security/security.go", "internal/security/security_test.go",
		"internal/diag/diag.go", "internal/diag/diag_test.go",
		"server.go", "server_security_test.go",
	}
	var out []Problem
	for _, rel := range required {
		path := filepath.Join(root, rel)
		if _, err := os.Stat(path); err != nil {
			out = append(out, Problem{Path: rel, Message: "artefak baseline Phase 0 tidak ditemukan"})
		}
	}
	for _, artifact := range RequiredArtifacts {
		path := filepath.Join(root, filepath.FromSlash(artifact.Path))
		raw, err := os.ReadFile(path)
		if err != nil {
			out = append(out, Problem{Path: artifact.Path, Message: "dokumen discovery tidak dapat dibaca: " + err.Error()})
			continue
		}
		content := string(raw)
		for _, required := range artifact.Required {
			if !strings.Contains(content, required) {
				out = append(out, Problem{Path: artifact.Path, Message: "bagian wajib tidak ditemukan: " + required})
			}
		}
	}
	return out
}
