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
	"strconv"
	"syscall"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/codexbridge"
	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/incident"
	"ainoc/internal/learning"
	"ainoc/internal/llm"
	"ainoc/internal/memory"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/session"
	"ainoc/internal/supervisor"
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

func timeoutCtx(r *http.Request, d time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(r.Context(), d)
	return ctx, cancel
}

func main() {
	cfgPath := flag.String("config", "", "path config.json (opsional)")
	addr := flag.String("addr", "", "alamat listen, mis. :8090")
	flag.Parse()

	cfg := config.Load(*cfgPath)
	if *addr != "" {
		cfg.Addr = *addr
	}

	client := llm.New(cfg.LLMBaseURL, cfg.LLMAPIKey, cfg.LLMModel, cfg.LLMTimeout)
	runner := diag.New(cfg.DiagTimeout)
	bridge := codexbridge.New(cfg.CodexPath, cfg.CodexModel, cfg.CodexSbx, 300)

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

	// ---- Blueprint upgrade: policy, registry, workflow, incident, audit ----
	// Semua deny-by-default. Policy & registry memuat file YAML; workflow dimuat
	// dari direktori; incident & audit dipersist ke JSON (PostgreSQL menyusul).
	pol := policy.Load(cfg.PolicyPath)
	reg := registry.Load(cfg.RegistryPath)
	wkf, wfErr := workflow.LoadDir(cfg.WorkflowDir)
	if wfErr != nil {
		log.Printf("[workflow] gagal memuat: %v (lanjut tanpa workflow)", wfErr)
	}
	inc := incident.New(cfg.IncidentPath, 500)
	aud := audit.New(cfg.AuditPath, 2000)

	// Hubungkan incident & audit ke engine supaya tiap diagnosis otomatis
	// terdokumentasi (blueprint: "Document the incident").
	engine.Inc = inc
	engine.Aud = aud

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

	srv := &Server{cfg: cfg, llm: client, diag: runner, codex: bridge, engine: engine, wa: waclient, sup: sup, sesi: sesi, mem: mem, learn: learn, pol: pol, reg: reg, wkf: wkf, inc: inc, aud: aud}

	httpSrv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.routes(),
		ReadHeaderTimeout: 15 * time.Second,
	}

	log.Printf("ai-noc-go  |  dashboard  http://127.0.0.1%s", cfg.Addr)
	log.Printf("LLM        |  %s  (model: %s, key: %s)", cfg.LLMBaseURL, cfg.LLMModel, keyState(cfg.LLMAPIKey))
	log.Printf("Codex CLI  |  %s  (model: %s, siap: %v)", cfg.CodexPath, cfg.CodexModel, bridge.Available())
	log.Printf("WhatsApp   |  gateway %s (timeout %ds, auto-reply: %v, async: %v, allowlist: %d entri)",
		orNone(cfg.WABaseURL), cfg.WATimeout, cfg.WAAutoReply, cfg.WAAsync, len(cfg.WAAllowlist))
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
