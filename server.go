package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/billing"
	"ainoc/internal/codexbridge"
	"ainoc/internal/config"
	"ainoc/internal/correlation"
	"ainoc/internal/dedupe"
	"ainoc/internal/diag"
	"ainoc/internal/directory"
	"ainoc/internal/escalation"
	"ainoc/internal/handoff"
	"ainoc/internal/genieacs"
	"ainoc/internal/health"
	"ainoc/internal/incident"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/memory"
	"ainoc/internal/mikrotik"
	"ainoc/internal/observability"
	"ainoc/internal/policy"
	"ainoc/internal/radius"
	"ainoc/internal/registry"
	"ainoc/internal/router"
	"ainoc/internal/security"
	"ainoc/internal/session"
	"ainoc/internal/standard"
	"ainoc/internal/supervisor"
	"ainoc/internal/tool"
	"ainoc/internal/updater"
	"ainoc/internal/wa"
	"ainoc/internal/workflow"
)

//go:embed web/*
var webFS embed.FS

type Server struct {
	cfg    *config.Config
	llm    *llm.Client
	diag   *diag.Runner
	codex  *codexbridge.Bridge
	engine *agent.Engine
	wa     *wa.Client
	sup    *supervisor.Manager
	sesi   *session.Store
	mem    *memory.Store
	learn  *learning.Store

	// Blueprint upgrade: policy, registry, workflow, incident, audit.
	pol *policy.Engine
	reg *registry.Registry
	wkf *workflow.Registry
	inc *incident.Store
	aud *audit.Store

	// Blueprint upgrade: health, dedupe.
	hreg  *health.Registry
	ded   *dedupe.Store
	obs   *observability.Collector
	teams *observability.TeamCollector // metrik keputusan tim CS/NOC (tanpa data pribadi)

	// Blueprint upgrade: tool dispatcher (registry -> policy -> adapter).
	disp *tool.Dispatcher

	// Billing adapter (NETORA /api/noc/v1) — nil bila belum dikonfigurasi.
	billing *billing.Adapter

	// MikroTik adapter — tunggal (kompatibilitas mundur) untuk endpoint lama.
	mikrotik *mikrotik.Adapter

	// MikroTik pool — daftar router untuk fitur multi-MikroTik (key = nama).
	mikrotikPool map[string]*mikrotik.Adapter

	// Radius adapter (NETORA Radius UI, HTTP REST) — nil bila belum dikonfigurasi.
	radius *radius.Adapter

	// GenieACS adapter (TR-069 NBI) — nil bila belum dikonfigurasi.
	genieacs *genieacs.Adapter

	// Dedup eskalasi fase 2: satu case hanya mengirim handoff ke NOC/Admin
	// satu kali (anti spam).
	esc *escalation.Dedup

	// ho = buku serah-terima CS<->NOC (Fase 4). nil = fitur mati.
	ho *handoff.Ledger

	// upd memeriksa rilis GitHub (cek otomatis + apply 1-klik). nil = fitur mati.
	upd *updater.Checker

	// dir = direktori staf; idf = identifikasi penelepon (staf/pelanggan) + RBAC.
	dir *directory.Directory
	idf *directory.Identifier
	// pinSesi = verifikasi PIN sementara per nomor untuk aksi berisiko via WA.
	pinSesi *pinStore
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(webFS, "web")
	staticHandler := http.FileServer(http.FS(static))
	mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// HTML di-embed ke binary dan berubah saat upgrade. Larang browser/proxy
		// mempertahankan halaman versi lama setelah service berganti binary.
		if r.URL.Path == "/" || strings.HasSuffix(strings.ToLower(r.URL.Path), ".html") {
			w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
		}
		staticHandler.ServeHTTP(w, r)
	}))

	// Identifikasi penelepon + manajemen staf (RBAC).
	s.registerIdentityRoutes(mux)

	// Liveness tidak bergantung pada layanan lain: proses HTTP masih hidup.
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "service": "ai-noc-go"})
	})

	// Readiness memakai hasil probe terakhir. UNKNOWN saat boot tidak dianggap
	// gagal supaya orchestrator tidak mematikan proses sebelum probe awal selesai.
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		status := health.StatusUnknown
		if s.hreg != nil {
			status = s.hreg.Overall()
		}
		ready := status != health.StatusDegraded
		code := http.StatusOK
		if !ready {
			code = http.StatusServiceUnavailable
		}
		writeJSON(w, code, map[string]any{"ok": ready, "service": "ai-noc-go", "status": status})
	})

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		// Health endpoint mengembalikan status dependensi + agregat.
		if s.hreg != nil {
			writeJSON(w, 200, map[string]any{
				"ok":      true,
				"service": "ai-noc-go",
				"time":    time.Now(),
				"health":  s.hreg.Summary(),
			})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "service": "ai-noc-go", "time": time.Now()})
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"config":      s.cfg.Redacted(),
			"codex_ready": s.codex.Available(),
			"reports":     len(s.engine.Reports()),
		})
	})

	// Metrik HTTP dan KPI diagnosis bersifat agregat: tidak memuat query,
	// identitas pelanggan, atau isi respons sehingga aman untuk dashboard operator.
	mux.HandleFunc("/api/team/metrics", func(w http.ResponseWriter, r *http.Request) {
		f := s.cfg.Teams()
		writeJSON(w, http.StatusOK, map[string]any{
			"flags": map[string]string{
				"routing": string(f.Routing), "cs_scope": string(f.CSScope), "handoff": string(f.Handoff),
				"severity": string(f.Severity), "presenter": string(f.Presenter),
			},
			"metrics": s.teams.Snapshot(),
		})
	})

	mux.HandleFunc("/api/metrics", func(w http.ResponseWriter, r *http.Request) {
		if s.obs == nil {
			writeJSON(w, http.StatusOK, observability.Snapshot{Routes: []observability.RouteMetric{}})
			return
		}
		writeJSON(w, http.StatusOK, s.obs.Snapshot())
	})

	// KPI §40 dihitung dari case engine (tracker), bukan hardcode (FASE 5).
	mux.HandleFunc("/api/kpi", s.handleCaseKPI)

	// Lifecycle case aktif: state, events, escalation target, verification
	// result — dari case engine + audit append-only (FASE 5).
	mux.HandleFunc("/api/cases", s.handleCases)

	mux.HandleFunc("/api/alerts", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, observability.BuildAlerts(s.observabilityDependencies(), s.observabilityOutcomes()))
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var body struct {
			LLMBaseURL    *string   `json:"llm_base_url"`
			LLMAPIKey     *string   `json:"llm_api_key"`
			LLMModel      *string   `json:"llm_model"`
			LLMWireAPI    *string   `json:"llm_wire_api"`
			MaxSteps      *int      `json:"max_steps"`
			CodexModel    *string   `json:"codex_model"`
			CodexBaseURL  *string   `json:"codex_base_url"`
			CodexProvider *string   `json:"codex_provider"`
			CodexAPIKey   *string   `json:"codex_api_key"`
			CodexWireAPI  *string   `json:"codex_wire_api"`
			WABaseURL     *string   `json:"wa_base_url"`
			WABlocklist   *[]string `json:"wa_blocklist"`
			NOCNumber     *string   `json:"noc_number"`
			AdminNumber   *string   `json:"admin_number"`
			WAAutoReply   *bool     `json:"wa_auto_reply"`
			WAAsync       *bool     `json:"wa_async"`
			WAGroup       *bool     `json:"wa_group"`
			// Endpoint adaptor eksternal (URL + token; token opsional = "tidak diubah").
			BillingURL   *string `json:"billing_url"`
			BillingToken *string `json:"billing_token"`
			// Mode tim CS/NOC: off | shadow | on. Nilai lain ditolak (400).
			TeamRouting   *string `json:"team_routing"`
			TeamCSScope   *string `json:"team_cs_scope"`
			TeamHandoff   *string `json:"team_handoff"`
			TeamSeverity  *string `json:"team_severity"`
			TeamPresenter *string `json:"team_presenter"`
			RadiusURL     *string `json:"radius_url"`
			RadiusToken   *string `json:"radius_token"`
			MikrotikHost  *string `json:"mikrotik_host"`
			MikrotikPort  *int    `json:"mikrotik_port"`
			MikrotikUser  *string `json:"mikrotik_user"`
			MikrotikPass  *string `json:"mikrotik_pass"`
			MikrotikTLS   *bool   `json:"mikrotik_tls"`
			// Multi-router: daftar lengkap router (menggantikan field tunggal).
			MikrotikRouters *[]config.MikrotikRouter `json:"mikrotik_routers"`
			GenieACSURL     *string                  `json:"genieacs_url"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		// Validasi mode tim SEBELUM mengubah apa pun: nilai tak sah = 400, bukan
		// diam-diam jadi off/on (salah ketik tak boleh mengubah perilaku produksi).
		for name, p := range map[string]*string{
			"team_routing": body.TeamRouting, "team_cs_scope": body.TeamCSScope,
			"team_handoff": body.TeamHandoff, "team_severity": body.TeamSeverity,
			"team_presenter": body.TeamPresenter,
		} {
			if p == nil {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(*p)) {
			case "off", "shadow", "on":
			default:
				writeJSON(w, 400, map[string]string{"error": name + " harus off, shadow, atau on"})
				return
			}
		}
		if body.TeamRouting != nil {
			s.cfg.TeamRouting = string(config.NormalizeTeamMode(*body.TeamRouting))
		}
		if body.TeamCSScope != nil {
			s.cfg.TeamCSScope = string(config.NormalizeTeamMode(*body.TeamCSScope))
		}
		if body.TeamHandoff != nil {
			s.cfg.TeamHandoff = string(config.NormalizeTeamMode(*body.TeamHandoff))
		}
		if body.TeamSeverity != nil {
			s.cfg.TeamSeverity = string(config.NormalizeTeamMode(*body.TeamSeverity))
		}
		if body.TeamPresenter != nil {
			s.cfg.TeamPresenter = string(config.NormalizeTeamMode(*body.TeamPresenter))
		}
		if body.LLMBaseURL != nil && *body.LLMBaseURL != "" {
			s.cfg.LLMBaseURL = strings.TrimRight(*body.LLMBaseURL, "/")
			s.llm.BaseURL = s.cfg.LLMBaseURL
		}
		if body.LLMAPIKey != nil && *body.LLMAPIKey != "" {
			s.cfg.LLMAPIKey = *body.LLMAPIKey
			s.llm.APIKey = *body.LLMAPIKey
		}
		if body.LLMModel != nil && *body.LLMModel != "" {
			// Ganti model = buang cache, supaya jawaban dari model lama tidak
			// disajikan ulang (standar konteks tetap, tapi kualitas jawaban bisa beda).
			if *body.LLMModel != s.cfg.LLMModel {
				s.sesi.CacheClearAll()
				log.Printf("[standar] model diganti ke %s — cache dikosongkan", *body.LLMModel)
			}
			s.cfg.LLMModel = *body.LLMModel
			s.llm.Model = *body.LLMModel
		}
		if body.LLMWireAPI != nil {
			s.cfg.LLMWireAPI = strings.TrimSpace(*body.LLMWireAPI)
			s.llm.WireAPI = s.cfg.LLMWireAPI
		}
		if body.MaxSteps != nil && *body.MaxSteps > 0 {
			s.cfg.MaxSteps = *body.MaxSteps
		}
		if body.CodexModel != nil && *body.CodexModel != "" {
			s.cfg.CodexModel = *body.CodexModel
			s.codex.Model = *body.CodexModel
		}
		if body.CodexBaseURL != nil {
			s.cfg.CodexBaseURL = strings.TrimRight(strings.TrimSpace(*body.CodexBaseURL), "/")
			s.codex.BaseURL = s.cfg.CodexBaseURL
		}
		if body.CodexProvider != nil {
			s.cfg.CodexProvider = strings.TrimSpace(*body.CodexProvider)
			s.codex.Provider = s.cfg.CodexProvider
		}
		if body.CodexAPIKey != nil {
			// Kosong = hapus key (kembali ke auth.json/config.toml).
			s.cfg.CodexAPIKey = strings.TrimSpace(*body.CodexAPIKey)
			s.codex.APIKey = s.cfg.CodexAPIKey
		}
		if body.CodexWireAPI != nil {
			s.cfg.CodexWireAPI = strings.TrimSpace(*body.CodexWireAPI)
			s.codex.WireAPI = s.cfg.CodexWireAPI
		}
		// WhatsApp: base URL boleh dikosongkan untuk mematikan integrasi.
		if body.WABaseURL != nil {
			s.cfg.WABaseURL = strings.TrimRight(strings.TrimSpace(*body.WABaseURL), "/")
			s.wa.BaseURL = s.cfg.WABaseURL
		}
		if body.WABlocklist != nil {
			s.cfg.WABlocklist = *body.WABlocklist
		}
		if body.NOCNumber != nil {
			s.cfg.NOCNumber = strings.TrimSpace(*body.NOCNumber)
		}
		if body.AdminNumber != nil {
			s.cfg.AdminNumber = strings.TrimSpace(*body.AdminNumber)
		}
		if body.WAAutoReply != nil {
			s.cfg.WAAutoReply = *body.WAAutoReply
		}
		if body.WAAsync != nil {
			s.cfg.WAAsync = *body.WAAsync
		}
		if body.WAGroup != nil {
			s.cfg.WAGroup = *body.WAGroup
		}
		// ---- Endpoint adaptor eksternal (URL + token). ----
		// Token kosong = "jangan diubah"; URL boleh dikosongkan untuk menonaktifkan.
		if body.BillingURL != nil {
			s.cfg.BillingURL = strings.TrimRight(strings.TrimSpace(*body.BillingURL), "/")
		}
		if body.BillingToken != nil {
			s.cfg.BillingToken = strings.TrimSpace(*body.BillingToken)
		}
		if body.RadiusURL != nil {
			s.cfg.RadiusURL = strings.TrimRight(strings.TrimSpace(*body.RadiusURL), "/")
		}
		if body.RadiusToken != nil {
			s.cfg.RadiusToken = strings.TrimSpace(*body.RadiusToken)
		}
		if body.MikrotikHost != nil {
			s.cfg.MikrotikHost = strings.TrimSpace(*body.MikrotikHost)
		}
		if body.MikrotikPort != nil {
			s.cfg.MikrotikPort = *body.MikrotikPort
		}
		if body.MikrotikUser != nil {
			s.cfg.MikrotikUser = strings.TrimSpace(*body.MikrotikUser)
		}
		if body.MikrotikPass != nil {
			s.cfg.MikrotikPass = *body.MikrotikPass
		}
		if body.MikrotikTLS != nil {
			s.cfg.MikrotikTLS = *body.MikrotikTLS
		}
		// Multi-router: daftar lengkap menggantikan field tunggal.
		if body.MikrotikRouters != nil {
			// Pertahankan password lama bila entri kirim pass kosong
			// (UI mengosongkan field password = "jangan ubah").
			old := map[string]string{} // key = host
			for _, r := range s.cfg.Routers() {
				if r.Pass != "" {
					old[r.Host] = r.Pass
				}
			}
			merged := make([]config.MikrotikRouter, 0, len(*body.MikrotikRouters))
			for _, r := range *body.MikrotikRouters {
				if r.Pass == "" {
					if p, ok := old[r.Host]; ok {
						r.Pass = p
					}
				}
				merged = append(merged, r)
			}
			s.cfg.MikrotikRouters = merged
			// Sinkronkan field tunggal ke router pertama (kompatibilitas).
			if len(s.cfg.MikrotikRouters) > 0 {
				r0 := s.cfg.MikrotikRouters[0]
				s.cfg.MikrotikHost = r0.Host
				s.cfg.MikrotikPort = r0.Port
				s.cfg.MikrotikUser = r0.User
				s.cfg.MikrotikPass = r0.Pass
				s.cfg.MikrotikTLS = r0.TLS
			}
		}
		if body.GenieACSURL != nil {
			s.cfg.GenieACSURL = strings.TrimRight(strings.TrimSpace(*body.GenieACSURL), "/")
		}
		// Setelah URL/token berubah, daftarkan ulang adapter bila lengkap.
		s.syncBillingAdapter()
		s.syncMikrotikAdapter()
		s.syncRadiusAdapter()
		s.syncGenieACSAdapter()
		// Tulis ke disk. Tanpa ini pengaturan hanya hidup di memori dan hilang
		// saat aplikasi di-restart — tombol "Simpan" jadi tidak ada artinya.
		if err := s.cfg.Save(); err != nil {
			log.Printf("[config] gagal menyimpan ke disk: %v", err)
			writeJSON(w, 500, map[string]any{
				"error":  "pengaturan berubah di memori tetapi GAGAL ditulis ke disk: " + err.Error(),
				"config": s.cfg.Redacted(),
			})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "saved_to": s.cfg.Path(), "config": s.cfg.Redacted()})
	})

	// Verifikasi kredensial + model lewat completion nyata (bukan sekadar daftar model).
	// Menerima body opsional {base_url,api_key,model,wire_api} untuk menguji nilai
	// yang baru diketik di kolom UI SEBELUM disimpan (agar kolom langsung berpengaruh).
	mux.HandleFunc("/api/llm/ping", func(w http.ResponseWriter, r *http.Request) {
		base, key, model, wire := s.cfg.LLMBaseURL, s.cfg.LLMAPIKey, s.cfg.LLMModel, s.cfg.LLMWireAPI
		if r.Method == http.MethodPost && r.Body != nil {
			var body struct {
				BaseURL *string `json:"base_url"`
				APIKey  *string `json:"api_key"`
				Model   *string `json:"model"`
				WireAPI *string `json:"wire_api"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				if body.BaseURL != nil && *body.BaseURL != "" {
					base = strings.TrimRight(*body.BaseURL, "/")
				}
				if body.APIKey != nil {
					key = *body.APIKey
				}
				if body.Model != nil && *body.Model != "" {
					model = *body.Model
				}
				if body.WireAPI != nil && *body.WireAPI != "" {
					wire = strings.TrimSpace(*body.WireAPI)
				}
			}
		}
		c := llm.New(base, key, model, s.cfg.LLMTimeout)
		c.WireAPI = wire
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.LLMTimeout)*time.Second)
		defer cancel()
		text, d, err := c.Ping(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "model": model})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "reply": text, "model": model, "latency_ms": d.Milliseconds()})
	})

	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, 30*time.Second)
		defer cancel()
		// endpoint=a (Conversation/LLM) | b (Reasoning/Codex). Default = a.
		ep := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("endpoint")))
		base, key := s.cfg.LLMBaseURL, s.cfg.LLMAPIKey
		if ep == "b" || ep == "reasoning" || ep == "codex" {
			base, key = s.cfg.CodexBaseURL, s.cfg.CodexAPIKey
			// Fallback: endpoint Codex kosong = pakai endpoint A (9Router sama).
			if base == "" {
				base = s.cfg.LLMBaseURL
			}
			if key == "" {
				key = s.cfg.LLMAPIKey
			}
		}
		// POST body {base_url,api_key} = uji nilai kolom sebelum disimpan.
		if r.Method == http.MethodPost && r.Body != nil {
			var body struct {
				BaseURL *string `json:"base_url"`
				APIKey  *string `json:"api_key"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				if body.BaseURL != nil && *body.BaseURL != "" {
					base = strings.TrimRight(*body.BaseURL, "/")
				}
				if body.APIKey != nil {
					key = *body.APIKey
				}
			}
		}
		ids, err := listModelsHTTP(ctx, base, key)
		if err != nil {
			writeJSON(w, 502, map[string]any{"error": err.Error(), "endpoint": ep})
			return
		}
		writeJSON(w, 200, map[string]any{"count": len(ids), "models": ids, "endpoint": ep})
	})

	// Probe tunggal (tombol manual di dashboard).
	mux.HandleFunc("/api/diag", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var body struct {
			Tool   string `json:"tool"`
			Target string `json:"target"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		body.Target = strings.TrimSpace(body.Target)
		if body.Tool != "interface" && body.Tool != "system" && !diag.AllowedBy(body.Target, s.cfg.DiagAllowlist) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "target diagnostik tidak ada dalam allowlist operator"})
			return
		}
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.DiagTimeout)*time.Second)
		defer cancel()
		writeJSON(w, 200, s.diag.Run(ctx, body.Tool, body.Target))
	})

	mux.HandleFunc("/api/tools", func(w http.ResponseWriter, r *http.Request) {
		type t struct {
			Name string `json:"name"`
			Desc string `json:"description"`
		}
		var out []t
		for _, tool := range s.diag.Tools() {
			out = append(out, t{Name: tool.Function.Name, Desc: tool.Function.Description})
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/reports", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.engine.Reports())
	})

	// Kondisi sesi percakapan (per nomor) + cache.
	mux.HandleFunc("/api/sesi", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"statistik":     s.sesi.Stats(),
			"percakapan":    s.sesi.Conversations(),
			"standar":       standard.Version,
			"standar_aktif": s.cfg.StandardDoc != "",
		})
	})

	// Memory: apa yang sistem ketahui tentang tiap pengirim.
	mux.HandleFunc("/api/memory", func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimSpace(r.URL.Query().Get("nomor"))
		if key != "" {
			prof := s.mem.Profile(session.Key(key))
			if prof == nil {
				writeJSON(w, 404, map[string]string{"error": "nomor belum dikenal"})
				return
			}
			writeJSON(w, 200, prof)
			return
		}
		type ringkas struct {
			Nomor    string `json:"nomor"`
			Fakta    int    `json:"fakta"`
			Insiden  int    `json:"insiden"`
			Terakhir string `json:"terakhir"`
		}
		var out []ringkas
		for _, k := range s.mem.Keys() {
			if p := s.mem.Profile(k); p != nil {
				out = append(out, ringkas{Nomor: k, Fakta: len(p.Facts), Insiden: len(p.Incidents),
					Terakhir: p.LastSeen.Format("02 Jan 15:04")})
			}
		}
		writeJSON(w, 200, map[string]any{
			"statistik": s.mem.Stats(),
			"pengirim":  out,
			"path":      s.cfg.MemoryPath,
		})
	})

	// Self-learning: apa yang dipelajari sistem dari hasil diagnosis.
	mux.HandleFunc("/api/learning", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"statistik": s.learn.Stats(),
			"signature": s.learn.Semua(),
		})
	})

	// ---- Blueprint upgrade: policy, registry, workflow, incident, audit ----

	// Ringkasan komponen blueprint (untuk dashboard).
	mux.HandleFunc("/api/blueprint", func(w http.ResponseWriter, r *http.Request) {
		totalTools, activeTools := s.reg.Count()
		writeJSON(w, 200, map[string]any{
			"mode":          s.pol.Mode(),
			"tools":         map[string]int{"total": totalTools, "aktif": activeTools},
			"workflow":      s.wkf.Names(),
			"incident":      s.inc.Len(),
			"audit":         s.aud.Len(),
			"policy_path":   s.cfg.PolicyPath,
			"registry_path": s.cfg.RegistryPath,
		})
	})

	// Evaluasi kebijakan untuk satu usulan tindakan (dry, tidak mengeksekusi).
	mux.HandleFunc("/api/policy/decide", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var req policy.Request
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		res := s.pol.Decide(req)
		// Catat ke audit (keputusan kebijakan adalah tindakan penting).
		s.aud.Record(audit.Entry{
			EventType:  "policy_decision",
			Actor:      "policy",
			EntityType: "tool",
			EntityID:   req.Tool,
			After:      map[string]any{"decision": res.Decision, "rule_id": res.RuleID},
			Note:       "evaluasi kebijakan (tidak mengeksekusi apa pun)",
		})
		writeJSON(w, 200, res)
	})

	// Daftar tool terdaftar (aktif + nonaktif) untuk dashboard.
	mux.HandleFunc("/api/registry", func(w http.ResponseWriter, r *http.Request) {
		aktif := s.reg.Enabled()
		if aktif == nil {
			aktif = []registry.Tool{} // jangan kirim null ke JSON
		}
		writeJSON(w, 200, map[string]any{
			"semua": s.reg.All(),
			"aktif": aktif,
		})
	})

	// Verifikasi koneksi billing (health + uji lookup kering).
	// Dipakai operator untuk memastikan host + API key benar.
	mux.HandleFunc("/api/billing/check", func(w http.ResponseWriter, r *http.Request) {
		if s.billing == nil {
			writeJSON(w, 200, map[string]any{
				"ok":      false,
				"error":   "billing belum dikonfigurasi — isi Host + API Key di Pengaturan",
				"message": "billing belum dikonfigurasi",
			})
			return
		}
		ctx, cancel := timeoutCtx(r, 20*time.Second)
		defer cancel()
		d, err := s.billing.Ping(ctx)
		if err != nil {
			writeJSON(w, 200, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":              true,
			"base_url":        d.BaseURL,
			"ping_ms":         d.LatencyMS,
			"total_customers": d.TotalCustomers,
			"message":         fmt.Sprintf("billing menjawab (total pelanggan: %d)", d.TotalCustomers),
		})
	})

	// Verifikasi koneksi MikroTik (GET /system/resource) untuk dashboard.
	mux.HandleFunc("/api/mikrotik/check", func(w http.ResponseWriter, r *http.Request) {
		// Pilih router: ?name=<nama> atau default (router pertama).
		target := s.mikrotik
		if name := r.URL.Query().Get("name"); name != "" && s.mikrotikPool != nil {
			if ad, ok := s.mikrotikPool[name]; ok {
				target = ad
			}
		}
		if target == nil {
			writeJSON(w, 200, map[string]any{
				"ok":      false,
				"error":   "mikrotik belum dikonfigurasi — isi Host + Username + Password di Pengaturan",
				"message": "mikrotik belum dikonfigurasi",
			})
			return
		}
		ctx, cancel := timeoutCtx(r, 20*time.Second)
		defer cancel()
		d, err := target.Ping(ctx)
		if err != nil {
			writeJSON(w, 200, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":         true,
			"router":     target.RouterName(),
			"base_url":   d.BaseURL,
			"ping_ms":    d.LatencyMS,
			"version":    d.Version,
			"board_name": d.BoardName,
			"uptime":     d.Uptime,
			"cpu_load":   d.CPU,
			"identity":   d.Identity,
			"message":    fmt.Sprintf("mikrotik menjawab (RouterOS v%s, %s)", d.Version, d.BoardName),
		})
	})

	// Daftar router MikroTik terkonfigurasi (untuk fitur multi-router).
	mux.HandleFunc("/api/mikrotik/routers", func(w http.ResponseWriter, r *http.Request) {
		rs := s.cfg.Routers()
		out := make([]map[string]any, 0, len(rs))
		for _, rr := range rs {
			out = append(out, map[string]any{
				"name":        rr.Name,
				"host":        rr.Host,
				"port":        rr.Port,
				"user":        rr.User,
				"tls":         rr.TLS,
				"pass_masked": maskSecret(rr.Pass),
			})
		}
		writeJSON(w, 200, map[string]any{
			"routers": out,
			"count":   len(out),
		})
	})

	// Verifikasi koneksi Radius UI (GET /api/system/stats) untuk dashboard.
	mux.HandleFunc("/api/radius/check", func(w http.ResponseWriter, r *http.Request) {
		if s.radius == nil {
			writeJSON(w, 200, map[string]any{
				"ok":      false,
				"error":   "radius belum dikonfigurasi — isi Host + API Token di Pengaturan",
				"message": "radius belum dikonfigurasi",
			})
			return
		}
		ctx, cancel := timeoutCtx(r, 20*time.Second)
		defer cancel()
		d, err := s.radius.Ping(ctx)
		if err != nil {
			writeJSON(w, 200, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":                  true,
			"base_url":            d.BaseURL,
			"ping_ms":             d.LatencyMS,
			"cpu_load":            d.CPULoad,
			"memory_used_percent": d.MemUsed,
			"user_count":          d.UserCount,
			"db_size_mb":          d.DBSizeMB,
			"message":             fmt.Sprintf("radius menjawab (cpu %s%%, %d user)", d.CPULoad, d.UserCount),
		})
	})

	// Verifikasi koneksi GenieACS (GET /devices) untuk dashboard.
	mux.HandleFunc("/api/genieacs/check", func(w http.ResponseWriter, r *http.Request) {
		if s.genieacs == nil {
			writeJSON(w, 200, map[string]any{
				"ok":      false,
				"error":   "genieacs belum dikonfigurasi — isi Host NBI di Pengaturan",
				"message": "genieacs belum dikonfigurasi",
			})
			return
		}
		ctx, cancel := timeoutCtx(r, 20*time.Second)
		defer cancel()
		d, err := s.genieacs.Ping(ctx)
		if err != nil {
			writeJSON(w, 200, map[string]any{
				"ok":    false,
				"error": err.Error(),
			})
			return
		}
		writeJSON(w, 200, map[string]any{
			"ok":             true,
			"base_url":       d.BaseURL,
			"ping_ms":        d.LatencyMS,
			"total_devices":  d.TotalDevices,
			"online_devices": d.OnlineDevices,
			"message":        fmt.Sprintf("genieacs menjawab (%d device, %d online)", d.TotalDevices, d.OnlineDevices),
		})
	})

	// ---- Auto-update (cek rilis GitHub + apply 1-klik) ----
	// GET /api/version — versi biner yang sedang berjalan (selalu tersedia).
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, updater.CurrentVersion())
	})

	// GET /api/update/check — status update terakhir. ?force=1 memicu cek baru
	// ke GitHub (default memakai cache agar tidak kena rate-limit).
	mux.HandleFunc("/api/update/check", func(w http.ResponseWriter, r *http.Request) {
		if s.upd == nil {
			writeJSON(w, 200, map[string]any{
				"ok":      false,
				"error":   "fitur update tidak aktif (owner/repo kosong)",
				"current": updater.CurrentVersion(),
			})
			return
		}
		if r.URL.Query().Get("force") == "1" {
			ctx, cancel := timeoutCtx(r, 15*time.Second)
			defer cancel()
			st := s.upd.Check(ctx)
			writeJSON(w, 200, st)
			return
		}
		writeJSON(w, 200, s.upd.Last())
	})

	// POST /api/update/apply — jalankan skrip update detached untuk versi yang
	// tersedia. Butuh aksi operator eksplisit (master spec: no silent change).
	// Aman: hanya loopback/trusted (dilindungi security guard di luar mux), dan
	// menolak bila tidak ada update atau skrip tidak ada.
	mux.HandleFunc("/api/update/apply", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		if s.upd == nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "fitur update tidak aktif"})
			return
		}
		st := s.upd.Last()
		if st.CheckedAt.IsZero() {
			ctx, cancel := timeoutCtx(r, 15*time.Second)
			defer cancel()
			st = s.upd.Check(ctx)
		}
		if !st.UpdateAvailable || st.Latest == nil {
			writeJSON(w, 200, map[string]any{
				"ok":      false,
				"error":   "tidak ada update tersedia",
				"current": st.Current,
			})
			return
		}
		script := strings.TrimSpace(s.cfg.UpdateScript)
		if script == "" {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "update_script tidak dikonfigurasi"})
			return
		}
		if _, err := os.Stat(script); err != nil {
			writeJSON(w, 200, map[string]any{
				"ok":    false,
				"error": fmt.Sprintf("skrip update tidak ditemukan: %s", script),
			})
			return
		}
		version := st.Latest.Version
		if err := launchUpdate(script, version); err != nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		log.Printf("[update] apply dipicu operator: versi %s via %s", version, script)
		writeJSON(w, 200, map[string]any{
			"ok":      true,
			"message": fmt.Sprintf("update ke v%s dimulai; layanan akan restart otomatis", version),
			"version": version,
		})
	})

	// Riwayat insiden terstruktur.
	mux.HandleFunc("/api/incidents", func(w http.ResponseWriter, r *http.Request) {
		identity := strings.TrimSpace(r.URL.Query().Get("nomor"))
		if identity != "" {
			writeJSON(w, 200, s.inc.ForIdentity(identity))
			return
		}
		writeJSON(w, 200, s.inc.Recent(100))
	})

	// Jejak audit (append-only).
	mux.HandleFunc("/api/audit", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.aud.Recent(100))
	})

	// Daftar adapter tool yang terdaftar (semua deny-by-default sampai endpoint
	// + kredensial nyata diberikan).
	mux.HandleFunc("/api/tool/adapters", func(w http.ResponseWriter, r *http.Request) {
		if s.disp == nil {
			writeJSON(w, 200, map[string]any{"adapters": []string{}, "note": "tidak ada adapter terdaftar"})
			return
		}
		writeJSON(w, 200, map[string]any{"adapters": s.disp.Adapters()})
	})

	// Uji korelasi bukti lintas sistem (dry, tanpa memanggil sistem eksternal).
	mux.HandleFunc("/api/correlate", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		set := correlation.NewSet()
		for domain, raw := range body {
			set.Add(domain, raw)
		}
		required := []string{"billing", "radius", "mikrotik", "genieacs"}
		writeJSON(w, 200, map[string]any{
			"conclusion": set.Correlate(required...),
			"evidence":   set.Summary(),
		})
	})

	// Simpan memory ke disk sekarang (tanpa menunggu berkala).
	mux.HandleFunc("/api/memory/simpan", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		if err := s.mem.Save(); err != nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "path": s.cfg.MemoryPath, "statistik": s.mem.Stats()})
	})

	// Kosongkan cache (mis. setelah mengganti model supaya tidak menyajikan
	// jawaban dari model lama).
	mux.HandleFunc("/api/sesi/cache", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		s.sesi.CacheClearAll()
		writeJSON(w, 200, map[string]any{"ok": true, "statistik": s.sesi.Stats()})
	})

	// Sesi diagnosis penuh dengan streaming langkah (SSE).
	mux.HandleFunc("/api/ask", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query    string `json:"query"`
			Target   string `json:"target"`
			Stream   bool   `json:"stream"`
			Identity string `json:"identity"` // nomor pengirim; kosong = "dashboard"
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		body.Query = strings.TrimSpace(body.Query)
		if body.Query == "" {
			writeJSON(w, 400, map[string]string{"error": "query kosong"})
			return
		}
		// Konteks & cache dipisahkan per pengirim. Uji dari dashboard memakai
		// identitas sendiri supaya tidak mencampuri percakapan pelanggan.
		identity := strings.TrimSpace(body.Identity)
		if identity == "" {
			identity = "dashboard"
		}
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.LLMTimeout*2+s.cfg.MaxSteps*s.cfg.DiagTimeout+60)*time.Second)
		defer cancel()

		if !body.Stream {
			rep := s.engine.Run(ctx, identity, body.Query, strings.TrimSpace(body.Target))
			s.deliverEscalation(ctx, rep, identity, escalation.ClassifyDomain(body.Query))
			writeJSON(w, 200, rep)
			return
		}

		flusher, ok := w.(http.Flusher)
		if !ok {
			writeJSON(w, 500, map[string]string{"error": "streaming tidak didukung"})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		enc := json.NewEncoder(w)
		send := func(event string, v any) {
			_, _ = w.Write([]byte("event: " + event + "\n"))
			_, _ = w.Write([]byte("data: "))
			_ = enc.Encode(v)
			_, _ = w.Write([]byte("\n"))
			flusher.Flush()
		}
		rep := s.engine.RunWith(ctx, identity, body.Query, strings.TrimSpace(body.Target), func(st agent.Step) {
			send("step", st)
		})
		s.deliverEscalation(ctx, rep, identity, escalation.ClassifyDomain(body.Query))
		send("report", rep)
	})

	// Eskalasi manual / tugas coding ke Codex CLI.
	mux.HandleFunc("/api/codex", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var body struct {
			Task string `json:"task"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		if strings.TrimSpace(body.Task) == "" {
			writeJSON(w, 400, map[string]string{"error": "task kosong"})
			return
		}
		ctx, cancel := timeoutCtx(r, 10*time.Minute)
		defer cancel()
		writeJSON(w, 200, s.codex.Run(ctx, body.Task))
	})

	// ---- WhatsApp Gateway (Node/Baileys) ----

	// Webhook masuk: gateway POST payload pesan WhatsApp ke sini.
	// Bila respons memuat "reply", gateway otomatis mengirimkannya ke WhatsApp.
	mux.HandleFunc("/api/wa/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var msg wa.InboundMessage
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		msg.Message = strings.TrimSpace(msg.Message)
		id := msg.Identity()
		if msg.Message == "" {
			writeJSON(w, 200, wa.Reply{Accepted: false, MessageID: msg.MessageID, Note: "pesan kosong"})
			return
		}

		// Dedup: pesan yang sama dari nomor sama dalam 2 menit dianggap ganda
		// (webhook dobel, reconnect, pelanggan kirim dua kali) — proses hanya
		// sekali supaya tidak membuat insiden/balasan ganda. (blueprint §30)
		if s.ded != nil {
			fp := dedupe.Fingerprint("wa", id, msg.Message)
			if s.ded.Seen(fp) {
				log.Printf("[WA] duplikat diabaikan: %s (%s)", id, truncateLog(msg.Message, 40))
				writeJSON(w, 200, wa.Reply{
					Accepted:  false,
					MessageID: msg.MessageID,
					Note:      "duplikat (pesan sama baru saja diproses)",
				})
				return
			}
		}

		// Pesan dari grup WhatsApp diabaikan secara bawaan. Ini disaring di sini,
		// bukan di gateway, supaya gateway tetap bisa memakai pesan grup untuk
		// keperluan lain dan filter bisa diubah tanpa restart.
		if wa.IsGroup(msg.ChatID, msg.Sender) && !s.cfg.WAGroup {
			log.Printf("[WA] diabaikan (pesan grup): %s", id)
			writeJSON(w, 200, wa.Reply{
				Accepted:  false,
				MessageID: msg.MessageID,
				Note:      "pesan grup diabaikan (aktifkan lewat pengaturan bila perlu)",
			})
			return
		}

		// Pengecualian blocklist: pesan dari NOMOR GATEWAY SENDIRI (operator
		// mengetik/menguji dari nomor yang tertaut) selalu diproses.
		dariNomorSendiri := false
		if st, err := s.wa.Status(r.Context()); err == nil {
			if nomor, ok := st["phone"].(string); ok && nomor != "" {
				dariNomorSendiri = session.Key(nomor) == session.Key(id)
			}
		}

		// Blocklist: nomor yang diblokir TIDAK dibalas. Daftar kosong =
		// tidak ada yang diblokir (semua nomor dibalas).
		if !dariNomorSendiri && wa.Blocked(s.cfg.WABlocklist, id) {
			log.Printf("[WA] ditolak (nomor diblokir): %s", id)
			writeJSON(w, 200, wa.Reply{
				Accepted:  false,
				MessageID: msg.MessageID,
				Note:      "nomor pengirim diblokir",
			})
			return
		}

		log.Printf("[WA] masuk dari %s: %s", id, truncateLog(msg.Message, 120))

		// Identifikasi penelepon (master spec §6): cari di direktori staf lalu
		// billing. Hasilnya dilog + diselipkan ke context agar agent/eskalasi tahu
		// siapa & perannya tanpa harus bertanya "siapa Anda".
		idCtx, idCancel := context.WithTimeout(r.Context(), 12*time.Second)
		caller := s.identifyCaller(idCtx, id)
		idCancel()
		if caller.IsStaff {
			log.Printf("[WA] penelepon dikenali: %s (%s / %s)", caller.Name, directory.Role(caller.Role).Label(), caller.Role)
		} else if caller.IsCustomer && caller.Customer != nil {
			log.Printf("[WA] penelepon pelanggan: %s (status=%s)", caller.Customer.Name, caller.Customer.Status)
		} else {
			log.Printf("[WA] penelepon belum dikenal: %s (dilayani sebagai pelanggan)", id)
		}

		// Staf internal (NOC Senior/Admin/Super Admin) bukan pelanggan: perintah
		// cek ("cek user pppoe X", "cek traffic") dijalankan KODE lewat gerbang
		// otorisasi + dispatcher, tanpa LLM, lalu dibalas dengan data nyata.
		if caller.IsStaff {
			cmdCtx, cmdCancel := context.WithTimeout(r.Context(), 40*time.Second)
			cmdStart := time.Now()
			text, handled := s.handleStaffCommand(cmdCtx, caller, session.Key(id), msg.Message)
			cmdCancel()
			if handled {
				dec := router.Route(caller, msg.Message)
				s.teams.Record(observability.TeamDecision{Team: string(dec.Team), Handler: string(dec.Handler), HandledBy: "code",
					Latency: time.Since(cmdStart), OK: true, Mode: string(s.routerMode())})
				s.compareRouter(caller, msg.Message, pathCode)
			}
			if handled {
				log.Printf("[WA] perintah staf %s (%s) ditangani kode", caller.Name, caller.Role)
				out := wa.Reply{Accepted: true, MessageID: msg.MessageID, Engine: "staff-command", Report: text}
				if s.cfg.WAAutoReply {
					out.Reply = text
				}
				writeJSON(w, 200, out)
				return
			}
		}

		// PENTING: context request DIBATALKAN begitu handler selesai. Untuk mode
		// async, diagnosis harus berjalan di atas context.Background() sendiri,
		// kalau tidak LLM langsung gagal dengan "context canceled".
		budget := time.Duration(s.cfg.LLMTimeout*2+s.cfg.MaxSteps*s.cfg.DiagTimeout+120) * time.Second
		runWith := func(parent context.Context) agent.Report {
			ctx, cancel := context.WithTimeout(withCaller(parent, caller), budget)
			defer cancel()
			return s.engine.Run(ctx, id, msg.Message, "")
		}

		// Mode async: balas "diterima" sekarang, hasil menyusul lewat /api/whatsapp/send.
		if s.cfg.WAAsync && s.wa.Enabled() {
			writeJSON(w, 200, wa.Reply{
				Accepted:  true,
				MessageID: msg.MessageID,
				Note:      "async: hasil akan dikirim menyusul",
			})
			s.compareRouter(caller, msg.Message, pathLLM)
			go func(chatID string) {
				rep := runWith(context.Background())
				s.deliverEscalation(context.Background(), rep, id, escalation.ClassifyDomain(msg.Message))
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.WATimeout)*time.Second)
				defer cancel()
				if _, err := s.wa.Send(ctx, chatID, s.presentReply(caller, wa.FormatReport(toView(rep)))); err != nil {
					log.Printf("[WA] gagal kirim hasil async ke %s: %v", chatID, err)
					return
				}
				log.Printf("[WA] hasil async terkirim ke %s (verdict=%s)", chatID, rep.Verdict)
			}(id)
			return
		}

		llmStart := time.Now()
		rep := runWith(r.Context())
		s.recordLLMDecision(caller, rep, time.Since(llmStart))
		s.compareRouter(caller, msg.Message, pathLLM)
		out := wa.Reply{
			Accepted:   true,
			MessageID:  msg.MessageID,
			Verdict:    rep.Verdict,
			Confidence: rep.Confidence,
			Engine:     rep.Engine,
			ElapsedMS:  rep.ElapsedMS,
			CaseID:     rep.CaseID,
			CaseState:  rep.CaseState,
			Report:     s.presentReply(caller, wa.FormatReport(toView(rep))),
		}
		s.deliverEscalation(r.Context(), rep, id, escalation.ClassifyDomain(msg.Message))
		// Hanya isi "reply" bila auto-reply aktif; kalau tidak, operator ambil dari dashboard.
		if s.cfg.WAAutoReply {
			out.Reply = out.Report
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/wa/status", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, 15*time.Second)
		defer cancel()
		if !s.wa.Enabled() {
			writeJSON(w, 200, map[string]any{"enabled": false, "connected": false,
				"error": "NOC_WA_BASE_URL kosong"})
			return
		}
		st, err := s.wa.Status(ctx)
		if err != nil {
			writeJSON(w, 200, map[string]any{"enabled": true, "connected": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"enabled": true, "connected": s.wa.Connected(ctx), "gateway": st})
	})

	mux.HandleFunc("/api/wa/qr", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, 20*time.Second)
		defer cancel()
		out, err := s.wa.QR(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/wa/connect", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, 20*time.Second)
		defer cancel()
		out, err := s.wa.Connect(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/wa/logout", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, 30*time.Second)
		defer cancel()
		out, err := s.wa.Logout(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, out)
	})

	mux.HandleFunc("/api/wa/reconnect", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		ctx, cancel := timeoutCtx(r, 30*time.Second)
		defer cancel()
		out, err := s.wa.Reconnect(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, out)
	})

	// Kirim notifikasi/balasan manual ke WhatsApp.
	mux.HandleFunc("/api/wa/send", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var body struct {
			To      string `json:"to"`
			Message string `json:"message"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
		}
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.WATimeout)*time.Second)
		defer cancel()
		res, err := s.wa.Send(ctx, body.To, body.Message)
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "gateway": res})
	})

	// ---- Kontrol proses WhatsApp Gateway (embedded) ----

	mux.HandleFunc("/api/wa/gateway", func(w http.ResponseWriter, r *http.Request) {
		if s.sup == nil {
			writeJSON(w, 200, supervisor.Status{State: supervisor.StateDisabled})
			return
		}
		ctx, cancel := timeoutCtx(r, 15*time.Second)
		defer cancel()
		writeJSON(w, 200, s.sup.Status(ctx))
	})

	waAction := func(action string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
				return
			}
			if s.sup == nil {
				writeJSON(w, 409, map[string]string{"error": "folder wa-gateway tidak ditemukan; set NOC_WA_DIR"})
				return
			}
			ctx, cancel := timeoutCtx(r, 12*time.Minute)
			defer cancel()
			var err error
			var hasil string
			switch action {
			case "start":
				hasil, err = s.sup.StartAman(ctx)
			case "stop":
				err = s.sup.Stop()
				hasil = "dihentikan"
			case "restart":
				err = s.sup.Restart(ctx)
				hasil = "restart"
			}
			st := s.sup.Status(ctx)
			if err != nil {
				writeJSON(w, 409, map[string]any{"ok": false, "error": err.Error(), "hasil": hasil, "status": st})
				return
			}
			writeJSON(w, 200, map[string]any{"ok": true, "action": action, "hasil": hasil, "status": st})
		}
	}
	mux.HandleFunc("/api/wa/start", waAction("start"))
	mux.HandleFunc("/api/wa/stop", waAction("stop"))
	mux.HandleFunc("/api/wa/restart", waAction("restart"))

	// Proxy API gateway Node.js. Hanya endpoint /api/whatsapp/* yang perlu
	// diteruskan; UI gateway sendiri tidak lagi dipakai karena semua kontrol
	// WhatsApp sudah menyatu di dashboard aplikasi ini.
	if s.sup != nil {
		mux.HandleFunc("/api/whatsapp/", s.proxyToGateway)
	}

	guard := security.New(s.cfg.Addr, s.cfg.OperatorToken, s.cfg.WebhookToken)
	guard.SetTrustedCIDRs(s.cfg.TrustedCIDRs)
	operator := guard.Operator(mux)
	webhook := guard.Webhook(mux)
	protected := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/wa/webhook" {
			webhook.ServeHTTP(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			operator.ServeHTTP(w, r)
			return
		}
		mux.ServeHTTP(w, r)
	})
	return withObservability(protected, s.obs, s.aud)
}

func (s *Server) observabilityOutcomes() []observability.CaseOutcome {
	if s.engine == nil {
		return []observability.CaseOutcome{}
	}
	reports := s.engine.Reports()
	out := make([]observability.CaseOutcome, 0, len(reports))
	for _, report := range reports {
		out = append(out, observability.CaseOutcome{
			ElapsedMS: report.ElapsedMS,
			Escalated: report.Escalated,
			Error:     report.Error,
			Verdict:   report.Verdict,
		})
	}
	return out
}

func (s *Server) observabilityDependencies() []observability.Dependency {
	if s.hreg == nil {
		return []observability.Dependency{}
	}
	deps := s.hreg.All()
	out := make([]observability.Dependency, 0, len(deps))
	for _, dep := range deps {
		out = append(out, observability.Dependency{Name: dep.Name, Status: string(dep.Status), Detail: dep.Detail})
	}
	return out
}

// proxyToGateway meneruskan request ke proses Node.js (WhatsApp Gateway).
func (s *Server) proxyToGateway(w http.ResponseWriter, r *http.Request) {
	if s.sup == nil {
		http.Error(w, "gateway tidak aktif", http.StatusServiceUnavailable)
		return
	}
	target := s.sup.BaseURL() + r.URL.RequestURI()
	req, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	for k, vv := range r.Header {
		for _, v := range vv {
			req.Header.Add(k, v)
		}
	}
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		// Sesi belum siap / proses belum jalan -> beri pesan yang jelas ke UI.
		http.Error(w, "gateway WhatsApp belum berjalan: "+err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// toView mengubah Report agen menjadi data minimal untuk formatter WhatsApp.
func toView(rep agent.Report) wa.ReportView {
	return wa.ReportView{
		Target:     rep.Target,
		Verdict:    rep.Verdict,
		Confidence: rep.Confidence,
		Engine:     rep.Engine,
		ElapsedMS:  rep.ElapsedMS,
		Escalated:  rep.Escalated,
		Answer:     rep.Answer,
		Balasan:    rep.Balasan,
		IsChat:     strings.Contains(rep.Engine, "tanpa pengecekan"),
	}
}

// syncBillingAdapter menyelaraskan adapter billing dengan cfg saat ini.
// Dipanggil saat startup (dari main) dan setiap kali /api/config menyimpan
// perubahan endpoint. Bila URL+token lengkap, adapter dibuat & didaftarkan ke
// dispatcher; bila tidak, adapter lama dilepas (tool tetap deny-by-default).
func (s *Server) syncBillingAdapter() {
	if s.cfg.BillingURL != "" && s.cfg.BillingToken != "" {
		if s.billing != nil {
			s.disp.Unregister(s.billing) // ganti endpoint lama
		}
		s.billing = billing.New(s.cfg.BillingURL, s.cfg.BillingToken)
		s.disp.Register(s.billing)
		log.Printf("[billing] adapter NETORA terdaftar: %s/api/noc/v1 (read-only)", s.cfg.BillingURL)
	} else {
		if s.billing != nil {
			s.disp.Unregister(s.billing)
			log.Printf("[billing] adapter NETORA dilepas (host/API key dikosongkan)")
		}
		s.billing = nil
	}
}

// syncMikrotikAdapter menyelaraskan adapter MikroTik dengan cfg saat ini.
// Mendukung MULTI router: membangun pool semua router dari cfg.Routers(),
// mendaftarkan tiap adapter ke dispatcher. Adapter tunggal (s.mikrotik) dipertahankan
// sebagai referensi router pertama untuk kompatibilitas endpoint lama.
func (s *Server) syncMikrotikAdapter() {
	// Lepas semua adapter lama dari dispatcher.
	if s.mikrotik != nil {
		s.disp.Unregister(s.mikrotik)
	}
	for _, a := range s.mikrotikPool {
		s.disp.Unregister(a)
	}

	routers := s.cfg.Routers()
	s.mikrotikPool = make(map[string]*mikrotik.Adapter, len(routers))
	s.mikrotik = nil

	for i, r := range routers {
		ad := mikrotik.New(mikrotik.Config{
			Name: r.Name,
			Host: r.Host,
			Port: r.Port,
			User: r.User,
			Pass: r.Pass,
			TLS:  r.TLS,
		})
		s.disp.Register(ad)
		key := r.Name
		if key == "" {
			key = r.Host
		}
		s.mikrotikPool[key] = ad
		if i == 0 {
			s.mikrotik = ad // router pertama = default
		}
		log.Printf("[mikrotik] router terdaftar: %s (%s:%d, read-only)", r.Name, r.Host, r.Port)
	}

	if len(routers) == 0 {
		log.Printf("[mikrotik] adapter RouterOS dilepas (tidak ada router terkonfigurasi)")
	}
}

// syncRadiusAdapter menyelaraskan adapter Radius UI dengan cfg saat ini.
// Dipanggil saat startup dan setiap /api/config menyimpan perubahan. Bila
// URL+token lengkap, adapter dibuat & didaftarkan (tool deny-by-default sampai
// registry enabled:true); bila tidak, adapter lama dilepas.
func (s *Server) syncRadiusAdapter() {
	if s.cfg.RadiusURL != "" && s.cfg.RadiusToken != "" {
		if s.radius != nil {
			s.disp.Unregister(s.radius)
		}
		s.radius = radius.New(s.cfg.RadiusURL, s.cfg.RadiusToken)
		s.disp.Register(s.radius)
		log.Printf("[radius] adapter Radius UI terdaftar: %s (read-only, Bearer)", s.cfg.RadiusURL)
	} else {
		if s.radius != nil {
			s.disp.Unregister(s.radius)
			log.Printf("[radius] adapter Radius UI dilepas (host/API token dikosongkan)")
		}
		s.radius = nil
	}
}

// syncGenieACSAdapter menyelaraskan adapter GenieACS dengan cfg saat ini.
// Bila URL NBI diisi, adapter dibuat & didaftarkan (tool deny-by-default).
func (s *Server) syncGenieACSAdapter() {
	if s.cfg.GenieACSURL != "" {
		if s.genieacs != nil {
			s.disp.Unregister(s.genieacs)
		}
		s.genieacs = genieacs.New(s.cfg.GenieACSURL, "")
		s.disp.Register(s.genieacs)
		log.Printf("[genieacs] adapter NBI terdaftar: %s (read-only)", s.cfg.GenieACSURL)
	} else {
		if s.genieacs != nil {
			s.disp.Unregister(s.genieacs)
			log.Printf("[genieacs] adapter NBI dilepas (host dikosongkan)")
		}
		s.genieacs = nil
	}
}

// launchUpdate menjalankan skrip update secara DETACHED untuk versi target.
// Skrip self-update memakai fitur Bash (pipefail/BASH_SOURCE), sehingga wajib
// dijalankan dengan bash pada semua OS. /bin/sh di Debian/Ubuntu adalah dash
// dan akan gagal sebelum proses update dimulai.
func launchUpdate(script, version string) error {
	bash, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("bash tidak ditemukan untuk menjalankan skrip update: %w", err)
	}

	// Di host systemd, jalankan updater sebagai transient unit terpisah. Child
	// biasa tetap berada di cgroup noc-sentinel dan ikut dibunuh saat skrip
	// me-restart service, sehingga health-check/rollback tidak sempat selesai.
	if systemdRun, lookErr := exec.LookPath("systemd-run"); lookErr == nil {
		unit := fmt.Sprintf("noc-sentinel-update-%d", time.Now().UnixNano())
		cmd := exec.Command(systemdRun, "--unit="+unit, "--collect", "--no-block", bash, script, version)
		cmd.Stdin = nil
		if out, runErr := cmd.CombinedOutput(); runErr != nil {
			return fmt.Errorf("gagal memulai transient update unit: %w (%s)", runErr, truncateLog(string(out), 300))
		}
		return nil
	}

	// Fallback untuk host tanpa systemd (mis. Windows/git-bash).
	cmd := exec.Command(bash, script, version)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("gagal memulai skrip update: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

func truncateLog(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// listModelsHTTP meminta daftar model dari endpoint OpenAI-compatible sembarang
// (dipakai /api/models?endpoint=a|b agar Conversation & Reasoning punya daftar
// model sendiri-sendiri).
func listModelsHTTP(ctx context.Context, baseURL, apiKey string) ([]string, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("base URL kosong — isi endpoint dulu")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/models", nil)
	if err != nil {
		return nil, err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
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
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

// maskSecret mengembalikan bentuk aman secret (••••abcd). Dipakai handler
// /api/mikrotik/routers supaya password router tidak bocor ke browser.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "••••"
	}
	return "••••" + s[len(s)-4:]
}

type observabilityResponseWriter struct {
	http.ResponseWriter
	status int
}

func (w *observabilityResponseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *observabilityResponseWriter) Write(body []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(body)
}

func withObservability(h http.Handler, collector *observability.Collector, aud *audit.Store) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &observabilityResponseWriter{ResponseWriter: w}
		h.ServeHTTP(wrapped, r)
		status := wrapped.status
		if status == 0 {
			status = http.StatusOK
		}
		duration := time.Since(start)
		collector.RecordHTTP(r.Method, r.URL.Path, status, duration)
		if aud != nil && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/api/audit" {
			aud.Record(audit.Entry{
				EventType:       "http_request",
				Actor:           "operator_api",
				EntityType:      "http_route",
				EntityID:        r.Method + " " + r.URL.Path,
				ExecutionStatus: http.StatusText(status),
				After: map[string]any{
					"status":      status,
					"duration_ms": duration.Milliseconds(),
				},
				Note: "akses API dicatat tanpa isi request atau query string",
			})
		}
		if !strings.HasPrefix(r.URL.Path, "/api/health") {
			log.Printf("%s %s status=%d (%s)", r.Method, r.URL.Path, status, duration.Round(time.Millisecond))
		}
	})
}
