package conversation

import (
	"strings"

	"ainoc/internal/caseengine"
)

// Intent adalah klasifikasi percakapan yang dipakai oleh pengumpulan data.
// Klasifikasi ini deterministik agar transisi case tidak bergantung pada model.
type Intent string

const (
	IntentUnknown        Intent = ""
	IntentSlowConnection Intent = "slow_connection"
	IntentInternetDown   Intent = "internet_down"
	IntentIntermittent   Intent = "intermittent"
	IntentWiFiIssue      Intent = "wifi_issue"
)

// Result adalah hasil satu pesan pelanggan. Facts hanya berisi informasi yang
// disampaikan pelanggan, bukan fakta live dari adaptor eksternal.
type Result struct {
	Intent  Intent
	Facts   map[string]string
	Missing []string
	Ready   bool
	State   caseengine.State
	Reply   string
}

// DetectIntent mengenali kategori keluhan tanpa melakukan diagnosis teknis.
func DetectIntent(message string) Intent {
	message = strings.ToLower(strings.TrimSpace(message))
	switch {
	case containsAny(message, "lemot", "lambat", "slow"):
		return IntentSlowConnection
	case containsAny(message, "putus-putus", "putus putus", "intermiten"):
		return IntentIntermittent
	case containsAny(message, "wifi", "wi-fi"):
		return IntentWiFiIssue
	case containsAny(message, "tidak bisa internet", "internet mati", "internet putus", "tidak ada koneksi", "offline"):
		return IntentInternetDown
	default:
		return IntentUnknown
	}
}

// Process mengumpulkan fakta pelanggan, menghitung kecukupan, lalu memindahkan
// lifecycle case secara legal. READY_FOR_DIAGNOSIS hanya dapat dicapai sesudah
// seluruh field wajib intent tersedia.
func Process(c *caseengine.Case, message string, ctx Context) Result {
	facts := copyFacts(ctx.Facts)
	intent := DetectIntent(message)
	if intent == IntentUnknown {
		return apply(c, Plan{Missing: []string{"problem"}}, intent, facts,
			"Agar saya bisa membantu, boleh jelaskan kendala koneksinya?")
	}

	collectFacts(facts, message, intent)
	plan := Evaluate(string(intent), Context{IdentityKnown: ctx.IdentityKnown, Facts: facts})
	question := questionFor(plan.Missing)
	return apply(c, plan, intent, facts, question)
}

func apply(c *caseengine.Case, plan Plan, intent Intent, facts map[string]string, reply string) Result {
	result := Result{Intent: intent, Facts: facts, Missing: append([]string(nil), plan.Missing...), Ready: plan.Ready, Reply: reply}
	if c == nil {
		return result
	}

	advanceToGathering(c)
	if plan.Ready {
		if c.State() == caseengine.StateInformationGathering {
			_ = c.Transition(caseengine.StateReadyForDiagnosis, "conversation", "informasi pelanggan lengkap")
		}
	} else if c.State() == caseengine.StateInformationGathering {
		_ = c.Transition(caseengine.StateWaitingCustomer, "conversation", "menunggu informasi pelanggan")
	}
	result.State = c.State()
	return result
}

func advanceToGathering(c *caseengine.Case) {
	switch c.State() {
	case caseengine.StateNew:
		_ = c.Transition(caseengine.StateIdentifying, "conversation", "pesan pelanggan diterima")
		_ = c.Transition(caseengine.StateConversation, "conversation", "identitas kanal tersedia")
		_ = c.Transition(caseengine.StateInformationGathering, "conversation", "mengumpulkan informasi keluhan")
	case caseengine.StateIdentifying:
		_ = c.Transition(caseengine.StateConversation, "conversation", "mulai percakapan")
		_ = c.Transition(caseengine.StateInformationGathering, "conversation", "mengumpulkan informasi keluhan")
	case caseengine.StateConversation:
		_ = c.Transition(caseengine.StateInformationGathering, "conversation", "mengumpulkan informasi keluhan")
	case caseengine.StateWaitingCustomer:
		_ = c.Transition(caseengine.StateConversation, "conversation", "balasan pelanggan diterima")
		_ = c.Transition(caseengine.StateInformationGathering, "conversation", "melengkapi informasi keluhan")
	}
}

func collectFacts(facts map[string]string, message string, intent Intent) {
	text := strings.ToLower(strings.TrimSpace(message))
	if containsAny(text, "semua perangkat", "semua", "seluruh perangkat") {
		facts["scope"] = "all_devices"
	} else if containsAny(text, "satu perangkat", "salah satu", "hp saya", "laptop saya") {
		facts["scope"] = "single_device"
	}
	if i := strings.Index(text, "sejak "); i >= 0 && strings.TrimSpace(text[i+len("sejak "):]) != "" {
		facts["started"] = strings.TrimSpace(text[i+len("sejak "):])
	}
	switch {
	case containsAny(text, "masih bisa", "bisa dipakai", "tetap bisa"):
		facts["condition"] = "available"
	case containsAny(text, "tidak bisa", "mati", "putus total", "tidak ada koneksi"):
		facts["condition"] = "unavailable"
	case intent == IntentSlowConnection:
		facts["condition"] = "available_but_slow"
	}
}

func questionFor(missing []string) string {
	if len(missing) == 0 {
		return "Baik, informasi awalnya sudah cukup. Saya lanjutkan pengecekan awal ya."
	}
	switch missing[0] {
	case "problem":
		return "Agar saya bisa membantu, boleh jelaskan kendala koneksinya?"
	case "scope":
		return "Kendalanya terjadi di semua perangkat atau hanya salah satu perangkat?"
	case "condition":
		return "Koneksinya masih bisa digunakan, atau sama sekali tidak bisa digunakan?"
	case "started":
		return "Sejak kapan kendala ini mulai terasa?"
	default:
		return "Boleh ceritakan sedikit lagi kondisi yang sedang dialami?"
	}
}

func containsAny(text string, values ...string) bool {
	for _, value := range values {
		if strings.Contains(text, value) {
			return true
		}
	}
	return false
}

func copyFacts(source map[string]string) map[string]string {
	out := make(map[string]string, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}

// Response adalah input terbatas untuk balasan pelanggan. InternalFacts sengaja
// tidak pernah digunakan; data itu hanya untuk audit atau reasoning internal.
type Response struct {
	Status        string
	NextQuestion  string
	InternalFacts []string
}

// CustomerReply menyusun teks customer-safe: hanya status yang aman dan langkah
// berikutnya. Kredensial, tool, reasoning internal, serta output mentah dibuang.
func CustomerReply(response Response) string {
	status := strings.TrimSpace(response.Status)
	if containsSensitive(status) {
		status = "Baik, saya sedang memeriksa informasi yang diperlukan."
	}
	question := strings.TrimSpace(response.NextQuestion)
	if containsSensitive(question) {
		question = "Mohon tunggu sebentar, saya lanjutkan pemeriksaan awalnya."
	}
	return strings.TrimSpace(strings.Join(nonEmpty(status, question), " "))
}

func containsSensitive(text string) bool {
	text = strings.ToLower(text)
	return containsAny(text, "bearer", "password", "token", "credential", "tool", "api", "raw output", "chain-of-thought", "chain of thought", "internal")
}

func nonEmpty(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
