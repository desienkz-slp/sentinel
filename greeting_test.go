package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"ainoc/internal/config"
	"ainoc/internal/directory"
)

func TestFirstMessageGreetingPrefixesOnlyEnabledFirstCustomerReply(t *testing.T) {
	cfg := &config.Config{GreetingFirstMessage: true, GreetingTemplate: "Halo {{name}} dari {{org}} ({{role}})."}
	caller := directory.Caller{Name: "Rina", Role: directory.RoleCustomer}

	if got := customerReply(cfg, caller, false, "Jawaban aman."); got != "Halo Rina dari NetLayer (customer).\n\nJawaban aman." {
		t.Fatalf("first reply = %q", got)
	}
	if got := customerReply(cfg, caller, true, "Jawaban aman."); got != "Jawaban aman." {
		t.Fatalf("continuation must not be prefixed: %q", got)
	}
	cfg.GreetingFirstMessage = false
	if got := customerReply(cfg, caller, false, "Jawaban aman."); got != "Jawaban aman." {
		t.Fatalf("disabled greeting changed reply: %q", got)
	}
}

func TestFirstMessageGreetingRendersOnlySafePlaceholdersOnce(t *testing.T) {
	cfg := &config.Config{GreetingFirstMessage: true, GreetingTemplate: "Hai {{name}}/{{org}}/{{role}}/{{unknown}}"}
	caller := directory.Caller{Name: "{{org}}", Role: directory.Role("noc_senior")}

	got := customerReply(cfg, caller, false, "Aman.")
	want := "Hai {{org}}/NetLayer/noc_senior/{{unknown}}\n\nAman."
	if got != want {
		t.Fatalf("render = %q, want %q", got, want)
	}
	if strings.Contains(got, "{{unknown}}") == false {
		t.Fatal("unknown template token must remain literal")
	}
	fallback := customerReply(&config.Config{GreetingFirstMessage: true, GreetingTemplate: "Halo {{name}}."}, directory.Caller{}, false, "Aman.")
	if fallback != "Halo Pelanggan.\n\nAman." {
		t.Fatalf("missing name fallback = %q", fallback)
	}
}

func TestFirstMessageGreetingNeverPrefixesStaffOrEmptyReplies(t *testing.T) {
	cfg := &config.Config{GreetingFirstMessage: true, GreetingTemplate: "Halo {{name}}."}
	staff := directory.Caller{Name: "Operator", Role: directory.RoleSuperAdmin, IsStaff: true}
	if got := customerReply(cfg, staff, false, "Perintah internal selesai."); got != "Perintah internal selesai." {
		t.Fatalf("staff response must not be prefixed: %q", got)
	}
	if got := customerReply(cfg, directory.Caller{}, false, ""); got != "" {
		t.Fatalf("empty reply must remain empty: %q", got)
	}
}

func TestGreetingConfigPostPersistsAndIsRedactedForRuntime(t *testing.T) {
	s, h := teamTestServer(t)
	code, out := call(t, h, http.MethodPost, "/api/config", `{"greeting_first_message":true,"greeting_template":"Halo {{name}} dari {{org}}."}`)
	if code != http.StatusOK {
		t.Fatalf("POST config = %d: %#v", code, out)
	}
	c := out["config"].(map[string]any)
	if c["greeting_first_message"] != true || c["greeting_template"] != "Halo {{name}} dari {{org}}." {
		t.Fatalf("runtime config missing greeting fields: %#v", c)
	}
	if !s.cfg.GreetingFirstMessage || s.cfg.GreetingTemplate != "Halo {{name}} dari {{org}}." {
		t.Fatalf("server config not updated: %#v", s.cfg)
	}
	b, err := os.ReadFile(s.cfg.Path())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"greeting_first_message": true`) || !strings.Contains(string(b), `"greeting_template": "Halo {{name}} dari {{org}}."`) {
		t.Fatalf("greeting config was not persisted: %s", b)
	}
}

func TestSettingsHTMLContainsGreetingControlsAndHook(t *testing.T) {
	s := &Server{cfg: &config.Config{Addr: "127.0.0.1:8090"}}
	h := s.routes()
	r := httptest.NewRequest(http.MethodGet, "http://noc.local/settings.html", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	body := w.Body.String()
	for _, want := range []string{"greetingFirstMessage", "greetingTemplate", "Greeting First Message", "greeting_first_message", "greeting_template", "{{name}}", "{{org}}", "{{role}}"} {
		if !strings.Contains(body, want) {
			t.Errorf("settings greeting control/hook missing %q", want)
		}
	}
}
