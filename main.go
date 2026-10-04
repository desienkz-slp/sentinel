// ai-noc-go — NOC Sentinel berbasis AI (Go).
//
// Satu binary: REST API + dashboard web + agen diagnostik (OODA loop) yang
// memakai LLM OpenAI-compatible (9Router/OpenAI/Ollama) dan dapat mengeskalasi
// analisis ke Codex CLI.
//
// Build:  go build -o bin/ai-noc-go.exe .
// Run:    ./bin/ai-noc-go.exe            (default http://127.0.0.1:8090)
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/billing"
	"ainoc/internal/cache"
	"ainoc/internal/capability"
	"ainoc/internal/codexbridge"
	"ainoc/internal/config"
	"ainoc/internal/customtool"
	"ainoc/internal/db"
	"ainoc/internal/dedupe"
	"ainoc/internal/diag"
	"ainoc/internal/escalation"
	"ainoc/internal/handoff"
	"ainoc/internal/health"
	"ainoc/internal/healthcheck"
	"ainoc/internal/incident"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/memory"
	"ainoc/internal/observability"
	"ainoc/internal/policy"
	"ainoc/internal/redisx"
	"ainoc/internal/registry"
	"ainoc/internal/session"
	"ainoc/internal/supervisor"
	"ainoc/internal/tool"
	"ainoc/internal/updater"
	"ainoc/internal/uplinkpoll"
	"ainoc/internal/wa"
	"ainoc/internal/workflow"
)

// portFromURL mengambil port dari base URL gateway (fallback bila gagal).
func portFromURL(raw string, def int) int {
	u, err := url.Parse(raw)
	if err != nil {
		return def
	}
	if p := u.Port(); p != "" {
		if n, err := strconv.Atoi(p); err == nil && n > 0 {
			return n
		}
	}
	return def
}

// monitoringStorePath keeps monitoring evidence alongside the configured
// incident data. Audit is a deterministic fallback for older configurations
// that do not set an incident path.
func monitoringStorePath(cfg *config.Config) string {
	if cfg == nil {
		return "monitoring.json"
	}
	path := cfg.IncidentPath
	if path == "" {
		path = cfg.AuditPath
	}
	return filepath.Join(filepath.Dir(path), "monitoring.json")
}

func timeoutCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), d)
	return ctx, cancel
}

func main() {
	cfgPath := flag.String("config", "", "path config.json (opsional)")
	addr := flag.String("addr", "", "alamat listen, mis. :8090")
	importPGFlag := flag.Bool("import-pg", false, "salin data JSON lama (kasus, handoff) ke PostgreSQL lalu keluar")
	dryRunFlag := flag.Bool("dry-run", false, "dengan -import-pg: hanya hitung dan bandingkan, tidak menulis")
	flag.Parse()

	cfg := config.Load(*cfgPath)
	if *importPGFlag {
		exe, _ := os.Executable()
		mdir := filepath.Join(filepath.Dir(filepath.Dir(exe)), "migrations")
		if err := importPG(context.Background(), os.Stdout, cfg, db.Default(), mdir, *dryRunFlag); err != nil {
			log.Fatalf("impor gagal: %v", err)
		}
		return
	}
	if *addr != "" {
		cfg.Addr = *addr
	}

	client := llm.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTimeout)
	client.WireAPI = cfg.LLMWireAPI
	runner := diag.New(cfg.DiagTimeout)
	bridge := codexbridge.New(cfg.CodexPath, cfg.CodexModel, cfg.CodexSbx, 300)
	// Endpoint Codex override (opsional): diteruskan sebagai -c per-invoke.
	bridge.BaseURL = cfg.CodexBaseURL
	bridge.Provider = cfg.CodexProvider
	bridge.APIKey = cfg.CodexAPIKey
	bridge.WireAPI = cfg.CodexWireAPI

	// Sesi percakapan per nomor + cache jawaban.
	sesi := session.New(session.Config{
		MaxTurns: 10,
		TTL:      time.Duration(cfg.SesiTTLMin) * time.Minute,
		CacheTTL: time.Duration(cfg.CacheTTLMin) * time.Minute,
		MaxConvs: 500,
	})
	// Memory (fakta & riwayat per pengirim) — dipersist ke disk.
	mem := memory.New(memory.Config{
		Path:     cfg.MemoryPath,
		MaxInc:   50,
		MaxFacts: 20,
	})
	// Self-learning: playbook probe per kategori keluhan, dari hasil nyata.
	learn := learning.New()
	engine := agent.New(cfg, client, runner, bridge, sesi, mem, learn)

	// Endpoint B (reasoning): klien LLM kedua untuk investigasi mendalam. Pakai
	// endpoint & model Codex (terpisah dari Endpoint A) tapi lewat HTTP langsung
	// (BUKAN Codex CLI), supaya tidak butuh dependensi codex binary. Bila
	// CodexBaseURL/CodexModel tidak terisi, ReasonLLM nil = deep-dive nonaktif
	// dan sistem langsung eskalasi ke manusia (perilaku lama).
	if cfg.CodexModel != "" {
		base := cfg.CodexBaseURL
		if base == "" {
			base = cfg.LLMBaseURL // fallback: ikut endpoint A
		}
		key := cfg.CodexAPIKey
		if key == "" {
			key = cfg.LLMAPIKey
		}
		rllm := llm.New(base, key, cfg.CodexModel, cfg.LLMTimeout)
		rllm.WireAPI = cfg.CodexWireAPI
		if rllm.WireAPI == "" {
			rllm.WireAPI = cfg.LLMWireAPI
		}
		engine.ReasonLLM = rllm
	}
	// Resep "cara pengecekan" Endpoint B (persist ke disk, bertahan restart).
	engine.Recipes = learning.NewRecipeStore(filepath.Join(filepath.Dir(cfg.IncidentPath), "recipes.json"))

	// ---- Blueprint upgrade: policy, registry, workflow, incident, audit ----
	// Semua deny-by-default. Policy & registry memuat file YAML; workflow dimuat
	// dari direktori; incident & audit dipersist ke JSON (PostgreSQL menyusul).
	pol := policy.LoadMerged(cfg.PolicyPath, config.PolicyOverlayPath())
	reg := registry.LoadMerged(cfg.RegistryPath, config.RegistryOverlayPath())
	wkf, wfErr := workflow.LoadDir(cfg.WorkflowDir)
	if wfErr != nil {
		log.Printf("[workflow] gagal memuat: %v (lanjut tanpa workflow)", wfErr)
	}
	inc := incident.New(cfg.IncidentPath, 500)
	aud := audit.New(cfg.AuditPath, 2000)
	monitor, monitorErr := incident.OpenMonitoringStore(monitoringStorePath(cfg), 500)
	if monitorErr != nil {
		log.Printf("[monitoring] gagal membuka store: %v", monitorErr)
		// Audit is already initialized and is the existing safe seam for runtime
		// initialization failures. No event source is started as a fallback.
		aud.Record(audit.Entry{
			EventType:       "monitoring_store_init_failed",
			Actor:           "runtime",
			EntityType:      "monitoring_store",
			EntityID:        monitoringStorePath(cfg),
			ExecutionStatus: "FAILED",
			Error:           monitorErr.Error(),
			Note:            "monitoring projections disabled; no source adapter started",
		})
		monitor = nil
	}

	// Hubungkan incident & audit ke engine supaya tiap diagnosis otomatis
	// terdokumentasi (blueprint: "Document the incident").
	engine.Inc = inc
	engine.Aud = aud

	// Tool dispatcher: registry -> static read-only manifest -> policy -> adapter.
	// A missing or invalid release-controlled manifest remains fail-closed.
	disp := tool.New(reg, pol, 8*time.Second)
	manifest, manifestErr := capability.Load(config.ReadOnlyCapabilityManifestPath())
	if manifestErr != nil {
		log.Printf("[capability-manifest] external tools remain blocked: %v", manifestErr)
		disp.SetCapabilityManifest(capability.DenyAll())
	} else {
		disp.SetCapabilityManifest(manifest)
	}

	// Daftarkan BillingAdapter (read-only) bila endpoint + API key tersedia.
	// Tool tetap tidak aktif sampai registry.yaml menandai enabled:true.
	// (Registrasi sebenarnya dilakukan lewat syncBillingAdapter setelah srv dibuat.)
	_ = billing.New

	// Hubungkan registry + dispatcher ke agent supaya tool eksternal yang aktif
	// bisa dipresentasikan ke LLM dan dipanggil lewat gerbang keamanan.
	engine.Reg = reg
	engine.Disp = disp
	// Hubungkan workflow engine: kode deterministik yang mengontrol urutan langkah
	// diagnosis saat ada keluhan (blueprint §13).
	engine.Wkf = wkf

	// FASE 1: hubungkan Case Engine ke alur diagnosis (state machine per nomor).
	// Additive — bila tidak di-set, alur diagnosis tetap jalan tanpa case.
	// FASE 5: CaseWire kini juga mencatat setiap transisi state ke audit
	// append-only (audit trail lengkap per §25) via store yang sama.
	casew := agent.NewCaseWireWithAudit(aud)
	engine.Case = casew
	// Kasus bertahan melewati restart/update (sebelumnya hanya di memori).
	casePath := filepath.Join(filepath.Dir(cfg.IncidentPath), "cases.json")
	if err := casew.Tracker().Load(casePath); err != nil {
		log.Printf("[case] pemulihan tracker gagal: %v", err)
	}
	casew.Tracker().SetPath(casePath)

	// FASE 3: wire Policy gate untuk execute (deny-by-default).
	// ExecutionGuard = allowlist aksi (dari registry + probe diag read-only) +
	// binding approval. AI hanya mengeksekusi yang diputuskan ALLOW (LOW
	// read-only); MEDIUM+/di luar allowlist → eskalasi ke manusia, tidak pernah
	// dieksekusi otomatis. Keputusan tercatat ke audit append-only.
	var diagNames []string
	for _, t := range runner.Tools() {
		diagNames = append(diagNames, t.Function.Name)
	}
	guard := policy.NewExecutionGuard(pol, agent.ActionDefinitions(reg.All(), diagNames))
	engine.Policy = agent.NewPolicyGate(guard, reg, casew, aud)

	// FASE 4: wire Verification Engine (resolved wajib verifikasi nyata).
	// Satu-satunya jalur yang boleh menutup aksi WRITE menjadi RESOLVED:
	// EXECUTING → VERIFYING → RecordVerification → RESOLVED | FAILED→ESCALATION.
	// Read-only tidak melewati gate ini (bukti probe sudah cukup). Tanpa
	// gate, aksi write TIDAK dijalankan (fail-closed di agent.go).
	engine.Verify = agent.NewVerificationGate(casew, reg, disp, aud, nil)

	// ---- Blueprint upgrade: health, cache, dedupe, db ----
	// Semua opsional: sistem tetap jalan walau database belum menyala.
	hreg := health.New()
	hreg.Set("llm", health.StatusUnknown, "belum dicek", 0)
	hreg.Set("whatsapp", health.StatusUnknown, "belum dicek", 0)
	hreg.Set("postgres", health.StatusUnknown, "belum dicek", 0)
	hreg.Set("redis", health.StatusUnknown, "belum dicek", 0)

	// Cache dedup untuk webhook WhatsApp (cegah pesan duplikat ganda).
	dedLocal := dedupe.New(2*time.Minute, 10000)
	var rc *redisx.Client
	if dbcfg0 := db.Default(); dbcfg0.RedisPassword != "" {
		rc = redisx.New(dbcfg0.RedisAddr, dbcfg0.RedisPassword)
	}
	ded := &dedupe.Hybrid{Local: dedLocal, TTL: 2 * time.Minute}
	if rc != nil {
		ded.Remote = rc
	}
	ded.Mode = func() string { return string(config.NormalizeTeamMode(cfg.StoreRedis)) }
	// Counter observability hanya menyimpan agregat rute/status/latensi.
	obs := observability.NewCollector()
	// Cache umum (bisa dipakai nanti oleh adaptor eksternal).
	cc := cache.New(time.Duration(cfg.CacheTTLMin) * time.Minute)

	// Konfigurasi DB (PostgreSQL + Redis). Belum terkoneksi sampai infra
	// dinyalakan; kehadirannya dicatat di health registry.
	dbcfg := db.Default()
	pgs := connectPG(context.Background(), cfg, dbcfg, filepath.Join(filepath.Dir(filepath.Dir(cfg.IncidentPath)), "migrations"))
	_ = cc

	// WhatsApp Gateway di-vendor di wa-gateway/. Bila ada, Go yang mengelolanya
	// supaya cukup satu perintah start untuk seluruh aplikasi.
	var sup *supervisor.Manager
	waBase := cfg.WABaseURL
	if cfg.WADir != "" {
		port := portFromURL(waBase, 3001)
		sup = supervisor.New(cfg.WADir, port, nil, 500)
		// Pastikan klien WA menunjuk ke port proses yang kita kelola.
		waBase = sup.BaseURL()
		cfg.WABaseURL = waBase
	}
	waclient := wa.New(waBase, cfg.WATimeout)

	// Probe kesehatan nyata: cek LLM, WhatsApp, Postgres, Redis berkala.
	probe := &healthcheck.Probe{
		Reg:       hreg,
		LLM:       client,
		LLMURL:    cfg.LLMBaseURL,
		WAURL:     waBase,
		PGAddr:    dbcfg.PGHost + ":" + dbcfg.PGPort,
		RedisAddr: dbcfg.RedisAddr,
	}
	go func() {
		probe.Run(context.Background())
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for range t.C {
			probe.Run(context.Background())
		}
	}()

	srv := &Server{cfg: cfg, llm: client, diag: runner, codex: bridge, engine: engine, wa: waclient, sup: sup, sesi: sesi, mem: mem, learn: learn, pol: pol, reg: reg, wkf: wkf, inc: inc, aud: aud, monitor: monitor, hreg: hreg, ded: ded, obs: obs, teams: observability.NewTeamCollector(), disp: disp, esc: escalation.NewDedup(), ho: handoff.New(filepath.Join(filepath.Dir(cfg.IncidentPath), "handoffs.json")), csUnknown: newCSUnknownState(), customTools: customtool.NewStore(filepath.Join(filepath.Dir(cfg.IncidentPath), "customtools.json"))}
	// Pembatas tim CS (Fase 2): penolakan/penimpaan identitas tercatat ke audit
	// dan metrik. Tanpa isi pesan, nomor pelanggan, atau data akun.
	srv.pg = pgs
	srv.rds = rc
	srv.attachHandoffSink()
	srv.attachCaseSink(casew)
	engine.ScopeHook = srv.recordScopeEvent
	engine.NOCHook = srv.recordNOCToolEvent

	// Auto-update: inisialisasi checker bila owner/repo terisi. Cek awal + berkala
	// (tiap 6 jam) di latar; hasilnya di-cache untuk dashboard. Tidak pernah
	// apply sendiri — apply butuh aksi operator (POST /api/update/apply).
	if cfg.UpdateOwner != "" && cfg.UpdateRepo != "" {
		srv.upd = updater.NewChecker(cfg.UpdateOwner, cfg.UpdateRepo)
		go func() {
			cur := updater.CurrentVersion()
			log.Printf("[update] versi berjalan: %s (sumber rilis: %s/%s)", cur.Version, cfg.UpdateOwner, cfg.UpdateRepo)
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			st := srv.upd.Check(ctx)
			cancel()
			if st.Error != "" {
				log.Printf("[update] cek awal gagal: %s", st.Error)
			} else if st.UpdateAvailable {
				log.Printf("[update] versi baru tersedia: v%s", st.Latest.Version)
			}
			t := time.NewTicker(6 * time.Hour)
			defer t.Stop()
			for range t.C {
				c, cl := context.WithTimeout(context.Background(), 15*time.Second)
				srv.upd.Check(c)
				cl()
			}
		}()
	}

	// Selaraskan BillingAdapter dengan config saat ini (URL+token dari env/config).
	srv.syncBillingAdapter()
	// Selaraskan MikrotikAdapter (host+user+pass dari env/config).
	srv.syncMikrotikAdapter()
	// Selaraskan RadiusAdapter (host+API token dari env/config).
	srv.syncRadiusAdapter()
	// Selaraskan GenieACSAdapter (host NBI dari env/config).
	srv.syncGenieACSAdapter()

	// Phase 7A: start only the explicit allowlisted, read-only poll-derived
	// source. Invalid mapping or unavailable store leaves the source disabled.
	pollCtx, stopPoll := context.WithCancel(context.Background())
	if monitor != nil && len(cfg.UplinkMonitors) > 0 {
		if err := cfg.ValidateUplinkMonitors(); err != nil {
			log.Printf("[uplink-poll] disabled: invalid allowlist: %v", err)
		} else {
			readers := make(map[string]uplinkpoll.InterfaceReader, len(srv.mikrotikPool))
			for name, adapter := range srv.mikrotikPool {
				readers[name] = adapter
			}
			producer, err := uplinkpoll.New(monitor, readers, cfg.UplinkMonitors)
			if err != nil {
				log.Printf("[uplink-poll] disabled: %v", err)
			} else {
				scheduler := uplinkpoll.NewScheduler(producer, cfg.UplinkPollInterval())
				go scheduler.Run(pollCtx)
				log.Printf("[uplink-poll] enabled: %d explicit uplinks, %s interval, poll-derived only", len(cfg.UplinkMonitors), scheduler.Interval())
			}
		}
	}
	// Selaraskan executor tool custom (setelah adapter domain aktif, supaya
	// domainConf lengkap). Mendaftarkan tool custom yang sudah active ke dispatcher.
	srv.syncCustomToolExecutor()

	// Identifikasi penelepon + RBAC: bangun direktori staf & identifier.
	// Harus SETELAH syncBillingAdapter (identifier memakai billing untuk lookup
	// pelanggan). PIN berlaku 10 menit per nomor.
	srv.pinSesi = newPINStore(10 * time.Minute)
	if migrateLegacyStaff(cfg) {
		if err := cfg.Save(); err != nil {
			log.Printf("[staf] migrasi nomor NOC/Admin lama ke Direktori Staf belum tersimpan: %v", err)
		} else {
			log.Printf("[staf] nomor NOC/Admin lama dipindahkan ke Direktori Staf")
		}
	}
	srv.syncDirectory()

	// Autentikasi login dashboard (bootstrap superadmin default + cookie session).
	srv.bootstrapAuth()

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	log.Printf("ai-noc-go  |  dashboard  http://127.0.0.1%s", cfg.Addr)
	log.Printf("LLM        |  %s  (model: %s, key: %s)", cfg.LLMBaseURL, cfg.LLMModel, keyState(cfg.LLMAPIKey))
	log.Printf("Codex CLI  |  %s  (model: %s, siap: %v)", cfg.CodexPath, cfg.CodexModel, bridge.Available())
	log.Printf("WhatsApp   |  gateway %s (timeout %ds, auto-reply: %v, async: %v, blocklist: %d entri)",
		orNone(cfg.WABaseURL), cfg.WATimeout, cfg.WAAutoReply, cfg.WAAsync, len(cfg.WABlocklist))
	if sup != nil {
		log.Printf("             embedded di %s — autostart: %v", cfg.WADir, cfg.WAAutoStart)
	} else {
		log.Printf("             (mode eksternal: jalankan gateway sendiri di %s)", orNone(cfg.WABaseURL))
	}
	log.Printf("Diagnostik |  timeout %ds, maks %d langkah per sesi", cfg.DiagTimeout, cfg.MaxSteps)

	// Nyalakan gateway WA otomatis (npm install dijalankan sekali bila perlu).
	if sup != nil && cfg.WAAutoStart {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
			defer cancel()
			if err := sup.Start(ctx); err != nil {
				log.Printf("[WA] gateway gagal start otomatis: %v", err)
				return
			}
			st := sup.Status(context.Background())
			log.Printf("[WA] gateway %s di %s", st.State, st.BaseURL)
		}()
	}

	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server gagal: %v", err)
		}
	}()

	// Simpan memory berkala (setiap 2 menit) supaya pembelajaran tidak hilang
	// bila proses mati mendadak.
	go func() {
		t := time.NewTicker(2 * time.Minute)
		defer t.Stop()
		for range t.C {
			if err := mem.SaveIfDirty(); err != nil {
				log.Printf("[memory] gagal simpan: %v", err)
			}
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop
	stopPoll()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(ctx)
	// Matikan gateway WA bersama aplikasi supaya tidak ada proses menggantung.
	if sup != nil {
		log.Println("menghentikan WhatsApp Gateway…")
		_ = sup.Stop()
	}
	// Simpan memory terakhir kali sebelum keluar.
	if err := mem.SaveIfDirty(); err != nil {
		log.Printf("[memory] gagal simpan saat keluar: %v", err)
	} else {
		log.Println("memory tersimpan.")
	}
	log.Println("dihentikan.")
}

func keyState(k string) string {
	if k == "" {
		return "TIDAK ADA (set NOC_LLM_API_KEY)"
	}
	if len(k) > 8 {
		return k[:8] + "...(ok)"
	}
	return "(ok)"
}

func orNone(s string) string {
	if s == "" {
		return "(belum dikonfigurasi)"
	}
	return s
}
