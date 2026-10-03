// Package registry mengimplementasikan Tool Discovery & Registry dari blueprint.
//
// Tujuan: model HANYA boleh melihat tool yang terdaftar, aktif, dan berizin.
// Tool yang enabled:false TIDAK boleh dipresentasikan ke model maupun dijalankan
// oleh workflow — ini gerbang keamanan sebelum adaptor API eksternal ada.
//
// Semua tool WRITE (Billing/RADIUS/MikroTik/GenieACS) dideklarasikan tapi
// disabled:false sampai endpoint + kredensial nyata diberikan oleh operator.
package registry

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"ainoc/internal/llm"

	"gopkg.in/yaml.v3"
)

// Permission & Risk menyalin enum paket policy supaya registry mandiri
// (menghindari import siklik bila policy nanti memakai registry).
type Permission string

const (
	PermRead    Permission = "READ"
	PermWrite   Permission = "WRITE"
	PermAdmin   Permission = "ADMIN"
	PermUnknown Permission = "UNKNOWN"
)

type Risk string

const (
	RiskLow    Risk = "LOW"
	RiskMedium Risk = "MEDIUM"
	RiskHigh   Risk = "HIGH"
)

// Tool adalah satu entri katalog.
type Tool struct {
	Name           string     `yaml:"name" json:"name"`
	Domain         string     `yaml:"domain" json:"domain"`
	Version        string     `yaml:"version" json:"version"`
	Enabled        bool       `yaml:"enabled" json:"enabled"`
	Permission     Permission `yaml:"permission" json:"permission"`
	Risk           Risk       `yaml:"risk" json:"risk"`
	Scope          string     `yaml:"scope" json:"scope,omitempty"`
	SourceOfTruth  string     `yaml:"source_of_truth" json:"source_of_truth,omitempty"`
	TimeoutSeconds int        `yaml:"timeout_seconds" json:"timeout_seconds,omitempty"`
	Retry          string     `yaml:"retry" json:"retry,omitempty"`
	Idempotency    string     `yaml:"idempotency" json:"idempotency,omitempty"`
	Approval       string     `yaml:"approval" json:"approval,omitempty"`
	Verification   string     `yaml:"verification" json:"verification,omitempty"`
	Rollback       string     `yaml:"rollback" json:"rollback,omitempty"`
	Description    string     `yaml:"description" json:"description,omitempty"`
	// Parameters adalah skema argumen (JSON Schema) untuk function-calling LLM.
	// Contoh: {type: object, properties: {identity: {type: string}}, required: [identity]}.
	Parameters map[string]any `yaml:"parameters" json:"parameters,omitempty"`
}

type registryDoc struct {
	Version string `yaml:"version"`
	Tools   []Tool `yaml:"tools"`
}

// Registry adalah katalog tool yang dimuat dari file.
type Registry struct {
	tools map[string]Tool
	order []string
}

// Load membaca registry YAML. File hilang/tak terbaca -> registry kosong
// (tidak ada tool yang diekspos; aman).
func Load(path string) *Registry {
	r := &Registry{tools: map[string]Tool{}}
	if path == "" {
		return r
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	var d registryDoc
	if yaml.Unmarshal(b, &d) != nil {
		return r
	}
	for _, t := range d.Tools {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		r.tools[name] = t
		r.order = append(r.order, name)
	}
	sort.Strings(r.order)
	return r
}

// LoadMerged memuat registry dasar (yang ikut rilis) lalu menerapkan overlay
// lokal di atasnya. Aturan penggabungan:
//
//   - Definisi tool (parameter, deskripsi, izin, risiko, dll.) SELALU dari dasar,
//     jadi tool baru di rilis otomatis tersedia tanpa edit manual di server.
//   - Overlay hanya boleh mengubah `enabled` pada tool yang SUDAH ada di dasar.
//   - Tool yang hanya ada di overlay (overlay lama yang lengkap, atau tool khusus
//     operator) tetap dimuat apa adanya — kompatibel mundur.
//   - Tool BARU di dasar yang tidak disebut overlay tetap NONAKTIF (deny-by-default):
//     rilis tidak pernah mengaktifkan tool sendiri.
//   - Tool tulis (permission != READ) TIDAK pernah diaktifkan lewat penggabungan
//     bila dasar menandainya nonaktif, kecuali overlay secara eksplisit
//     menyebutnya — dan itu tetap melalui gerbang kebijakan.
//
// overlay kosong/hilang/rusak -> hasilnya identik dengan Load(base).
func LoadMerged(base, overlay string) *Registry {
	r := Load(base)
	if overlay == "" || overlay == base {
		return r
	}
	b, err := os.ReadFile(overlay)
	if err != nil {
		return r
	}
	var d registryDoc
	if yaml.Unmarshal(b, &d) != nil {
		return r
	}
	for _, o := range d.Tools {
		name := strings.TrimSpace(o.Name)
		if name == "" {
			continue
		}
		if cur, ok := r.tools[name]; ok {
			cur.Enabled = o.Enabled // hanya enabled yang diambil dari overlay
			r.tools[name] = cur
			continue
		}
		r.tools[name] = o // hanya ada di overlay: pertahankan
		r.order = append(r.order, name)
	}
	sort.Strings(r.order)
	return r
}

// Get mengembalikan tool berdasarkan nama (found=false bila tidak ada).
func (r *Registry) Get(name string) (Tool, bool) {
	t, ok := r.tools[name]
	return t, ok
}

// Enabled mengembalikan tool yang aktif saja (untuk dipresentasikan ke model).
func (r *Registry) Enabled() []Tool {
	var out []Tool
	for _, name := range r.order {
		if t := r.tools[name]; t.Enabled {
			out = append(out, t)
		}
	}
	return out
}

// All mengembalikan seluruh tool terdaftar (aktif + nonaktif), untuk dashboard.
func (r *Registry) All() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// CanInvoke melaporkan apakah tool boleh dipanggil: harus terdaftar DAN aktif.
func (r *Registry) CanInvoke(name string) (Tool, error) {
	t, ok := r.tools[name]
	if !ok {
		return Tool{}, fmt.Errorf("tool %q tidak terdaftar", name)
	}
	if !t.Enabled {
		return Tool{}, fmt.Errorf("tool %q nonaktif (belum diaktifkan)", name)
	}
	return t, nil
}

// Count mengembalikan (total, aktif).
func (r *Registry) Count() (int, int) {
	active := 0
	for _, t := range r.tools {
		if t.Enabled {
			active++
		}
	}
	return len(r.tools), active
}

// LLMTool mengembalikan tool dalam format function-calling LLM (llm.Tool).
// Hanya tool yang Enabled yang boleh dipresentasikan ke model.
func (r *Registry) LLMTools() []llm.Tool {
	out := make([]llm.Tool, 0)
	for _, name := range r.order {
		t := r.tools[name]
		if !t.Enabled {
			continue
		}
		params := t.Parameters
		if params == nil {
			params = map[string]any{"type": "object", "properties": map[string]any{}}
		}
		out = append(out, llm.Tool{
			Type: "function",
			Function: llm.Function{
				Name:        t.Name,
				Description: t.Description,
				Parameters:  params,
			},
		})
	}
	return out
}
