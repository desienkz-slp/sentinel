package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/health"
	"ainoc/internal/observability"
)

func teamTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	// Berkas sementara agar POST /api/config tidak menyentuh config nyata dan
	// Save() punya path tujuan.
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"addr":"127.0.0.1:8090"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg2 := config.Load(p)
	cfg2.OperatorToken = "operator-secret"
	s := &Server{
		cfg:   cfg2,
		hreg:  health.New(),
		aud:   audit.New("", 10),
		obs:   observability.NewCollector(),
		teams: observability.NewTeamCollector(),
	}
	return s, s.routes()
}

func call(t *testing.T, h http.Handler, method, path, body string) (int, map[string]any) {
	t.Helper()
	var rd *bytes.Reader
	if body != "" {
		rd = bytes.NewReader([]byte(body))
	} else {
		rd = bytes.NewReader(nil)
	}
	r := httptest.NewRequest(method, "http://noc.local"+path, rd)
	r.Header.Set("Authorization", "Bearer operator-secret")
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

func TestTeamMetricsDefaultOffDanKosong(t *testing.T) {
	_, h := teamTestServer(t)
	code, out := call(t, h, http.MethodGet, "/api/team/metrics", "")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	flags := out["flags"].(map[string]any)
	for k, v := range flags {
		if v != "off" {
			t.Errorf("flag %s = %v, default harus off", k, v)
		}
	}
	if m := out["metrics"].(map[string]any); m["total"].(float64) != 0 {
		t.Errorf("metrik awal harus nol: %v", m)
	}
}

func TestTeamConfigTolakNilaiTakSahDanTerimaYangSah(t *testing.T) {
	s, h := teamTestServer(t)

	// nilai tak sah -> 400 dan TIDAK mengubah apa pun
	code, out := call(t, h, http.MethodPost, "/api/config", `{"team_routing":"onn"}`)
	if code != 400 || !strings.Contains(out["error"].(string), "team_routing") {
		t.Fatalf("nilai tak sah harus 400, dapat %d %v", code, out)
	}
	if s.cfg.Teams().Routing != config.TeamOff {
		t.Fatalf("nilai tak sah tidak boleh mengubah flag: %v", s.cfg.Teams().Routing)
	}

	// campuran sah + tak sah -> semuanya ditolak (tidak sebagian diterapkan)
	code, _ = call(t, h, http.MethodPost, "/api/config", `{"team_cs_scope":"on","team_handoff":"ngawur"}`)
	if code != 400 || s.cfg.Teams().CSScope != config.TeamOff {
		t.Fatalf("permintaan campuran harus ditolak utuh: code=%d cs=%v", code, s.cfg.Teams().CSScope)
	}

	// nilai sah diterima, bertahan ke metrik
	code, _ = call(t, h, http.MethodPost, "/api/config", `{"team_routing":"shadow"}`)
	if code != 200 {
		t.Fatalf("nilai sah harus 200, dapat %d", code)
	}
	_, out = call(t, h, http.MethodGet, "/api/team/metrics", "")
	if got := out["flags"].(map[string]any)["routing"]; got != "shadow" {
		t.Fatalf("routing = %v, mau shadow", got)
	}
	// field lain tidak ikut berubah
	if got := out["flags"].(map[string]any)["cs_scope"]; got != "off" {
		t.Fatalf("cs_scope = %v, harus tetap off", got)
	}
}

func TestTeamMetricsTanpaDataPribadi(t *testing.T) {
	s, h := teamTestServer(t)
	s.teams.Record(observability.TeamDecision{Team: "noc", Handler: "cek_billing", HandledBy: "code", Tools: []string{"billing.get_customer"}, OK: true})
	r := httptest.NewRequest(http.MethodGet, "http://noc.local/api/team/metrics", nil)
	r.Header.Set("Authorization", "Bearer operator-secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	low := strings.ToLower(w.Body.String())
	for _, bad := range []string{"628", "phone", "nomor", "username", "message", "identity"} {
		if strings.Contains(low, bad) {
			t.Errorf("respons memuat %q: %s", bad, w.Body.String())
		}
	}
	if !strings.Contains(low, "billing.get_customer") {
		t.Errorf("nama tool seharusnya tercatat: %s", w.Body.String())
	}
}

func TestTeamMetricsButuhOtorisasi(t *testing.T) {
	_, h := teamTestServer(t)
	r := httptest.NewRequest(http.MethodGet, "http://noc.local/api/team/metrics", nil) // non-loopback tanpa token
	r.RemoteAddr = "203.0.113.9:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 && w.Code != 403 {
		t.Fatalf("tanpa token dari luar harus 401/403, dapat %d", w.Code)
	}
}

func TestTeamConfigNOCToolFirstValidasi(t *testing.T) {
	s, h := teamTestServer(t)
	if code, _ := call(t, h, http.MethodPost, "/api/config", `{"team_noc_toolfirst":"onn"}`); code != 400 {
		t.Fatalf("nilai ilegal harus 400, dapat %d", code)
	}
	if s.cfg.TeamNOCTools != "" {
		t.Fatal("nilai ilegal tidak boleh tersimpan")
	}
	if code, _ := call(t, h, http.MethodPost, "/api/config", `{"team_noc_toolfirst":"shadow"}`); code != 200 || s.cfg.Teams().NOCTools != config.TeamShadow {
		t.Fatalf("shadow harus diterima: %d %v", code, s.cfg.Teams().NOCTools)
	}
	_, out := call(t, h, http.MethodGet, "/api/team/metrics", "")
	if out["flags"].(map[string]any)["noc_toolfirst"] != "shadow" {
		t.Fatalf("flag harus tampil di metrik: %v", out["flags"])
	}
}
