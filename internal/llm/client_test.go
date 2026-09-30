package llm

import (
	"context"
	"strings"
	"testing"
)

func TestParseSSEContent(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"role\":\"assistant\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\"Halo\"}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" dunia\"}}]}\n\n" +
		"data: [DONE]\n\n"
	m, err := parseSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if m.Content != "Halo dunia" {
		t.Errorf("content = %q", m.Content)
	}
	if len(m.ToolCalls) != 0 {
		t.Errorf("tool_calls = %d, mau 0", len(m.ToolCalls))
	}
}

// Tool call di-stream bertahap: id/nama di chunk pertama, argumen dipecah.
func TestParseSSEToolCalls(t *testing.T) {
	stream := "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"ping\",\"arguments\":\"\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"target\\\":\"}}]}}]}\n\n" +
		"data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"\\\"8.8.8.8\\\"}\"}}]}}]}\n\n" +
		"data: [DONE]\n\n"
	m, err := parseSSE(strings.NewReader(stream))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.ToolCalls) != 1 {
		t.Fatalf("tool_calls = %d, mau 1", len(m.ToolCalls))
	}
	tc := m.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "ping" {
		t.Errorf("tool call = %+v", tc)
	}
	if tc.Function.Arguments != `{"target":"8.8.8.8"}` {
		t.Errorf("arguments = %q", tc.Function.Arguments)
	}
}

func TestParseSSEError(t *testing.T) {
	stream := "data: {\"error\":{\"message\":\"rate limit exceeded\"}}\n\n"
	_, err := parseSSE(strings.NewReader(stream))
	if err == nil || !strings.Contains(err.Error(), "rate limit") {
		t.Errorf("error = %v, mau memuat pesan dari API", err)
	}
}

func TestParseJSON(t *testing.T) {
	body := `{"choices":[{"message":{"role":"assistant","content":"PONG"}}]}`
	m, err := parseJSON(strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if m.Content != "PONG" {
		t.Errorf("content = %q", m.Content)
	}
}

// Endpoint yang mengembalikan error JSON harus jadi error Go, bukan panic.
func TestChatHTTPError(t *testing.T) {
	c := New("http://127.0.0.1:1", "k", "m", 2)
	_, err := c.Chat(context.Background(), []Message{{Role: "user", Content: "hi"}}, nil)
	if err == nil {
		t.Error("mau error karena tidak ada server di port 1")
	}
}
