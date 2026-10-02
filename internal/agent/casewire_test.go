package agent

import (
	"testing"

	"ainoc/internal/caseengine"
	"ainoc/internal/standard"
)

func TestCaseWireFirstMessageOpensConversation(t *testing.T) {
	w := NewCaseWire()
	snap := w.begin("628111222333", "whatsapp")
	if snap.CaseID == "" {
		t.Fatal("case ID tidak boleh kosong")
	}
	if snap.CaseState != string(caseengine.StateConversation) {
		t.Fatalf("state setelah pesan pertama = %s, ingin CONVERSATION", snap.CaseState)
	}
}

func TestCaseWireComplaintReachesInvestigation(t *testing.T) {
	w := NewCaseWire()
	w.begin("628111222333", "whatsapp")
	snap := w.onDiagnosis("628111222333", standard.IntentComplaint)
	if snap.CaseState != string(caseengine.StateReasoning) {
		t.Fatalf("state setelah keluhan = %s, ingin REASONING", snap.CaseState)
	}
	snap = w.onResult("628111222333", standard.IntentComplaint, "GANGGUAN", 85, true)
	if snap.CaseState != string(caseengine.StateInvestigation) {
		t.Fatalf("state setelah diagnosis selesai = %s, ingin INVESTIGATION", snap.CaseState)
	}
}

func TestCaseWireComplaintUnknownVerdictEscalates(t *testing.T) {
	w := NewCaseWire()
	w.begin("628111222333", "whatsapp")
	w.onDiagnosis("628111222333", standard.IntentComplaint)
	snap := w.onResult("628111222333", standard.IntentComplaint, "TIDAK DIKETAHUI", 30, true)
	if snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("state verdict tidak pasti = %s, ingin ESCALATION", snap.CaseState)
	}
}

func TestCaseWireLowConfidenceEscalates(t *testing.T) {
	w := NewCaseWire()
	w.begin("628111222333", "whatsapp")
	w.onDiagnosis("628111222333", standard.IntentComplaint)
	snap := w.onResult("628111222333", standard.IntentComplaint, "GANGGUAN", 40, true)
	if snap.CaseState != string(caseengine.StateEscalation) {
		t.Fatalf("state keyakinan rendah = %s, ingin ESCALATION", snap.CaseState)
	}
}

func TestCaseWireChatStaysConversation(t *testing.T) {
	w := NewCaseWire()
	w.begin("628111222333", "whatsapp")
	snap := w.onDiagnosis("628111222333", standard.IntentChat)
	if snap.CaseState != string(caseengine.StateConversation) {
		t.Fatalf("state pesan obrolan = %s, ingin CONVERSATION", snap.CaseState)
	}
	snap = w.onResult("628111222333", standard.IntentChat, "", 0, false)
	if snap.CaseState != string(caseengine.StateConversation) {
		t.Fatalf("state setelah obrolan = %s, ingin CONVERSATION", snap.CaseState)
	}
}

func TestCaseWireUnclearWaitsCustomer(t *testing.T) {
	w := NewCaseWire()
	w.begin("628111222333", "whatsapp")
	snap := w.onDiagnosis("628111222333", standard.IntentUnclear)
	if snap.CaseState != string(caseengine.StateWaitingCustomer) {
		t.Fatalf("state pesan tidak jelas = %s, ingin WAITING_CUSTOMER", snap.CaseState)
	}
}

func TestCaseWireContinuationReusesSameCase(t *testing.T) {
	w := NewCaseWire()
	first := w.begin("628111222333", "whatsapp")
	// Lanjutkan percakapan yang sama -> case ID tidak berubah.
	second := w.begin("628111222333", "whatsapp")
	if second.CaseID != first.CaseID {
		t.Fatalf("pesan lanjutan harus memakai case sama: %s vs %s", second.CaseID, first.CaseID)
	}
}

func TestCaseWireNewContactAfterInvestigation(t *testing.T) {
	w := NewCaseWire()
	first := w.begin("628111222333", "whatsapp")
	w.onDiagnosis("628111222333", standard.IntentComplaint)
	w.onResult("628111222333", standard.IntentComplaint, "GANGGUAN", 85, true)
	// Investigasi selesai -> pesan berikutnya mulai case baru.
	second := w.begin("628111222333", "whatsapp")
	if second.CaseID == first.CaseID {
		t.Fatalf("kontak baru setelah investigasi harus punya case baru, dapat %s", second.CaseID)
	}
}

func TestCaseWireNilSafe(t *testing.T) {
	var w *CaseWire
	if s := w.begin("x", "whatsapp"); s.CaseID != "" || s.CaseState != "" {
		t.Fatal("begin pada CaseWire nil harus kosong")
	}
	if s := w.onDiagnosis("x", standard.IntentComplaint); s.CaseID != "" {
		t.Fatal("onDiagnosis pada CaseWire nil harus kosong")
	}
	if s := w.onResult("x", standard.IntentComplaint, "GANGGUAN", 90, true); s.CaseID != "" {
		t.Fatal("onResult pada CaseWire nil harus kosong")
	}
}
