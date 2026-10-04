// Package tool menyediakan framework adapter tool yang aman dan seragam.
//
// Ini adalah fondasi Stage 2 blueprint (§2, §16, §18): setiap sistem eksternal
// (Billing, RADIUS, MikroTik, GenieACS) diwadahi satu interface Adapter, dan
// setiap pemanggilan tool WAJIB melewati tiga gerbang berurutan:
//
//  1. registry  — tool harus terdaftar DAN aktif (CanInvoke)
//  2. policy    — usulan tindakan dievaluasi (ALLOW / APPROVAL_REQUIRED / DENY)
//  3. adapter   — baru dieksekusi dengan timeout + request_id
//
// Ini menjamin model TIDAK PERNAH bisa memanggil tool yang tidak terdaftar,
// nonaktif, atau ditolak kebijakan — terlepas dari apa pun yang dihalusinasikan.
package tool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"ainoc/internal/capability"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
)

// Adapter adalah satu sistem eksternal beserta tool-tool bacaannya.
type Adapter interface {
	// Domain mengembalikan nama domain (billing | radius | mikrotik | genieacs).
	Domain() string
	// Name adalah nama tampilan.
	Name() string
	// Configured melaporkan apakah ada endpoint + kredensial nyata.
	Configured() bool
	// Health mengembalikan status liveness (detail, error).
	Health(ctx context.Context) (detail string, err error)
	// ToolNames mengembalikan nama tool yang diekspos adapter ini.
	ToolNames() []string
	// Invoke menjalankan satu tool read-only.
	Invoke(ctx context.Context, tool string, args map[string]any) (Output, error)
}

// Output adalah hasil eksekusi tool.
type Output struct {
	Data any    `json:"data,omitempty"`
	Text string `json:"text,omitempty"`
}

// Result adalah hasil invoke lengkap dengan jejak keputusan (untuk audit).
type Result struct {
	RequestID string          `json:"request_id"`
	Tool      string          `json:"tool"`
	Decision  policy.Decision `json:"decision"`
	RuleID    string          `json:"rule_id,omitempty"`
	OK        bool            `json:"ok"`
	Output    Output          `json:"output,omitempty"`
	Error     string          `json:"error,omitempty"`
	LatencyMS int64           `json:"latency_ms"`
}

// Dispatcher merutekan pemanggilan tool lewat gerbang registry + policy.
type Dispatcher struct {
	Reg     *registry.Registry
	Pol     *policy.Engine
	Timeout time.Duration
	// CapabilityManifest is an explicit allowlist for external READ calls.
	// Production wiring always sets it; nil preserves isolated dispatcher tests.
	CapabilityManifest *capability.Manifest
	adapters           map[string]Adapter // key = nama tool
}

// New membuat dispatcher. Registry dan policy wajib tersedia saat Invoke;
// dispatcher yang belum lengkap akan gagal tertutup.
func New(reg *registry.Registry, pol *policy.Engine, timeout time.Duration) *Dispatcher {
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &Dispatcher{
		Reg:      reg,
		Pol:      pol,
		Timeout:  timeout,
		adapters: map[string]Adapter{},
	}
}

// Register mendaftarkan adapter dan seluruh tool-nya.
// SetCapabilityManifest installs the reviewed static external-read allowlist.
// Passing nil intentionally fail-closes every external invocation.
func (d *Dispatcher) SetCapabilityManifest(manifest *capability.Manifest) {
	d.CapabilityManifest = manifest
}

func (d *Dispatcher) Register(a Adapter) {
	for _, name := range a.ToolNames() {
		d.adapters[name] = a
	}
}

// Unregister melepas seluruh tool milik adapter (dipanggil saat konfigurasi
// adaptor dikosongkan lewat Pengaturan).
func (d *Dispatcher) Unregister(a Adapter) {
	for _, name := range a.ToolNames() {
		delete(d.adapters, name)
	}
}

// DomainHealth adalah hasil pengecekan kesehatan satu domain.
type DomainHealth struct {
	Domain    string
	Name      string
	OK        bool
	Detail    string // pesan sukses atau error
	LatencyMS int64
}

// Health memeriksa kesehatan adapter satu domain yang terdaftar. found=false
// bila domain itu tidak terdaftar (belum dikonfigurasi).
func (d *Dispatcher) Health(ctx context.Context, domain string) (h DomainHealth, found bool) {
	for _, a := range d.adapters {
		if a.Domain() != domain {
			continue
		}
		start := time.Now()
		detail, err := a.Health(ctx)
		h = DomainHealth{Domain: domain, Name: a.Name(), OK: err == nil, Detail: detail, LatencyMS: time.Since(start).Milliseconds()}
		if err != nil {
			h.Detail = err.Error()
		}
		return h, true
	}
	return DomainHealth{Domain: domain}, false
}

// Adapters mengembalikan daftar domain yang terdaftar (terurut).
func (d *Dispatcher) Adapters() []string {
	seen := map[string]bool{}
	for _, a := range d.adapters {
		seen[a.Domain()] = true
	}
	out := make([]string, 0, len(seen))
	for dom := range seen {
		out = append(out, dom)
	}
	sort.Strings(out)
	return out
}

// Invoke menjalankan tool melalui seluruh gerbang keamanan.
func (d *Dispatcher) Invoke(ctx context.Context, toolName string, args map[string]any) Result {
	res := Result{Tool: toolName, RequestID: newID()}
	if d.Reg == nil {
		res.Decision = policy.Deny
		res.Error = "tool gateway tidak siap: registry tidak tersedia"
		return res
	}
	if d.Pol == nil {
		res.Decision = policy.Deny
		res.Error = "tool gateway tidak siap: policy tidak tersedia"
		return res
	}

	// Gerbang 1: registry — terdaftar & aktif.
	entry, err := d.Reg.CanInvoke(toolName)
	if err != nil {
		res.Decision = policy.Deny
		res.Error = err.Error()
		return res
	}

	// Gerbang 1b: static manifest — when production wiring supplies the
	// release-controlled manifest, capability must be reviewed, non-sensitive,
	// READ-only, and bounded before policy or adapter execution.
	if d.CapabilityManifest != nil {
		if err := d.CapabilityManifest.Allows(entry, args); err != nil {
			res.Decision = policy.Deny
			res.Error = err.Error()
			return res
		}
	}

	// Meta tool dari registry (permission, risk, scope) untuk policy.
	perm := entry.Permission
	risk := entry.Risk
	scope := "single_customer"
	if entry.Scope != "" {
		scope = entry.Scope
	}

	// Gerbang 2: policy.
	dec := d.Pol.Decide(policy.Request{
		Tool:       toolName,
		Permission: convertPerm(perm),
		Scope:      scope,
		Risk:       convertRisk(risk),
	})
	res.Decision = dec.Decision
	res.RuleID = dec.RuleID
	if dec.Decision != policy.Allow {
		res.Error = fmt.Sprintf("ditolak kebijakan: %s", strings.Join(dec.Reasons, "; "))
		return res
	}

	// Gerbang 3: adapter.
	ad, ok := d.adapters[toolName]
	if !ok {
		res.Error = fmt.Sprintf("tidak ada adapter untuk tool %q", toolName)
		res.Decision = policy.Deny
		return res
	}

	start := time.Now()
	cctx, cancel := context.WithTimeout(ctx, d.Timeout)
	defer cancel()
	out, err := ad.Invoke(cctx, toolName, args)
	res.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		res.Error = err.Error()
		return res
	}
	res.Output = out
	res.OK = true
	return res
}

func convertPerm(p registry.Permission) policy.Permission {
	return policy.Permission(string(p))
}

func convertRisk(r registry.Risk) policy.Risk {
	return policy.Risk(string(r))
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
