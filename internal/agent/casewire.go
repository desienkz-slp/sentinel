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
	"log"

	"ainoc/internal/caseengine"
	"ainoc/internal/standard"
)

// CaseWire memegang tracker case dan menerapkan aturan transisi fase 1.
// Semua mutasi state terjadi di dalam caseengine.Transition() yang menegakkan
// invariant (mis. RESOLVED wajib verifikasi) — jadi kode di sini tidak bisa
// "melompati" state machine.
type CaseWire struct {
	tracker *caseengine.Tracker
}

// NewCaseWire membuat wiring dengan tracker in-memory.
func NewCaseWire() *CaseWire {
	return &CaseWire{tracker: caseengine.NewTracker()}
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
			if err := c.Transition(next, "system", "pesan masuk"); err != nil {
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
			if err := c.Transition(next, "system", "kontak baru setelah investigasi"); err != nil {
				log.Printf("[case] transisi pembuka %s->%s gagal untuk %s: %v", c.State(), next, key, err)
				break
			}
		}
	case caseengine.StateWaitingCustomer:
		// Pelanggan menjawab pertanyaan -> kembali ke CONVERSATION.
		_ = c.Transition(caseengine.StateConversation, "system", "pelanggan menjawab")
	case caseengine.StateConversation, caseengine.StateInformationGathering:
		// Sudah dalam percakapan aktif: lanjutkan.
	default:
		// State lain (baru sebagian terbentuk, dsb.) -> dorong ke CONVERSATION
		// bila legal; kalau tidak, biarkan di state sekarang.
		if err := c.Transition(caseengine.StateConversation, "system", "lanjut percakapan"); err != nil {
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
			_ = c.Transition(caseengine.StateWaitingCustomer, "system", "perlu klarifikasi")
		}
		return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
	}

	// Keluhan nyata: CONVERSATION -> READY_FOR_DIAGNOSIS -> REASONING.
	// Urutan dibangun dari state aktual supaya transisi tetap legal walau case
	// baru saja dibuka (sudah CONVERSATION) atau berada di INFORMATION_GATHERING.
	switch c.State() {
	case caseengine.StateConversation, caseengine.StateInformationGathering, caseengine.StateWaitingCustomer:
		if err := c.Transition(caseengine.StateReadyForDiagnosis, "system", "keluhan terkonfirmasi"); err != nil {
			log.Printf("[case] transisi ->READY_FOR_DIAGNOSIS gagal untuk %s: %v", identity, err)
			break
		}
		fallthrough
	case caseengine.StateReadyForDiagnosis:
		if err := c.Transition(caseengine.StateReasoning, "system", "mulai diagnosis"); err != nil {
			log.Printf("[case] transisi ->REASONING gagal untuk %s: %v", identity, err)
		}
	case caseengine.StateReasoning, caseengine.StateInvestigation:
		// Sudah dalam diagnosis: biarkan.
	default:
		log.Printf("[case] state %s tidak masuk jalur diagnosis untuk %s", c.State(), identity)
	}
	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
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
		if err := c.Transition(caseengine.StateInvestigation, "system", "diagnosis selesai"); err != nil {
			log.Printf("[case] transisi ->INVESTIGATION gagal untuk %s: %v", identity, err)
		}
	}

	// Verdict tak dikenal / keyakinan rendah pada diagnosis nyata -> tandai
	// ESCALATION (hanya state; pengiriman handoff = fase 2).
	if diagnosed && c.State() == caseengine.StateInvestigation {
		if verdict == "" || verdict == "TIDAK DIKETAHUI" || confidence < 60 {
			if err := c.Transition(caseengine.StateEscalation, "system", "verdict tidak pasti / keyakinan rendah"); err != nil {
				log.Printf("[case] transisi ->ESCALATION gagal untuk %s: %v", identity, err)
			}
		}
	}

	return CaseSnapshot{CaseID: string(c.ID), CaseState: string(c.State())}
}
