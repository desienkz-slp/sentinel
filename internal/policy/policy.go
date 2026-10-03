// Package policy mengimplementasikan Policy/Risk Engine dari blueprint.
//
// Prinsip: LLM TIDAK PERNAH mengotorisasi tindakan berbahaya sendiri. Setiap
// tindakan yang diusulkan model harus melewati keputusan deterministik di sini:
// ALLOW / APPROVAL_REQUIRED / DENY.
//
// Urutan evaluasi (sesuai blueprint §23):
//  1. Aturan eksplisit (cocok pertama menang).
//  2. Default per-permission (READ allow, WRITE/ADMIN/UNKNOWN deny).
//  3. Gerbang risiko (risk medium+ -> butuh persetujuan).
//  4. Gerbang mode (COPILOT memaksa semua WRITE/ADMIN menjadi APPROVAL_REQUIRED).
package policy

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// Permission adalah tingkat izin sebuah tool.
type Permission string

const (
	PermRead    Permission = "READ"
	PermWrite   Permission = "WRITE"
	PermAdmin   Permission = "ADMIN"
	PermUnknown Permission = "UNKNOWN"
)

// Risk adalah tingkat risiko sebuah tindakan.
type Risk string

const (
	RiskLow      Risk = "LOW"
	RiskMedium   Risk = "MEDIUM"
	RiskHigh     Risk = "HIGH"
	RiskCritical Risk = "CRITICAL"
)

// Decision adalah hasil evaluasi kebijakan.
type Decision string

const (
	Allow            Decision = "ALLOW"
	ApprovalRequired Decision = "APPROVAL_REQUIRED"
	Deny             Decision = "DENY"
)

// Mode menentukan perilaku otonomi sistem.
type Mode string

const (
	ModeCopilot    Mode = "COPILOT"    // investigasi + rekomendasi, manusia yang menyetujui
	ModeAutonomous Mode = "AUTONOMOUS" // aksi risiko rendah/terpilih otomatis
	ModeReadOnly   Mode = "READ_ONLY"  // tidak ada mutasi sama sekali
)

// Request adalah usulan tindakan yang mau dievaluasi.
type Request struct {
	Tool       string     `json:"tool"`
	Permission Permission `json:"permission"`
	Scope      string     `json:"scope"` // single_customer | multi_customer | ""
	Risk       Risk       `json:"risk"`
	DryRun     bool       `json:"dry_run"`
}

// Result adalah keputusan final beserta alasannya (untuk audit).
type Result struct {
	Decision Decision `json:"decision"`
	RuleID   string   `json:"rule_id,omitempty"`
	Reasons  []string `json:"reasons,omitempty"`
}

// Match adalah kriteria pencocokan satu aturan. Field kosong = wildcard.
type Match struct {
	Tool       string `yaml:"tool" json:"tool"`
	Permission string `yaml:"permission" json:"permission"`
	Scope      string `yaml:"scope" json:"scope"`
	Risk       string `yaml:"risk" json:"risk"`
}

// rule adalah satu aturan dari file kebijakan.
type rule struct {
	ID          string   `yaml:"id" json:"id"`
	Description string   `yaml:"description" json:"description"`
	Match       Match    `yaml:"match" json:"match"`
	Decision    string   `yaml:"decision" json:"decision"`
	Requires    []string `yaml:"requires" json:"requires"`
}

// Rule adalah bentuk publik (diekspos) dari satu aturan kebijakan, dipakai oleh
// API/UI untuk list + edit.
type Rule struct {
	ID          string   `json:"id" yaml:"id"`
	Description string   `json:"description" yaml:"description"`
	Tool        string   `json:"tool,omitempty" yaml:"tool,omitempty"`
	Permission  string   `json:"permission,omitempty" yaml:"permission,omitempty"`
	Scope       string   `json:"scope,omitempty" yaml:"scope,omitempty"`
	Risk        string   `json:"risk,omitempty" yaml:"risk,omitempty"`
	Decision    string   `json:"decision" yaml:"decision"`
	Requires    []string `json:"requires,omitempty" yaml:"requires,omitempty"`
}

func (r Rule) toInternal() rule {
	return rule{
		ID:          r.ID,
		Description: r.Description,
		Match:       Match{Tool: r.Tool, Permission: r.Permission, Scope: r.Scope, Risk: r.Risk},
		Decision:    r.Decision,
		Requires:    r.Requires,
	}
}

func (r rule) toPublic() Rule {
	return Rule{
		ID:          r.ID,
		Description: r.Description,
		Tool:        r.Match.Tool,
		Permission:  r.Match.Permission,
		Scope:       r.Match.Scope,
		Risk:        r.Match.Risk,
		Decision:    r.Decision,
		Requires:    r.Requires,
	}
}

type riskCfg struct {
	MaxScope         int  `yaml:"max_scope"`
	ApprovalRequired bool `yaml:"approval_required"`
}

type defaultCfg struct {
	Read    string `yaml:"read"`
	Write   string `yaml:"write"`
	Admin   string `yaml:"admin"`
	Unknown string `yaml:"unknown"`
}

type doc struct {
	Version string             `yaml:"version"`
	Mode    string             `yaml:"mode"`
	Default defaultCfg         `yaml:"default"`
	Risk    map[string]riskCfg `yaml:"risk"`
	Rules   []rule             `yaml:"rules"`
}

// Engine mengevaluasi tindakan terhadap kebijakan yang dimuat.
type Engine struct {
	mode  Mode
	def   defaultCfg
	risk  map[string]riskCfg
	rules []rule
}

// Load membaca file kebijakan YAML. Bila path kosong atau file tidak ada,
// kembali ke kebijakan bawaan deny-by-default (aman).
func Load(path string) *Engine {
	e := &Engine{
		mode: ModeCopilot,
		def: defaultCfg{
			Read:    "allow",
			Write:   "deny",
			Admin:   "deny",
			Unknown: "deny",
		},
		risk: map[string]riskCfg{
			"LOW":      {MaxScope: 1, ApprovalRequired: false},
			"MEDIUM":   {MaxScope: 1, ApprovalRequired: true},
			"HIGH":     {MaxScope: 1, ApprovalRequired: true},
			"CRITICAL": {MaxScope: 1, ApprovalRequired: true},
		},
	}
	if path == "" {
		return e
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return e // deny-by-default saat file tak terbaca
	}
	var d doc
	if yaml.Unmarshal(b, &d) != nil {
		return e
	}
	if strings.TrimSpace(d.Mode) != "" {
		switch Mode(strings.ToUpper(strings.TrimSpace(d.Mode))) {
		case ModeCopilot, ModeAutonomous, ModeReadOnly:
			e.mode = Mode(strings.ToUpper(strings.TrimSpace(d.Mode)))
		}
	}
	if d.Default.Read != "" {
		e.def.Read = strings.ToLower(d.Default.Read)
	}
	if d.Default.Write != "" {
		e.def.Write = strings.ToLower(d.Default.Write)
	}
	if d.Default.Admin != "" {
		e.def.Admin = strings.ToLower(d.Default.Admin)
	}
	if d.Default.Unknown != "" {
		e.def.Unknown = strings.ToLower(d.Default.Unknown)
	}
	for k, v := range d.Risk {
		e.risk[strings.ToUpper(k)] = v
	}
	e.rules = d.Rules
	return e
}

// Mode mengembalikan mode otonomi aktif.
func (e *Engine) Mode() Mode { return e.mode }

// Rules mengembalikan semua aturan eksplisit (bentuk publik) untuk list/UI.
func (e *Engine) Rules() []Rule {
	out := make([]Rule, 0, len(e.rules))
	for _, r := range e.rules {
		out = append(out, r.toPublic())
	}
	return out
}

func normPermission(p Permission) Permission {
	switch Permission(strings.ToUpper(strings.TrimSpace(string(p)))) {
	case PermRead:
		return PermRead
	case PermWrite:
		return PermWrite
	case PermAdmin:
		return PermAdmin
	}
	return PermUnknown
}

func normRisk(r Risk) Risk {
	switch Risk(strings.ToUpper(strings.TrimSpace(string(r)))) {
	case RiskLow:
		return RiskLow
	case RiskMedium:
		return RiskMedium
	case RiskHigh:
		return RiskHigh
	case RiskCritical:
		return RiskCritical
	}
	return ""
}

func parseDecision(s string) Decision {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "ALLOW":
		return Allow
	case "APPROVAL_REQUIRED", "APPROVAL":
		return ApprovalRequired
	}
	return Deny // termasuk string tak dikenal -> deny (aman)
}

// Decide mengevaluasi satu usulan tindakan.
func (e *Engine) Decide(req Request) Result {
	perm := normPermission(req.Permission)
	risk := normRisk(req.Risk)

	var reasons []string

	// 1. Aturan eksplisit, cocok pertama menang.
	for _, r := range e.rules {
		// Field pencocokan yang kosong = "tidak dibatasi" (wildcard).
		if !matchOptional(r.Match.Tool, req.Tool) {
			continue
		}
		if !matchOptional(r.Match.Permission, string(perm)) {
			continue
		}
		if !matchOptional(r.Match.Scope, req.Scope) {
			continue
		}
		if !matchOptional(r.Match.Risk, string(risk)) {
			continue
		}
		d := parseDecision(r.Decision)
		res := Result{Decision: d, RuleID: r.ID}
		if d == Deny {
			res.Reasons = append(res.Reasons, fmt.Sprintf("aturan %q: %s", r.ID, r.Description))
		}
		if len(r.Requires) > 0 {
			res.Reasons = append(res.Reasons, "butuh: "+strings.Join(r.Requires, ", "))
		}
		return e.applyGates(res, perm, risk, req)
	}

	// 2. Default per-permission.
	fallback := e.def.Unknown
	switch perm {
	case PermRead:
		fallback = e.def.Read
	case PermWrite:
		fallback = e.def.Write
	case PermAdmin:
		fallback = e.def.Admin
	}
	d := parseDecision(fallback)
	if d == Deny {
		reasons = append(reasons, fmt.Sprintf("default kebijakan: %s = deny", perm))
	}
	return e.applyGates(Result{Decision: d, Reasons: reasons}, perm, risk, req)
}

// applyGates menerapkan gerbang risiko dan mode di atas keputusan awal.
func (e *Engine) applyGates(res Result, perm Permission, risk Risk, req Request) Result {
	if res.Decision != Deny {
		if cfg, ok := e.risk[string(risk)]; ok && cfg.MaxScope > 0 && scopeSize(req.Scope) > cfg.MaxScope {
			res.Decision = Deny
			res.Reasons = append(res.Reasons, fmt.Sprintf("risiko %s melampaui scope yang diizinkan", risk))
			return res
		}
	}
	// 3. Gerbang risiko: medium+ butuh persetujuan walau aturan bilang allow.
	if res.Decision == Allow {
		if rc, ok := e.risk[string(risk)]; ok && rc.ApprovalRequired {
			res.Decision = ApprovalRequired
			res.Reasons = append(res.Reasons, fmt.Sprintf("risiko %s membutuhkan persetujuan", risk))
		}
	}
	// 4. Gerbang mode.
	switch e.mode {
	case ModeReadOnly:
		if perm != PermRead {
			res.Decision = Deny
			res.Reasons = append(res.Reasons, "mode READ_ONLY: mutasi ditolak")
		}
	case ModeCopilot:
		if res.Decision == Allow && perm != PermRead {
			res.Decision = ApprovalRequired
			res.Reasons = append(res.Reasons, "mode COPILOT: mutasi butuh persetujuan operator")
		}
	}
	// dry-run tidak pernah diizinkan menjadi mutasi nyata.
	if req.DryRun && perm != PermRead && res.Decision != Deny {
		res.Reasons = append(res.Reasons, "dry-run: tidak ada perubahan produksi")
	}
	return res
}

func scopeSize(scope string) int {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "", "single_customer":
		return 1
	case "multi_customer":
		return 2
	default:
		return 2
	}
}

func matchOptional(pattern, value string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return true // field tidak dibatasi
	}
	return strings.EqualFold(pattern, strings.TrimSpace(value))
}
