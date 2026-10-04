package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ainoc/internal/config"
)

func TestLLMPingEndpointSelection(t *testing.T) {
	for _, tc := range []struct{ name, endpoint, body, bKey, bBase, wantKey, wantModel, wantPath string }{
		{"default A", "", `{}`, "saved-b", "b", "saved-a", "model-a", "/chat/completions"},
		{"empty POST remains A", "", ``, "saved-b", "", "saved-a", "model-a", "/chat/completions"},
		{"B model and wire fallback", "", `{"endpoint":"b"}`, "", "", "saved-a", "model-a", "/chat/completions"},
		{"saved B body", "", `{"endpoint":"b"}`, "saved-b", "b", "saved-b", "model-b", "/responses"},
		{"saved B query", "?endpoint=b", `{}`, "saved-b", "b", "saved-b", "model-b", "/responses"},
		{"reasoning alias", "?endpoint=reasoning", `{}`, "saved-b", "b", "saved-b", "model-b", "/responses"},
		{"codex alias", "", `{"endpoint":"codex"}`, "saved-b", "b", "saved-b", "model-b", "/responses"},
		{"explicit override", "", `{"endpoint":"b","api_key":"typed","model":"typed-model","wire_api":"chat"}`, "saved-b", "b", "typed", "typed-model", "/chat/completions"},
		{"explicit empty key", "", `{"endpoint":"b","api_key":""}`, "saved-b", "b", "", "model-b", "/responses"},
		{"no A key to different B", "", `{"endpoint":"b"}`, "", "b", "", "model-b", "/responses"},
		{"shared A fallback", "", `{"endpoint":"b"}`, "", "", "saved-a", "model-b", "/responses"},
		{"changed URL no implicit key", "", `{"endpoint":"b","base_url":"OVERRIDE"}`, "saved-b", "b", "", "model-b", "/responses"},
		{"changed URL explicit key", "", `{"endpoint":"b","base_url":"OVERRIDE","api_key":"typed"}`, "saved-b", "b", "typed", "model-b", "/responses"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seen := 0
			var wantHost string
			upstream := func(w http.ResponseWriter, r *http.Request) {
				seen++
				if "http://"+r.Host != wantHost {
					t.Errorf("target = %q, want %q", r.Host, wantHost)
				}
				wantAuth := ""
				if tc.wantKey != "" {
					wantAuth = "Bearer " + tc.wantKey
				}
				if got := r.Header.Get("Authorization"); got != wantAuth {
					t.Errorf("auth = %q, want %q", got, wantAuth)
				}
				if r.URL.Path != tc.wantPath {
					t.Errorf("path = %q, want %q", r.URL.Path, tc.wantPath)
				}
				var payload struct {
					Model string `json:"model"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Error(err)
					return
				}
				if payload.Model != tc.wantModel {
					t.Errorf("model = %q, want %q", payload.Model, tc.wantModel)
				}
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/responses" {
					w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"PONG"}]}]}`))
				} else {
					w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"PONG"}}]}`))
				}
			}
			a := httptest.NewServer(http.HandlerFunc(upstream))
			defer a.Close()
			b := httptest.NewServer(http.HandlerFunc(upstream))
			defer b.Close()
			override := httptest.NewServer(http.HandlerFunc(upstream))
			defer override.Close()
			cfg := &config.Config{LLMBaseURL: a.URL, LLMAPIKey: "saved-a", LLMModel: "model-a", LLMWireAPI: "chat", LLMTimeout: 2, CodexAPIKey: tc.bKey, CodexModel: "model-b", CodexWireAPI: "responses"}
			if tc.bBase != "" {
				cfg.CodexBaseURL = b.URL
			}
			if tc.name == "B model and wire fallback" {
				cfg.CodexModel, cfg.CodexWireAPI = "", ""
			}
			wantHost = b.URL
			if tc.name == "default A" || tc.bBase == "" {
				wantHost = a.URL
			}
			if bytes.Contains([]byte(tc.body), []byte("OVERRIDE")) {
				wantHost = override.URL
			}
			body := bytes.ReplaceAll([]byte(tc.body), []byte("OVERRIDE"), []byte(override.URL))
			r := httptest.NewRequest(http.MethodPost, "/api/llm/ping"+tc.endpoint, bytes.NewReader(body))
			r.RemoteAddr = "127.0.0.1:12345"
			w := httptest.NewRecorder()
			(&Server{cfg: cfg}).routes().ServeHTTP(w, r)
			if w.Code != 200 {
				t.Fatalf("status=%d: %s", w.Code, w.Body.String())
			}
			if seen != 1 {
				t.Errorf("upstream calls=%d", seen)
			}
		})
	}
}
