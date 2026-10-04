package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"ainoc/internal/agent"
	"ainoc/internal/billing"
	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/memory"
	"ainoc/internal/observability"
	"ainoc/internal/session"
	"ainoc/internal/wa"
)

// Webhook coverage for the QA harness: unknown callers must be resolved before
// normal diagnosis, even when the message carries a non-authorizing QA label.
func TestWebhookUnknownSyntheticComplaintUsesCSUnknown(t *testing.T) {
	for _, tc := range []struct {
		name    string
		message string
	}{
		{name: "complaint", message: "internet saya mati total"},
		{name: "qa read only label is not authorization", message: "[QA READ-ONLY] internet saya mati total"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := qaWebhookServer(t, nil)
			out := h.post(t, "628555000001", tc.message)

			if out.Engine != "cs-unknown" {
				t.Fatalf("engine = %q, want cs-unknown; response=%+v", out.Engine, out)
			}
			if !strings.Contains(strings.ToLower(out.Report), "lokasi") {
				t.Fatalf("cs-unknown reply must ask for location: %q", out.Report)
			}
			if out.Reply != "" {
				t.Fatalf("webhook must not request gateway auto-reply: %q", out.Reply)
			}
			if h.llmCalls() != 0 {
				t.Fatalf("normal engine reached %d times", h.llmCalls())
			}
			if h.waSendCalls() != 0 {
				t.Fatalf("outbound WA sends = %d", h.waSendCalls())
			}
			if h.nonGETBillingCalls() != 0 {
				t.Fatalf("billing writes = %d", h.nonGETBillingCalls())
			}
		})
	}
}

func TestWebhookKnownSyntheticCustomerBypassesCSUnknown(t *testing.T) {
	const number = "628555000002"
	h := qaWebhookServer(t, []qaBillingCustomer{{
		Name: "Pelanggan Sintetis", Username: "qa-customer", Phone: number, Status: "active",
	}})

	out := h.post(t, number, "halo")
	if out.Engine == "cs-unknown" {
		t.Fatalf("known billing customer must bypass cs-unknown: %+v", out)
	}
	if out.Engine != "llm (tanpa pengecekan)" {
		t.Fatalf("engine = %q, want customer-safe normal path; response=%+v", out.Engine, out)
	}
	if !strings.Contains(out.Report, "Halo Pelanggan Sintetis") {
		t.Fatalf("customer-safe normal response missing: %q", out.Report)
	}
	if !h.sawKnownCustomer() {
		t.Fatal("normal engine did not receive the billing-backed customer identity")
	}
	if h.waSendCalls() != 0 {
		t.Fatalf("outbound WA sends = %d", h.waSendCalls())
	}
	if h.nonGETBillingCalls() != 0 {
		t.Fatalf("billing writes = %d", h.nonGETBillingCalls())
	}
}

type qaBillingCustomer struct {
	Name     string `json:"name"`
	Username string `json:"username"`
	Phone    string `json:"phone"`
	Status   string `json:"status"`
}

type qaWebhookHarness struct {
	h http.Handler

	mu               sync.Mutex
	llm              int
	waSend           int
	billingNonGET    int
	knownCustomerSet bool
}

func qaWebhookServer(t *testing.T, customers []qaBillingCustomer) *qaWebhookHarness {
	t.Helper()
	h := &qaWebhookHarness{}

	billingServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.Method != http.MethodGet {
			h.billingNonGET++
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/api/noc/v1/customers" {
			http.NotFound(w, r)
			return
		}
		query := r.URL.Query().Get("search")
		matched := make([]qaBillingCustomer, 0, 1)
		for _, customer := range customers {
			if strings.Contains(customer.Phone+" "+customer.Name+" "+customer.Username, query) {
				matched = append(matched, customer)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": matched})
	}))
	t.Cleanup(billingServer.Close)

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.llm++
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		for _, message := range req.Messages {
			if strings.Contains(message.Content, "Pengirim adalah pelanggan terdaftar: Pelanggan Sintetis") {
				h.knownCustomerSet = true
			}
		}
		h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"BALASAN: Halo Pelanggan Sintetis, ada yang bisa kami bantu?"}}]}`))
	}))
	t.Cleanup(llmServer.Close)

	gatewayServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		if r.Method == http.MethodPost && r.URL.Path == "/api/whatsapp/send" {
			h.waSend++
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"connected":false}`))
	}))
	t.Cleanup(gatewayServer.Close)

	cfg := config.Default()
	cfg.WAAutoReply = false
	cfg.WATimeout = 5
	engine := agent.New(cfg, llm.New(llmServer.URL, "", "qa-test", 5), diag.New(5), nil,
		session.New(session.DefaultConfig()), memory.New(memory.Config{}), learning.New())
	s := &Server{
		cfg:       cfg,
		billing:   billing.New(billingServer.URL, "qa-token"),
		engine:    engine,
		wa:        wa.New(gatewayServer.URL, 5),
		mem:       memory.New(memory.Config{}),
		csUnknown: newCSUnknownState(),
		teams:     observability.NewTeamCollector(),
	}
	s.syncDirectory()
	h.h = s.routes()
	return h
}

func (h *qaWebhookHarness) post(t *testing.T, number, message string) wa.Reply {
	t.Helper()
	body, err := json.Marshal(wa.InboundMessage{
		MessageID: "qa-" + number, ChatID: number, Sender: number, Message: message, Type: "text",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "http://noc.test/api/wa/webhook", bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("webhook status = %d: %s", w.Code, w.Body.String())
	}
	var out wa.Reply
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("decode webhook response: %v; body=%s", err, w.Body.String())
	}
	return out
}

func (h *qaWebhookHarness) llmCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.llm
}

func (h *qaWebhookHarness) waSendCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.waSend
}

func (h *qaWebhookHarness) nonGETBillingCalls() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.billingNonGET
}

func (h *qaWebhookHarness) sawKnownCustomer() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.knownCustomerSet
}
