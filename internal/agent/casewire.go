// casewire menghubungkan Case Engine (internal/caseengine) ke alur diagnosis.
//
// FASE 1 (dari docs/ORCHESTRATOR_WIRING_PLAN.md): setiap pesan masuk membuat
// atau melanjutkan Case dengan state yang benar, dan Case ID + state muncul di
// laporan/audit. Hanya sampai INVESTIGATION/ESCALATION — action, policy, dan
// verification adalah fase 2/3/4 dan TIDAK disentuh di sini.
//
// Prinsip aman: tracker boleh gagal diam-diam (dicatat ke log) tanpa pernah
// memblokir balasan ke pelanggan.
package agent

import (
	"fmt"
	"log"

	"ainoc/internal/audit"
	"ainoc/internal/caseengine"
	"ainoc/internal/standard"
)

// CaseWire memegang tracker case dan menerapkan aturan transisi fase 1.
// Semua mutasi state terjadi di dalam caseengine.Transition() yang menegakkan
// invariant (mis. RESOLVED wajib verifikasi) — jadi kode di sini tidak bisa
// "melompati" state machine.
type CaseWire struct {
	tracker *caseengine.Tracker
	aud     *audit.Store
	sink    func(caseengine.Record) // opsional: salinan ke penyimpanan lain (PostgreSQL)
}

// SetSink memasang penerima salinan case setiap kali ia bertransisi. Tidak boleh
// memblokir; kegagalannya tidak pernah memengaruhi alur diagnosis.
func (w *CaseWire) SetSink(f func(caseengine.Record)) {
	if w != nil {
		w.sink = f
	}
}

// NewCaseWire membuat wiring dengan tracker in-memory.
func NewCaseWire() *CaseWire {
	return &CaseWire{tracker: caseengine.NewTracker()}
}

// NewCaseWireWithAudit membuat wiring yang turut mencatat setiap transisi state
// case ke audit append-only (FASE 5: audit trail lengkap per §25).
func NewCaseWireWithAudit(aud *audit.Store) *CaseWire {
	return &CaseWire{tracker: caseengine.NewTracker(), aud: aud}
}

// SetAudit memasang store audit (idempotent). Bila nil, transisi tetap jalan
// tanpa jejak audit (backward-compatible).
func (w *CaseWire) SetAudit(aud *audit.Store) {
	if w == nil {
		return
	}
	w.aud = aud
}

// transition menerapkan transisi dan mencatatnya ke audit bila berhasil.
// Merupakan satu-satunya jalur mutasi state di package ini.
func (w *CaseWire) transition(c *caseengine.Case, next caseengine.State, actor, reason string) error {
	err := recordTransition(w.aud, c, next, actor, reason)
	w.persist()
	if w.sink != nil && c != nil {
		w.sink(c.Record())
	}
	return err
}

// persist menyimpan tracker bila path terpasang. Kegagalan hanya dicatat; tidak
// pernah memblokir atau mengubah balasan.
func (w *CaseWire) persist() {
	if w == nil || w.tracker == nil {
		return
	}
	if err := w.tracker.Save(); err != nil {
		log.Printf("[case] GAGAL simpan tracker: %v", err)
	}
}

// CaseSnapshot adalah proyeksi read-only yang disalin ke Report, supaya Report
// tidak memegang pointer ke Case yang bisa berubah di pesan berikutnya.
type CaseSnapshot struct {
	CaseID    string
	CaseState string
}

// begin dipanggil di awal RunWith: membuat case baru atau melanjutkan case lama
// untuk identity, lalu mendorong transisi pembuka yang valid.
//
//   - Case baru -> NEW -> IDENTIFYING -> CONVERSATION.
//   - Case lanjutan di CONVERSATION/WAITING_CUSTOMER -> tetap di CONVERSATION.
//   - Case investigasi lama yang sudah selesai -> mulai case baru (reset).
func (w *CaseWire) begin(identity, channel string) CaseSnapshot {
	if w == nil || w.tracker == nil {
		return CaseSnapshot{}
	}
	key := identity
	c, ok := w.tracker.Get(key)
	if !ok {
		c = w.tracker.Replace(key, channel)
		// Jalur pembuka: NEW -> IDENTIFYING -> CONVERSATION.
		// Gagal transisi di sini tidak pernah dibiarkan membatalkan diagnosis:
		// kita hanya log dan lanjut (case tetap NEW dengan ID yang valid).
		for _, next := range []caseengine.State{caseengine.StateIdentifying, caseengine.StateConversation} {
			if err := w.transition(c, next, "system", "pesan masuk"); err != nil {
				log.Printf("[case] transisi pembuka %s->%s gagal untuk %s: %v", c.State(), next, key, err)
				break
			}
		}
		return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
	}

	switch c.State() {
	case caseengine.StateInvestigation, caseengine.StateEscalation, caseengine.StateHumanHandling:
		// Investigasi sebelumnya selesai (fase ini berhenti di INVESTIGATION /
		// ESCALATION). Pesan baru berarti kontak baru -> case baru.
		c = w.tracker.Replace(key, channel)
		for _, next := range []caseengine.State{caseengine.StateIdentifying, caseengine.StateConversation} {
			if err := w.transition(c, next, "system", "kontak baru setelah investigasi"); err != nil {
				log.Printf("[case] transisi pembuka %s->%s gagal untuk %s: %v", c.State(), next, key, err)
				break
			}
		}
	case caseengine.StateWaitingCustomer:
		// Pelanggan menjawab pertanyaan -> kembali ke CONVERSATION.
		_ = w.transition(c, caseengine.StateConversation, "system", "pelanggan menjawab")
	case caseengine.StateConversation, caseengine.StateInformationGathering:
		// Sudah dalam percakapan aktif: lanjutkan.
	default:
		// State lain (baru sebagian terbentuk, dsb.) -> dorong ke CONVERSATION
		// bila legal; kalau tidak, biarkan di state sekarang.
		if err := w.transition(c, caseengine.StateConversation, "system", "lanjut percakapan"); err != nil {
			log.Printf("[case] transisi lanjut %s->CONVERSATION gagal untuk %s: %v", c.State(), key, err)
		}
	}
	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}

// onDiagnosis dipanggil SETELAH klasifikasi intent diketahui: mendorong keluhan
// nyata ke jalur diagnosis (CONVERSATION -> READY_FOR_DIAGNOSIS -> REASONING),
// dan mengembalikan snapshot state terkini.
func (w *CaseWire) onDiagnosis(identity string, intent standard.Intent) CaseSnapshot {
	if w == nil || w.tracker == nil {
		return CaseSnapshot{}
	}
	c, ok := w.tracker.Get(identity)
	if !ok {
		return CaseSnapshot{}
	}
	if !standard.BolehProbe(intent) {
		// Non-keluhan (CHAT/INFO/UNCLEAR) tetap di CONVERSATION; UNCLEAR yang
		// butuh klarifikasi bisa masuk WAITING_CUSTOMER.
		if intent == standard.IntentUnclear && c.State() == caseengine.StateConversation {
			_ = w.transition(c, caseengine.StateWaitingCustomer, "system", "perlu klarifikasi")
		}
		return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
	}

	// Keluhan nyata: CONVERSATION -> READY_FOR_DIAGNOSIS -> REASONING.
	// Urutan dibangun dari state aktual supaya transisi tetap legal walau case
	// baru saja dibuka (sudah CONVERSATION) atau berada di INFORMATION_GATHERING.
	switch c.State() {
	case caseengine.StateConversation, caseengine.StateInformationGathering, caseengine.StateWaitingCustomer:
		if err := w.transition(c, caseengine.StateReadyForDiagnosis, "system", "keluhan terkonfirmasi"); err != nil {
			log.Printf("[case] transisi ->READY_FOR_DIAGNOSIS gagal untuk %s: %v", identity, err)
			break
		}
		fallthrough
	case caseengine.StateReadyForDiagnosis:
		if err := w.transition(c, caseengine.StateReasoning, "system", "mulai diagnosis"); err != nil {
			log.Printf("[case] transisi ->REASONING gagal untuk %s: %v", identity, err)
		}
	case caseengine.StateReasoning, caseengine.StateInvestigation:
		// Sudah dalam diagnosis: biarkan.
	default:
		log.Printf("[case] state %s tidak masuk jalur diagnosis untuk %s", c.State(), identity)
	}
	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}

// Snapshot mengembalikan proyeksi read-only case untuk identity. Identity
// tak dikenal mengembalikan snapshot kosong (bukan error).
func (w *CaseWire) Snapshot(identity string) CaseSnapshot {
	if w == nil || w.tracker == nil {
		return CaseSnapshot{}
	}
	c, ok := w.tracker.Get(identity)
	if !ok {
		return CaseSnapshot{}
	}
	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}

// AllCases mengembalikan salinan semua case yang sedang terlacak (terbaru dulu)
// untuk observabilitas FASE 5: tabel case aktif + KPI dari case engine.
func (w *CaseWire) AllCases() []*caseengine.Case {
	if w == nil || w.tracker == nil {
		return nil
	}
	return w.tracker.All()
}

// Tracker mengembalikan tracker underlying (untuk restore/persist observabilitas).
func (w *CaseWire) Tracker() *caseengine.Tracker {
	if w == nil {
		return nil
	}
	return w.tracker
}

// ---- FASE 4: Verification Engine ----

// onActionStarted — FASE 4 — mendorong case lewat jalur eksekusi aksi bila
// legal. Case tiba di sini setelah gerbang kebijakan memutuskan ALLOW dan
// action benar-benar akan dieksekusi:
//
//	ACTION_PROPOSED → POLICY_CHECK → EXECUTING
//
// Jalur lengkap pasca-aksi (dilakukan pemanggil lewat onActionCompleted):
//
//	EXECUTING → VERIFYING → (RecordVerification Passed) → RESOLVED
//	EXECUTING → VERIFYING → (RecordVerification gagal) → FAILED → ESCALATION
//
// Tanpa panggilan ini, tidak ada case yang boleh sampai RESOLVED: state
// machine menolak RESOLVED tanpa verification yang Passed (invariant fase 1).
func (w *CaseWire) onActionStarted(identity, tool string) CaseSnapshot {
	if w == nil || w.tracker == nil {
		return CaseSnapshot{}
	}
	c, ok := w.tracker.Get(identity)
	if !ok {
		log.Printf("[verify] case untuk %s tidak ditemukan — action %s tanpa case", identity, tool)
		return CaseSnapshot{}
	}
	switch c.State() {
	case caseengine.StateReasoning, caseengine.StateInvestigation, caseengine.StateActionProposed:
		if c.State() == caseengine.StateReasoning || c.State() == caseengine.StateInvestigation {
			if err := w.transition(c, caseengine.StateActionProposed, "agent", "aksi diusulkan AI: "+tool); err != nil {
				log.Printf("[verify] transisi ->ACTION_PROPOSED gagal untuk %s: %v", identity, err)
			}
		}
		if c.State() == caseengine.StateActionProposed {
			if err := w.transition(c, caseengine.StatePolicyCheck, "policy", "kebijakan ALLOW: "+tool); err != nil {
				log.Printf("[verify] transisi ->POLICY_CHECK gagal untuk %s: %v", identity, err)
			}
		}
		if c.State() == caseengine.StatePolicyCheck {
			if err := w.transition(c, caseengine.StateExecuting, "agent", "eksekusi aksi: "+tool); err != nil {
				log.Printf("[verify] transisi ->EXECUTING gagal untuk %s: %v", identity, err)
			}
		}
	case caseengine.StateExecuting, caseengine.StateVerifying:
		// Sudah dalam jalur aksi — biarkan (idempotent).
	default:
		log.Printf("[verify] state %s tidak masuk jalur aksi untuk %s (tool %s)", c.State(), identity, tool)
	}
	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}

// onActionCompleted — FASE 4 — menutup jalur aksi setelah verifikasi nyata.
// Ini satu-satunya jalan legal menuju RESOLVED:
//
//	EXECUTING → VERIFYING → RecordVerification(Passed) → RESOLVED
//	EXECUTING → VERIFYING → RecordVerification(Passed=false) → FAILED → ESCALATION
//
// Verification dulu dicatat (hanya boleh saat VERIFYING), baru transisi:
// Passed=true membuka RESOLVED; Passed=false (atau verifikasi tidak dapat
// dicatat) memaksa FAILED → ESCALATION. Kode ini TIDAK PERNAH mengklaim
// RESOLVED tanpa bukti yang tercatat (invariant caseengine).
func (w *CaseWire) onActionCompleted(identity, tool string, verification caseengine.Verification) CaseSnapshot {
	if w == nil || w.tracker == nil {
		return CaseSnapshot{}
	}
	c, ok := w.tracker.Get(identity)
	if !ok {
		log.Printf("[verify] case untuk %s tidak ditemukan — hasil aksi %s tanpa case", identity, tool)
		return CaseSnapshot{}
	}

	// Masuk ke VERIFYING dari EXECUTING (atau ACTION_PROPOSED/POLICY_CHECK bila
	// action dieksekusi tanpa jejak mulai — tetap legal menuju VERIFYING hanya
	// dari EXECUTING; state lain dicatat sebagai kondisi yang tidak bisa
	// diverifikasi dan jatuh ke eskalasi).
	switch c.State() {
	case caseengine.StateExecuting:
		// lanjut ke VERIFYING di bawah
	case caseengine.StateActionProposed, caseengine.StatePolicyCheck:
		// Action dieksekusi tanpa onActionStarted — lengkapi jejak dulu.
		if c.State() == caseengine.StateActionProposed {
			_ = w.transition(c, caseengine.StatePolicyCheck, "policy", "kebijakan ALLOW: "+tool)
		}
		if c.State() == caseengine.StatePolicyCheck {
			_ = w.transition(c, caseengine.StateExecuting, "agent", "eksekusi aksi: "+tool)
		}
	case caseengine.StateVerifying:
		// Sudah VERIFYING — catat langsung.
	default:
		log.Printf("[verify] state %s tidak bisa diverifikasi untuk %s (tool %s)", c.State(), identity, tool)
	}

	if c.State() != caseengine.StateVerifying {
		if err := w.transition(c, caseengine.StateVerifying, "agent", "mulai verifikasi: "+tool); err != nil {
			log.Printf("[verify] transisi ->VERIFYING gagal untuk %s: %v", identity, err)
		}
	}

	// Catat bukti verifikasi. Hanya boleh saat VERIFYING — bila state belum
	// sampai VERIFYING (transisi gagal), RecordVerification akan menolak dan
	// jalur di bawah memaksa eskalasi (fail-closed, tidak pernah RESOLVED).
	recorded := false
	if c.State() == caseengine.StateVerifying {
		if err := c.RecordVerification(verification); err != nil {
			log.Printf("[verify] RecordVerification gagal untuk %s: %v", identity, err)
		} else {
			recorded = true
		}
	}

	if recorded && verification.Passed {
		if err := w.transition(c, caseengine.StateResolved, "verification", fmt.Sprintf("verifikasi lulus: %s", verification.Source)); err != nil {
			log.Printf("[verify] transisi ->RESOLVED gagal untuk %s: %v", identity, err)
		}
	} else {
		// Verifikasi gagal (atau tidak dapat dicatat) → FAILED → ESCALATION.
		if c.State() == caseengine.StateVerifying {
			_ = w.transition(c, caseengine.StateFailed, "verification", "verifikasi tidak lulus")
		}
		if c.State() == caseengine.StateFailed {
			if err := w.transition(c, caseengine.StateEscalation, "verification", "verifikasi gagal → eskalasi"); err != nil {
				log.Printf("[verify] transisi ->ESCALATION gagal untuk %s: %v", identity, err)
			}
		}
	}
	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}

//	ACTION_PROPOSED → POLICY_CHECK → ESCALATION
//
// Keputusan ALLOW tidak pernah lewat sini (LOW read-only dieksekusi inline di
// REASONING). Transisi ilegal hanya dicatat ke log — state machine caseengine
// adalah otoritas final, kode ini tidak bisa melompati invariant.
func (w *CaseWire) onPolicyBlock(identity, tool, decision, reason string) {
	if w == nil || w.tracker == nil {
		return
	}
	c, ok := w.tracker.Get(identity)
	if !ok {
		log.Printf("[policy] case untuk %s tidak ditemukan — eskalasi kebijakan tanpa case", identity)
		return
	}
	// ACTION_PROPOSED legal dari REASONING atau INVESTIGATION (graph caseengine).
	switch c.State() {
	case caseengine.StateReasoning, caseengine.StateInvestigation, caseengine.StateActionProposed:
		if c.State() == caseengine.StateReasoning || c.State() == caseengine.StateInvestigation {
			if err := w.transition(c, caseengine.StateActionProposed, "agent",
				"aksi diusulkan AI: "+tool); err != nil {
				log.Printf("[policy] transisi ->ACTION_PROPOSED gagal untuk %s: %v", identity, err)
			}
		}
		if c.State() == caseengine.StateActionProposed {
			if err := w.transition(c, caseengine.StatePolicyCheck, "policy",
				"evaluasi kebijakan: "+tool); err != nil {
				log.Printf("[policy] transisi ->POLICY_CHECK gagal untuk %s: %v", identity, err)
			}
		}
	default:
		log.Printf("[policy] state %s tidak masuk jalur aksi untuk %s (tool %s)", c.State(), identity, tool)
	}
	if c.State() == caseengine.StatePolicyCheck {
		if err := w.transition(c, caseengine.StateEscalation, "policy",
			"kebijakan menahan aksi "+tool+": "+decision+" ("+reason+")"); err != nil {
			log.Printf("[policy] transisi ->ESCALATION gagal untuk %s: %v", identity, err)
		}
	}
}

// onResult dipanggil SETELAH diagnosis selesai: menutup jalur REASONING ->
// INVESTIGATION, dan menandai ESCALATION bila diagnosis benar-benar berjalan
// namun verdict tidak diketahui / keyakinan rendah. Pengiriman ke nomor tujuan
// adalah fase 2 — di sini hanya state yang dipindahkan.
//
// diagnosed=true berarti diagnosis (workflow/probe) benar-benar berjalan; false
// berarti pesan keluhan dijawab tanpa pengecekan (mis. model minta klarifikasi)
// — kasus ini tidak di-eskalasi.
func (w *CaseWire) onResult(identity string, intent standard.Intent, verdict string, confidence float64, diagnosed bool) CaseSnapshot {
	if w == nil || w.tracker == nil {
		return CaseSnapshot{}
	}
	c, ok := w.tracker.Get(identity)
	if !ok {
		return CaseSnapshot{}
	}

	if !standard.BolehProbe(intent) {
		// Obrolan/info/klarifikasi: tidak ada diagnosis yang ditutup.
		return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
	}

	// Tutup REASONING/READY_FOR_DIAGNOSIS -> INVESTIGATION supaya case tidak
	// tersangkut di state pertengahan.
	if c.State() == caseengine.StateReasoning || c.State() == caseengine.StateReadyForDiagnosis {
		if err := w.transition(c, caseengine.StateInvestigation, "system", "diagnosis selesai"); err != nil {
			log.Printf("[case] transisi ->INVESTIGATION gagal untuk %s: %v", identity, err)
		}
	}

	// Verdict tak dikenal / keyakinan rendah pada diagnosis nyata -> tandai
	// ESCALATION (hanya state; pengiriman handoff = fase 2).
	if diagnosed && c.State() == caseengine.StateInvestigation {
		if verdict == "" || verdict == "TIDAK DIKETAHUI" || confidence < 60 {
			if err := w.transition(c, caseengine.StateEscalation, "system", "verdict tidak pasti / keyakinan rendah"); err != nil {
				log.Printf("[case] transisi ->ESCALATION gagal untuk %s: %v", identity, err)
			}
		}
	}

	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}
