// Package reasoning menyusun diagnosis deterministik dari input case dan bukti
// read-only. Package ini tidak memanggil adaptor eksternal secara langsung;
// pengumpul bukti harus telah melewati registry, policy, timeout, dan audit.
package reasoning

import (
	"context"
	"sort"
	"strings"
	"time"

	"ainoc/internal/correlation"
)

const SchemaVersion = "reasoning.v1"

var requiredDomains = []string{"billing", "radius", "mikrotik", "genieacs"}

var allowedIntents = map[string]struct{}{
	"slow_connection": {},
	"internet_down":   {},
	"intermittent":    {},
	"wifi_issue":      {},
}

// CaseInput adalah konteks minimum yang boleh dipakai reasoning. Facts berasal
// dari conversation dan tidak dianggap sebagai fakta live jaringan.
type CaseInput struct {
	CaseID   string            `json:"case_id"`
	Intent   string            `json:"intent"`
	Identity string            `json:"identity,omitempty"`
	Facts    map[string]string `json:"facts,omitempty"`
}

// Evidence adalah bukti typed dari satu domain. Nilai State UNKNOWN berarti
// data belum tersedia, gagal diperoleh, atau tidak bisa dinormalisasi.
type Evidence struct {
	Domain      string            `json:"domain"`
	State       correlation.State `json:"state"`
	Source      string            `json:"source,omitempty"`
	CollectedAt time.Time         `json:"collected_at,omitempty"`
	Error       string            `json:"error,omitempty"`
}

// Collector mengabstraksikan pengumpulan bukti. Implementasi production wajib
// read-only dan menjalankan tool lewat dispatcher yang sudah ada.
type Collector interface {
	Collect(ctx context.Context, input CaseInput, domain string) Evidence
}

type HypothesisStatus string

const (
	HypothesisSupported    HypothesisStatus = "SUPPORTED"
	HypothesisInsufficient HypothesisStatus = "INSUFFICIENT_EVIDENCE"
)

// Hypothesis menyimpan hasil uji yang dapat diaudit tanpa hidden reasoning.
type Hypothesis struct {
	Statement string           `json:"statement"`
	Area      string           `json:"area,omitempty"`
	Status    HypothesisStatus `json:"status"`
	Evidence  []string         `json:"evidence_domains"`
}

type DiagnosticStatus string

const (
	DiagnosticComplete DiagnosticStatus = "COMPLETE"
	DiagnosticUnknown  DiagnosticStatus = "UNKNOWN"
)

// Diagnostic mencatat tiap pemeriksaan yang dibutuhkan dan status buktinya.
type Diagnostic struct {
	Domain string           `json:"domain"`
	Status DiagnosticStatus `json:"status"`
	Source string           `json:"source,omitempty"`
	Error  string           `json:"error,omitempty"`
}

// Output adalah schema stabil untuk audit, handoff manusia, dan API internal.
type Output struct {
	SchemaVersion    string                 `json:"schema_version"`
	Input            CaseInput              `json:"input"`
	InputValid       bool                   `json:"input_valid"`
	ValidationErrors []string               `json:"validation_errors,omitempty"`
	Evidence         []Evidence             `json:"evidence"`
	Diagnostics      []Diagnostic           `json:"diagnostics"`
	Hypotheses       []Hypothesis           `json:"hypotheses"`
	Conclusion       correlation.Conclusion `json:"conclusion"`
	GeneratedAt      time.Time              `json:"generated_at"`
}

// EvidenceFor mengambil bukti domain. Bila domain tidak ada, kembalikan UNKNOWN
// secara eksplisit agar pemanggil tidak menganggap ketiadaan sebagai keadaan live.
func (o Output) EvidenceFor(domain string) Evidence {
	for _, evidence := range o.Evidence {
		if evidence.Domain == domain {
			return evidence
		}
	}
	return unknownEvidence(domain, "bukti belum tersedia")
}

// Engine menjalankan pengumpulan, diagnostic record, hypothesis, dan conclusion.
type Engine struct {
	collector Collector
	now       func() time.Time
}

func New(collector Collector) *Engine {
	return &Engine{collector: collector, now: func() time.Time { return time.Now().UTC() }}
}

// Evaluate selalu menghasilkan output berstruktur. Input atau bukti yang tidak
// tersedia dipertahankan sebagai UNKNOWN; tidak ada fallback yang menebak state.
func (e *Engine) Evaluate(ctx context.Context, input CaseInput) Output {
	if e == nil {
		e = New(nil)
	}
	output := Output{SchemaVersion: SchemaVersion, Input: copyInput(input), GeneratedAt: e.now()}
	output.ValidationErrors = validate(input)
	output.InputValid = len(output.ValidationErrors) == 0

	set := correlation.NewSet()
	for _, domain := range requiredDomains {
		evidence := unknownEvidence(domain, "bukti belum tersedia")
		if output.InputValid && e.collector != nil && ctx.Err() == nil {
			evidence = normalizeEvidence(domain, e.collector.Collect(ctx, input, domain))
		} else if ctx.Err() != nil {
			evidence.Error = "pengumpulan dibatalkan: " + ctx.Err().Error()
		}
		output.Evidence = append(output.Evidence, evidence)
		set.Add(domain, rawForState(evidence.State))
		output.Diagnostics = append(output.Diagnostics, diagnosticFor(evidence))
	}

	output.Conclusion = set.Correlate(requiredDomains...)
	output.Hypotheses = hypothesesFor(output.Conclusion, output.Evidence)
	return output
}

func validate(input CaseInput) []string {
	var errors []string
	if strings.TrimSpace(input.CaseID) == "" {
		errors = append(errors, "case_id wajib diisi")
	}
	intent := strings.TrimSpace(strings.ToLower(input.Intent))
	if intent == "" {
		errors = append(errors, "intent wajib diisi")
	} else if _, ok := allowedIntents[intent]; !ok {
		errors = append(errors, "intent tidak didukung untuk diagnosis")
	}
	return errors
}

func copyInput(input CaseInput) CaseInput {
	out := input
	if input.Facts != nil {
		out.Facts = make(map[string]string, len(input.Facts))
		for key, value := range input.Facts {
			out.Facts[key] = value
		}
	}
	return out
}

func normalizeEvidence(domain string, evidence Evidence) Evidence {
	evidence.Domain = domain
	if !validState(domain, evidence.State) {
		evidence.State = correlation.StateUnknown
		if strings.TrimSpace(evidence.Error) == "" {
			evidence.Error = "state bukti tidak sah untuk domain"
		}
	}
	return evidence
}

func validState(domain string, state correlation.State) bool {
	if state == correlation.StateUnknown {
		return true
	}
	switch domain {
	case "billing":
		return state == correlation.StateActive || state == correlation.StateInactive
	case "radius":
		return state == correlation.StateAuthed || state == correlation.StateUnauthed
	case "mikrotik":
		return state == correlation.StatePPPoEUp || state == correlation.StatePPPoEDown
	case "genieacs":
		return state == correlation.StateDeviceOn || state == correlation.StateDeviceOff
	default:
		return false
	}
}

func unknownEvidence(domain, reason string) Evidence {
	return Evidence{Domain: domain, State: correlation.StateUnknown, Error: reason}
}

func diagnosticFor(evidence Evidence) Diagnostic {
	status := DiagnosticComplete
	if evidence.State == correlation.StateUnknown {
		status = DiagnosticUnknown
	}
	return Diagnostic{Domain: evidence.Domain, Status: status, Source: evidence.Source, Error: evidence.Error}
}

func hypothesesFor(conclusion correlation.Conclusion, evidence []Evidence) []Hypothesis {
	domains := make([]string, 0, len(evidence))
	for _, item := range evidence {
		if item.State != correlation.StateUnknown {
			domains = append(domains, item.Domain)
		}
	}
	sort.Strings(domains)
	if conclusion.Diagnosis == "bukti tidak cukup untuk diagnosis pasti" {
		return []Hypothesis{{Statement: conclusion.Diagnosis, Status: HypothesisInsufficient, Evidence: domains}}
	}
	return []Hypothesis{{Statement: conclusion.Diagnosis, Area: conclusion.PrimaryArea, Status: HypothesisSupported, Evidence: domains}}
}

// rawForState menjembatani status kanonik ke korelator existing tanpa menambah
// informasi baru. UNKNOWN tetap dipetakan menjadi nilai yang tidak dikenali.
func rawForState(state correlation.State) string {
	switch state {
	case correlation.StateActive:
		return "ACTIVE"
	case correlation.StateInactive:
		return "INACTIVE"
	case correlation.StateAuthed:
		return "AUTH_OK"
	case correlation.StateUnauthed:
		return "AUTH_FAILED"
	case correlation.StatePPPoEUp:
		return "ACTIVE"
	case correlation.StatePPPoEDown:
		return "OFFLINE"
	case correlation.StateDeviceOn:
		return "ONLINE"
	case correlation.StateDeviceOff:
		return "OFFLINE"
	default:
		return "UNKNOWN"
	}
}
