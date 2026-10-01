// Package llm adalah klien OpenAI-compatible (9Router / OpenAI / Ollama / vLLM)
// dengan dukungan streaming SSE dan function-calling.
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Function struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Tool struct {
	Type     string   `json:"type"`
	Function Function `json:"function"`
}

type Client struct {
	BaseURL string
	APIKey  string
	Model   string
	// WireAPI memilih format komunikasi native model:
	//   "chat"      -> /chat/completions (OpenAI-compatible; default)
	//   "responses" -> /responses (OpenAI Responses; untuk model GPT/reasoning)
	//   "messages"  -> /messages (Anthropic Messages; untuk model Claude)
	WireAPI string
	HTTP    *http.Client
}

func New(baseURL, apiKey, model string, timeoutSec int) *Client {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		Model:   model,
		WireAPI: "chat",
		HTTP:    &http.Client{Timeout: time.Duration(timeoutSec) * time.Second},
	}
}

// wire menormalkan WireAPI ke salah satu dari chat/responses/messages.
func (c *Client) wire() string {
	switch strings.ToLower(strings.TrimSpace(c.WireAPI)) {
	case "responses":
		return "responses"
	case "messages", "anthropic":
		return "messages"
	default:
		return "chat"
	}
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Tools    []Tool    `json:"tools,omitempty"`
	Stream   bool      `json:"stream"`
}

// Chat mengirim percakapan dan mengembalikan satu pesan assistant (final atau berisi tool_calls).
func (c *Client) Chat(ctx context.Context, msgs []Message, tools []Tool) (*Message, error) {
	switch c.wire() {
	case "responses":
		return c.chatResponses(ctx, msgs, tools)
	case "messages":
		return c.chatMessages(ctx, msgs, tools)
	default:
		return c.chatCompletions(ctx, msgs, tools)
	}
}

func (c *Client) chatCompletions(ctx context.Context, msgs []Message, tools []Tool) (*Message, error) {
	payload := chatRequest{Model: c.Model, Messages: msgs, Tools: tools, Stream: true}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	ct := resp.Header.Get("Content-Type")
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	if strings.Contains(ct, "event-stream") {
		return parseSSE(resp.Body)
	}
	return parseJSON(resp.Body)
}

// chatResponses memakai OpenAI Responses API (/responses) — format native untuk
// model reasoning (o-series/GPT-4o). Input digabung, reasoning effort bisa
// diatur, dan output object mencakup reasoning + tool calls.
func (c *Client) chatResponses(ctx context.Context, msgs []Message, tools []Tool) (*Message, error) {
	// Gabungkan system + history menjadi array input (string untuk text).
	var input []map[string]any
	for _, m := range msgs {
		if m.Role == "system" {
			input = append(input, map[string]any{"role": "developer", "content": m.Content})
			continue
		}
		if m.Role == "tool" {
			// tool result -> function_call_output
			input = append(input, map[string]any{
				"type": "function_call_output", "call_id": m.ToolCallID, "output": m.Content,
			})
			continue
		}
		role := m.Role
		if role == "assistant" {
			// assistant berisi tool_calls -> item function_call
			if len(m.ToolCalls) > 0 {
				for _, tc := range m.ToolCalls {
					input = append(input, map[string]any{
						"type":      "function_call",
						"name":      tc.Function.Name,
						"arguments": tc.Function.Arguments,
						"call_id":   tc.ID,
					})
				}
				continue
			}
		}
		input = append(input, map[string]any{"role": role, "content": m.Content})
	}

	var respTools []map[string]any
	for _, t := range tools {
		respTools = append(respTools, map[string]any{
			"type":        "function",
			"name":        t.Function.Name,
			"description": t.Function.Description,
			"parameters":  t.Function.Parameters,
		})
	}

	payload := map[string]any{"model": c.Model, "input": input}
	if len(respTools) > 0 {
		payload["tools"] = respTools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/responses", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return parseResponses(resp.Body)
}

// chatMessages memakai Anthropic Messages API (/messages) — format native untuk
// model Claude. system dipisah, thinking & tool_use dalam blok content.
func (c *Client) chatMessages(ctx context.Context, msgs []Message, tools []Tool) (*Message, error) {
	var system strings.Builder
	var messages []map[string]any
	for _, m := range msgs {
		switch m.Role {
		case "system":
			system.WriteString(m.Content)
			system.WriteString("\n")
		case "tool":
			messages = append(messages, map[string]any{
				"role": "user",
				"content": []map[string]any{{
					"type": "tool_result", "tool_use_id": m.ToolCallID, "content": m.Content,
				}},
			})
		case "assistant":
			content := []map[string]any{}
			if m.Content != "" {
				content = append(content, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				content = append(content, map[string]any{
					"type": "tool_use", "id": tc.ID, "name": tc.Function.Name,
					"input": parseJSONAny(tc.Function.Arguments),
				})
			}
			messages = append(messages, map[string]any{"role": "assistant", "content": content})
		default:
			messages = append(messages, map[string]any{"role": m.Role, "content": m.Content})
		}
	}

	var respTools []map[string]any
	for _, t := range tools {
		respTools = append(respTools, map[string]any{
			"name": t.Function.Name, "description": t.Function.Description,
			"input_schema": t.Function.Parameters,
		})
	}

	payload := map[string]any{"model": c.Model, "max_tokens": 4096, "messages": messages}
	if sys := strings.TrimSpace(system.String()); sys != "" {
		payload["system"] = sys
	}
	if len(respTools) > 0 {
		payload["tools"] = respTools
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/messages", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("x-api-key", c.APIKey)
	}
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("llm http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	return parseAnthropic(resp.Body)
}

func parseJSON(r io.Reader) (*Message, error) {
	var out struct {
		Choices []struct {
			Message Message `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != nil && out.Error.Message != "" {
		return nil, fmt.Errorf("llm error: %s", out.Error.Message)
	}
	if len(out.Choices) == 0 {
		return nil, fmt.Errorf("llm: tidak ada choice pada respons")
	}
	m := out.Choices[0].Message
	if m.Role == "" {
		m.Role = "assistant"
	}
	return &m, nil
}

type sseDelta struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

func parseSSE(r io.Reader) (*Message, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var content strings.Builder
	acc := map[int]*ToolCall{}
	var order []int
	var apiErr string

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, ":") || strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta sseDelta `json:"delta"`
			} `json:"choices"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if chunk.Error != nil && chunk.Error.Message != "" {
			apiErr = chunk.Error.Message
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		d := chunk.Choices[0].Delta
		content.WriteString(d.Content)
		for _, tc := range d.ToolCalls {
			cur, ok := acc[tc.Index]
			if !ok {
				cur = &ToolCall{Type: "function"}
				acc[tc.Index] = cur
				order = append(order, tc.Index)
			}
			if tc.ID != "" {
				cur.ID = tc.ID
			}
			if tc.Type != "" {
				cur.Type = tc.Type
			}
			if tc.Function.Name != "" {
				cur.Function.Name = tc.Function.Name
			}
			cur.Function.Arguments += tc.Function.Arguments
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if apiErr != "" {
		return nil, fmt.Errorf("llm error: %s", apiErr)
	}

	m := &Message{Role: "assistant", Content: content.String()}
	for i, idx := range order {
		tc := *acc[idx]
		if tc.ID == "" {
			tc.ID = fmt.Sprintf("call_%d", i)
		}
		if tc.Type == "" {
			tc.Type = "function"
		}
		m.ToolCalls = append(m.ToolCalls, tc)
	}
	return m, nil
}

// parseResponses membaca respons dari OpenAI Responses API menjadi Message.
func parseResponses(r io.Reader) (*Message, error) {
	var doc struct {
		Status string `json:"status"`
		Error  *struct {
			Message string `json:"message"`
		} `json:"error"`
		Output []struct {
			Type      string `json:"type"`
			Role      string `json:"role"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			CallID    string `json:"call_id"`
			Content   []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"output"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 16<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Error != nil && doc.Error.Message != "" {
		return nil, fmt.Errorf("llm error: %s", doc.Error.Message)
	}
	m := &Message{Role: "assistant"}
	for _, item := range doc.Output {
		if item.Type == "message" || item.Role == "assistant" {
			for _, c := range item.Content {
				if c.Type == "output_text" || c.Type == "text" {
					m.Content += c.Text
				}
			}
		} else if item.Type == "function_call" {
			tc := ToolCall{ID: item.CallID, Type: "function"}
			tc.Function.Name = item.Name
			tc.Function.Arguments = item.Arguments
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("call_%d", len(m.ToolCalls))
			}
			m.ToolCalls = append(m.ToolCalls, tc)
		}
	}
	if m.Content == "" && len(m.ToolCalls) == 0 {
		return nil, fmt.Errorf("llm: respons kosong (status=%s)", doc.Status)
	}
	return m, nil
}

// parseAnthropic membaca respons Anthropic Messages API menjadi Message.
func parseAnthropic(r io.Reader) (*Message, error) {
	var doc struct {
		Type  string `json:"type"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Content []struct {
			Type  string          `json:"type"`
			Text  string          `json:"text"`
			ID    string          `json:"id"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.NewDecoder(io.LimitReader(r, 16<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	if doc.Error != nil && doc.Error.Message != "" {
		return nil, fmt.Errorf("llm error: %s", doc.Error.Message)
	}
	m := &Message{Role: "assistant"}
	for _, c := range doc.Content {
		switch c.Type {
		case "text":
			m.Content += c.Text
		case "tool_use":
			tc := ToolCall{ID: c.ID, Type: "function"}
			tc.Function.Name = c.Name
			if len(c.Input) > 0 && string(c.Input) != "null" {
				tc.Function.Arguments = string(c.Input)
			} else {
				tc.Function.Arguments = "{}"
			}
			if tc.ID == "" {
				tc.ID = fmt.Sprintf("call_%d", len(m.ToolCalls))
			}
			m.ToolCalls = append(m.ToolCalls, tc)
		}
		// blok thinking diabaikan (tidak diteruskan ke agent)
	}
	if m.Content == "" && len(m.ToolCalls) == 0 {
		return nil, fmt.Errorf("llm: respons kosong (type=%s)", doc.Type)
	}
	return m, nil
}

// parseJSONAny mengubah string JSON menjadi any (untuk input tool_use Anthropic).
func parseJSONAny(s string) any {
	if s == "" {
		return nil
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return s // fallback: kirim string mentah
	}
	return v
}
func (c *Client) Models(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var doc struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&doc); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(doc.Data))
	for _, m := range doc.Data {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

// Ping melakukan satu completion kecil untuk verifikasi kredensial & model.
func (c *Client) Ping(ctx context.Context) (string, time.Duration, error) {
	start := time.Now()
	m, err := c.Chat(ctx, []Message{{Role: "user", Content: "Reply with exactly: PONG"}}, nil)
	if err != nil {
		return "", time.Since(start), err
	}
	return strings.TrimSpace(m.Content), time.Since(start), nil
}
