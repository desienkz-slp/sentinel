package conversation

import (
	"strings"
	"testing"
	"time"

	"ainoc/internal/caseengine"
)

func TestProcessTransitionsToReadyOnlyAfterRequiredFacts(t *testing.T) {
	c := caseengine.New("whatsapp", "628111222333", time.Now())

	first := Process(c, "Internet saya lemot", Context{IdentityKnown: true})
	if first.Intent != IntentSlowConnection {
		t.Fatalf("intent pertama = %q, ingin %q", first.Intent, IntentSlowConnection)
	}
	if first.State != caseengine.StateWaitingCustomer {
		t.Fatalf("state setelah data belum cukup = %s, ingin WAITING_CUSTOMER", first.State)
	}
	if first.Ready {
		t.Fatal("case tidak boleh siap sebelum data wajib lengkap")
	}

	second := Process(c, "Semua perangkat, masih bisa dipakai tapi lambat sejak sore", Context{
		IdentityKnown: true,
		Facts:         first.Facts,
	})
	if !second.Ready {
		t.Fatalf("data lengkap harus siap, missing=%v", second.Missing)
	}
	if second.State != caseengine.StateReadyForDiagnosis {
		t.Fatalf("state data lengkap = %s, ingin READY_FOR_DIAGNOSIS", second.State)
	}
}

func TestProcessNeverMovesUnknownIntentToReady(t *testing.T) {
	c := caseengine.New("whatsapp", "628111222333", time.Now())
	result := Process(c, "Halo", Context{IdentityKnown: true})

	if result.Ready || result.State == caseengine.StateReadyForDiagnosis {
		t.Fatalf("intent tidak jelas tidak boleh siap diagnosis: %+v", result)
	}
	if result.Intent != IntentUnknown {
		t.Fatalf("intent = %q, ingin %q", result.Intent, IntentUnknown)
	}
}

func TestCustomerReplyDoesNotLeakInternalData(t *testing.T) {
	reply := CustomerReply(Response{
		Status: "Kami sedang memeriksa",
		InternalFacts: []string{
			"Authorization: Bearer rahasia", "tool=billing.get_customer", "chain-of-thought: analisis privat",
			"raw output: {\"password\":\"rahasia\"}",
		},
		NextQuestion: "Apakah kendala terjadi di semua perangkat atau hanya salah satu?",
	})

	for _, forbidden := range []string{"bearer", "billing", "chain-of-thought", "raw output", "password", "rahasia"} {
		if strings.Contains(strings.ToLower(reply), forbidden) {
			t.Fatalf("balasan membocorkan %q: %q", forbidden, reply)
		}
	}
	if !strings.Contains(reply, "semua perangkat") {
		t.Fatalf("pertanyaan aman tidak ada dalam balasan: %q", reply)
	}
}
