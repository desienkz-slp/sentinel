// verifywire.go — FASE 4: Verification Engine.
//
// Master spec §13 + §53 (absolute rule 7): API success BUKAN berarti resolved.
// Setelah sebuah aksi yang mengubah state dieksekusi, hasilnya WAJIB
// diverifikasi ulang terhadap sistem yang terkena aksi SEBELUM case boleh
// RESOLVED. Alur yang ditegakkan di kode (bukan prompt):
//
//	EXECUTING → VERIFYING → RecordVerification(Passed=true) → RESOLVED
//	EXECUTING → VERIFYING → RecordVerification(Passed=false) → FAILED → ESCALATION
//
// Gerbang ini hanya dipakai untuk aksi WRITE (yang benar-benar mengubah
// state). Pembacaan diagnostik LOW read-only tidak mengubah state sehingga
// tidak membutuhkan loop verifikasi — bukti probe yang sudah terkumpul sudah
// cukup (rekomendasi fase 4). Bila aksi WRITE tidak punya rencana verifikasi
// (field `verification` kosong / bukan tool), gerbang ini fail-closed: aksi
// TIDAK PERNAH boleh dianggap selesai.
package agent

import (
	"context"
	"strings"

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/correlation"
	"ainoc/internal/registry"
	"ainoc/internal/tool"
)

// Executor menjalankan satu aksi yang sudah diizinkan. Produksi memakai
// dispatcher; pengujian menyuntikkan executor fake agar verifikasi teruji
// deterministik tanpa menyentuh adapter nyata.
type Executor func(ctx context.Context, name string, args map[string]any) tool.Result

// Verifier mengubah hasil eksekusi aksi menjadi bukti verifikasi. Nil yang
// diteruskan ke NewVerificationGate memakai verifier bawaan berbasis registry
// (query ulang sistem via tool `verification` yang dideklarasikan).
type Verifier func(ctx context.Context, name string, args map[string]any, rres tool.Result) caseengine.Verification

// VerificationGate adalah satu-satunya jalur yang boleh menutup sebuah aksi
// WRITE menjadi RESOLVED. Ia memegang CaseWire (state machine), registry
// (rencana verifikasi per tool), dispatcher (query ulang), dan audit.
type VerificationGate struct {
	casew    *CaseWire
	reg      *registry.Registry
	disp     *tool.Dispatcher
	aud      *audit.Store
	verifier Verifier
}

// NewVerificationGate membangun gerbang. verifier nil = verifier bawaan.
func NewVerificationGate(casew *CaseWire, reg *registry.Registry, disp *tool.Dispatcher, aud *audit.Store, verifier Verifier) *VerificationGate {
	return &VerificationGate{casew: casew, reg: reg, disp: disp, aud: aud, verifier: verifier}
}

// RunAction menjalankan aksi yang SUDAH lolos policy gate (ALLOW) lalu
// memverifikasi hasilnya dan menutup loop case:
//
//	onActionStarted  → EXECUTING
//	execute          → jalankan aksi (executor / dispatcher)
//	verify           → query ulang sistem terkena aksi
//	onActionCompleted→ VERIFYING → RecordVerification → RESOLVED | FAILED→ESCALATION
//
// Mengembalikan hasil eksekusi + snapshot case setelah loop selesai. Bila
// gate nil atau dispatcher tidak tersedia, aksi tidak dijalankan dan case
// jatuh ke jalur verifikasi-gagal (fail-closed, tidak pernah RESOLVED).
func (v *VerificationGate) RunAction(ctx context.Context, identity, caseID, name string, args map[string]any, execute Executor) (tool.Result, CaseSnapshot) {
	rres := tool.Result{Tool: name, Error: "verification gate tidak tersedia"}
	snap := CaseSnapshot{CaseID: strings.TrimSpace(caseID)}
	if v == nil {
		return rres, snap
	}

	// 1. EXECUTING — jejak mulai aksi (ACTION_PROPOSED → POLICY_CHECK → EXECUTING).
	if v.casew != nil {
		snap = v.casew.onActionStarted(identity, name)
		if snap.CaseID != "" {
			caseID = snap.CaseID
		}
	}

	// 2. Eksekusi aksi.
	if execute == nil {
		if v.disp != nil {
			execute = v.disp.Invoke
		} else {
			rres.Error = "dispatcher tidak tersedia — aksi tidak dijalankan"
			return v.finish(ctx, identity, caseID, name, args, rres)
		}
	}
	rres = execute(ctx, name, args)

	// 3+4. Verifikasi + tutup loop (RESOLVED / FAILED→ESCALATION).
	return v.finish(ctx, identity, caseID, name, args, rres)
}

// finish menjalankan verifikasi dan menutup loop case + audit.
func (v *VerificationGate) finish(ctx context.Context, identity, caseID, name string, args map[string]any, rres tool.Result) (tool.Result, CaseSnapshot) {
	verification := v.verify(ctx, name, args, rres)

	snap := CaseSnapshot{CaseID: strings.TrimSpace(caseID)}
	if v.casew != nil {
		snap = v.casew.onActionCompleted(identity, name, verification)
		if snap.CaseID != "" {
			caseID = snap.CaseID
		}
	}
	if v.aud != nil {
		status := "PASSED"
		if !verification.Passed {
			status = "FAILED"
		}
		v.aud.Record(audit.Entry{
			EventType:       "verification",
			Actor:           "verification_engine",
			CaseID:          strings.TrimSpace(caseID),
			Tool:            name,
			ExecutionStatus: status,
			Verification:    verification.Source + ": " + verification.Summary,
			After: map[string]any{
				"passed":       verification.Passed,
				"source":       verification.Source,
				"case_state":   snap.CaseState,
				"action_ok":    rres.OK,
				"action_error": rres.Error,
			},
			Note: "verifikasi pasca-aksi (API success bukan berarti resolved)",
		})
	}
	return rres, snap
}

// verify menentukan bukti verifikasi deterministik. Bila verifier custom
// disuntikkan (pengujian), ia dipakai langsung; selain itu verifier bawaan
// berbasis registry + dispatcher.
func (v *VerificationGate) verify(ctx context.Context, name string, args map[string]any, rres tool.Result) caseengine.Verification {
	if v == nil {
		return caseengine.Verification{Passed: false, Source: "verification_engine", Summary: "gate tidak tersedia"}
	}
	if v.verifier != nil {
		return v.verifier(ctx, name, args, rres)
	}
	return v.defaultVerify(ctx, name, args, rres)
}

// defaultVerify mengimplementasikan verifikasi master spec §13: query ulang
// sistem yang terkena aksi lewat tool `verification` yang dideklarasikan di
// registry, lalu normalkan hasil ke status kanonik. Gagal/UNKNOWN/rencana
// kosong → fail-closed (TIDAK PERNAH RESOLVED).
func (v *VerificationGate) defaultVerify(ctx context.Context, name string, args map[string]any, rres tool.Result) caseengine.Verification {
	// Aksi harus berhasil dulu; klaim sukses API semata tidak cukup.
	if !rres.OK {
		return caseengine.Verification{Passed: false, Source: name, Summary: "aksi gagal: " + oneLine(rres.Error)}
	}

	// Rencana verifikasi diambil dari field `verification` tool di registry.
	verifyTool := ""
	if v.reg != nil {
		if t, ok := v.reg.Get(name); ok {
			verifyTool = strings.TrimSpace(t.Verification)
		}
	}
	if verifyTool == "" {
		return caseengine.Verification{Passed: false, Source: name, Summary: "aksi WRITE tanpa rencana verifikasi — tidak boleh RESOLVED"}
	}
	// Nilai `verification` yang bukan nama tool (mis. keyword "response_contract")
	// tidak bisa dipakai untuk query ulang → fail-closed.
	if v.reg != nil {
		if _, ok := v.reg.Get(verifyTool); !ok {
			return caseengine.Verification{Passed: false, Source: name, Summary: "rencana verifikasi " + verifyTool + " bukan tool terdaftar"}
		}
	}

	if v.disp == nil {
		return caseengine.Verification{Passed: false, Source: verifyTool, Summary: "dispatcher tidak tersedia untuk verifikasi"}
	}

	// Query ulang sistem yang terkena aksi.
	r := v.disp.Invoke(ctx, verifyTool, args)
	if !r.OK {
		return caseengine.Verification{Passed: false, Source: verifyTool, Summary: "verifikasi gagal: " + oneLine(r.Error)}
	}

	// Normalkan ke status kanonik; healthy state per domain menandakan pulih.
	domain := toolDomain(verifyTool)
	state := correlation.Normalize(domain, r.Output.Text)
	if state == healthyState(domain) {
		return caseengine.Verification{Passed: true, Source: verifyTool, Summary: oneLine(r.Output.Text)}
	}
	return caseengine.Verification{Passed: false, Source: verifyTool, Summary: "state sistem belum pulih: " + string(state) + " (" + oneLine(r.Output.Text) + ")"}
}

// healthyState memetakan domain ke status kanonik yang menandakan layanan pulih.
func healthyState(domain string) correlation.State {
	switch strings.ToLower(strings.TrimSpace(domain)) {
	case "billing":
		return correlation.StateActive
	case "radius":
		return correlation.StateAuthed
	case "mikrotik":
		return correlation.StatePPPoEUp
	case "genieacs":
		return correlation.StateDeviceOn
	default:
		return correlation.StateUnknown
	}
}

// toolIsWrite melaporkan apakah sebuah tool mengubah state (bukan READ).
// Tool tak dikenal dianggap write (fail-closed: wajib verifikasi).
func toolIsWrite(reg *registry.Registry, name string) bool {
	if reg == nil {
		return true
	}
	t, ok := reg.Get(name)
	if !ok {
		return true
	}
	return strings.ToUpper(strings.TrimSpace(string(t.Permission))) != "READ"
}
