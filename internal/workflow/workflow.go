// Package workflow mengimplementasikan Workflow Engine deterministik dari blueprint.
//
// Pemisahan penting (blueprint §13): SKILL menjelaskan APA yang diketahui,
// WORKFLOW menjelaskan BAGAIMANA langkah dieksekusi. AI memilih workflow mana
// yang dipanggil; kode deterministik yang mengontrol urutan langkah dan bukti
// yang diwajibkan — AI TIDAK mengimprovisasi tiap langkah operasional.
//
// Workflow yang tersedia dibaca dari file YAML (workflows/*.yaml). Setiap langkah
// bisa mensyaratkan tool tertentu yang harus lolos registry + policy dulu.
package workflow

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Step adalah satu langkah workflow.
type Step struct {
	Index            int      `yaml:"index" json:"index"`
	Name             string   `yaml:"name" json:"name"`
	Tool             string   `yaml:"tool,omitempty" json:"tool,omitempty"`
	Permission       string   `yaml:"permission,omitempty" json:"permission,omitempty"`
	RiskLevel        string   `yaml:"risk_level,omitempty" json:"risk_level,omitempty"`
	RequiredEvidence []string `yaml:"required_evidence,omitempty" json:"required_evidence,omitempty"`
	AllowedTools     []string `yaml:"allowed_tools,omitempty" json:"allowed_tools,omitempty"`
	TimeoutMS        int      `yaml:"timeout_ms,omitempty" json:"timeout_ms,omitempty"`
	Description      string   `yaml:"description,omitempty" json:"description,omitempty"`
}

// Definition adalah satu workflow.
type Definition struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Version     string `yaml:"version" json:"version"`
	Trigger     string `yaml:"trigger" json:"trigger"`
	Mode        string `yaml:"mode,omitempty" json:"mode,omitempty"`
	Steps       []Step `yaml:"steps" json:"steps"`
}

type workflowDoc struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Version     string `yaml:"version"`
	Trigger     string `yaml:"trigger"`
	Mode        string `yaml:"mode"`
	Steps       []Step `yaml:"steps"`
}

// Registry menyimpan workflow yang dimuat.
type Registry struct {
	byName    map[string]Definition
	byTrigger map[string]Definition
}

// Load membaca semua workflow dari sebuah file YAML.
func Load(path string) (*Registry, error) {
	r := &Registry{byName: map[string]Definition{}, byTrigger: map[string]Definition{}}
	if path == "" {
		return r, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil // file opsional
		}
		return nil, fmt.Errorf("baca workflow: %w", err)
	}
	var d workflowDoc
	if err := yaml.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("parse workflow: %w", err)
	}
	if strings.TrimSpace(d.Name) == "" {
		return nil, fmt.Errorf("workflow tanpa name")
	}
	def := Definition{
		Name:        d.Name,
		Description: d.Description,
		Version:     d.Version,
		Trigger:     strings.ToUpper(strings.TrimSpace(d.Trigger)),
		Mode:        d.Mode,
		Steps:       d.Steps,
	}
	r.byName[d.Name] = def
	if def.Trigger != "" {
		r.byTrigger[def.Trigger] = def
	}
	return r, nil
}

// LoadDir memuat semua file .yaml/.yml dalam sebuah direktori.
func LoadDir(dir string) (*Registry, error) {
	r := &Registry{byName: map[string]Definition{}, byTrigger: map[string]Definition{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		sub, err := Load(dir + string(os.PathSeparator) + name)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		for n, d := range sub.byName {
			r.byName[n] = d
		}
		for tr, d := range sub.byTrigger {
			r.byTrigger[tr] = d
		}
	}
	return r, nil
}

// Get mengembalikan workflow berdasarkan nama.
func (r *Registry) Get(name string) (Definition, bool) {
	d, ok := r.byName[name]
	return d, ok
}

// ForTrigger mengembalikan workflow untuk intent tertentu.
func (r *Registry) ForTrigger(intent string) (Definition, bool) {
	d, ok := r.byTrigger[strings.ToUpper(strings.TrimSpace(intent))]
	return d, ok
}

// Names mengembalikan daftar nama workflow (untuk dashboard).
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.byName))
	for n := range r.byName {
		out = append(out, n)
	}
	return out
}

// Len mengembalikan jumlah workflow terdaftar.
func (r *Registry) Len() int { return len(r.byName) }
