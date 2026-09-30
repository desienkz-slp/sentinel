package main

import (
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/codexbridge"
	"ainoc/internal/config"
	"ainoc/internal/diag"
	"ainoc/internal/llm"
	"ainoc/internal/supervisor"
	"ainoc/internal/wa"
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
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(static)))

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "service": "ai-noc-go", "time": time.Now()})
	})

	mux.HandleFunc("/api/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{
			"config":      s.cfg.Redacted(),
			"codex_ready": s.codex.Available(),
			"reports":     len(s.engine.Reports()),
		})
	})

	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]string{"error": "gunakan POST"})
			return
		}
		var body struct {
			LLMBaseURL  *string   `json:"llm_base_url"`
			LLMAPIKey   *string   `json:"llm_api_key"`
			LLMModel    *string   `json:"llm_model"`
			MaxSteps    *int      `json:"max_steps"`
			CodexModel  *string   `json:"codex_model"`
			WABaseURL   *string   `json:"wa_base_url"`
			WAAllowlist *[]string `json:"wa_allowlist"`
			WAAutoReply *bool     `json:"wa_auto_reply"`
			WAAsync     *bool     `json:"wa_async"`
			WAGroup     *bool     `json:"wa_group"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": err.Error()})
			return
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
			s.cfg.LLMModel = *body.LLMModel
			s.llm.Model = *body.LLMModel
		}
		if body.MaxSteps != nil && *body.MaxSteps > 0 {
			s.cfg.MaxSteps = *body.MaxSteps
		}
		if body.CodexModel != nil && *body.CodexModel != "" {
			s.cfg.CodexModel = *body.CodexModel
			s.codex.Model = *body.CodexModel
		}
		// WhatsApp: base URL boleh dikosongkan untuk mematikan integrasi.
		if body.WABaseURL != nil {
			s.cfg.WABaseURL = strings.TrimRight(strings.TrimSpace(*body.WABaseURL), "/")
			s.wa.BaseURL = s.cfg.WABaseURL
		}
		if body.WAAllowlist != nil {
			s.cfg.WAAllowlist = *body.WAAllowlist
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
		writeJSON(w, 200, map[string]any{"ok": true, "config": s.cfg.Redacted()})
	})

	// Verifikasi kredensial + model lewat completion nyata (bukan sekadar daftar model).
	mux.HandleFunc("/api/llm/ping", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.LLMTimeout)*time.Second)
		defer cancel()
		text, d, err := s.llm.Ping(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]any{"ok": false, "error": err.Error(), "model": s.cfg.LLMModel})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "reply": text, "model": s.cfg.LLMModel, "latency_ms": d.Milliseconds()})
	})

	mux.HandleFunc("/api/models", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := timeoutCtx(r, 30*time.Second)
		defer cancel()
		ids, err := s.llm.Models(ctx)
		if err != nil {
			writeJSON(w, 502, map[string]any{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{"count": len(ids), "models": ids})
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
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.DiagTimeout)*time.Second)
		defer cancel()
		writeJSON(w, 200, s.diag.Run(ctx, body.Tool, strings.TrimSpace(body.Target)))
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

	// Sesi diagnosis penuh dengan streaming langkah (SSE).
	mux.HandleFunc("/api/ask", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query  string `json:"query"`
			Target string `json:"target"`
			Stream bool   `json:"stream"`
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
		ctx, cancel := timeoutCtx(r, time.Duration(s.cfg.LLMTimeout*2+s.cfg.MaxSteps*s.cfg.DiagTimeout+60)*time.Second)
		defer cancel()

		if !body.Stream {
			writeJSON(w, 200, s.engine.Run(ctx, body.Query, strings.TrimSpace(body.Target)))
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
		rep := s.engine.RunWith(ctx, body.Query, strings.TrimSpace(body.Target), func(st agent.Step) {
			send("step", st)
		})
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

		// Allowlist: cegah siapa pun memicu perintah diagnostik di jaringan.
		if !wa.Allowed(s.cfg.WAAllowlist, id) {
			log.Printf("[WA] ditolak (tidak ada di allowlist): %s", id)
			writeJSON(w, 200, wa.Reply{
				Accepted:  false,
				MessageID: msg.MessageID,
				Note:      "pengirim tidak ada di allowlist",
			})
			return
		}

		log.Printf("[WA] masuk dari %s: %s", id, truncateLog(msg.Message, 120))

		// PENTING: context request DIBATALKAN begitu handler selesai. Untuk mode
		// async, diagnosis harus berjalan di atas context.Background() sendiri,
		// kalau tidak LLM langsung gagal dengan "context canceled".
		budget := time.Duration(s.cfg.LLMTimeout*2+s.cfg.MaxSteps*s.cfg.DiagTimeout+120) * time.Second
		runWith := func(parent context.Context) agent.Report {
			ctx, cancel := context.WithTimeout(parent, budget)
			defer cancel()
			return s.engine.Run(ctx, msg.Message, "")
		}

		// Mode async: balas "diterima" sekarang, hasil menyusul lewat /api/whatsapp/send.
		if s.cfg.WAAsync && s.wa.Enabled() {
			writeJSON(w, 200, wa.Reply{
				Accepted:  true,
				MessageID: msg.MessageID,
				Note:      "async: hasil akan dikirim menyusul",
			})
			go func(chatID string) {
				rep := runWith(context.Background())
				ctx, cancel := context.WithTimeout(context.Background(), time.Duration(s.cfg.WATimeout)*time.Second)
				defer cancel()
				if _, err := s.wa.Send(ctx, chatID, wa.FormatReport(toView(rep))); err != nil {
					log.Printf("[WA] gagal kirim hasil async ke %s: %v", chatID, err)
					return
				}
				log.Printf("[WA] hasil async terkirim ke %s (verdict=%s)", chatID, rep.Verdict)
			}(id)
			return
		}

		rep := runWith(r.Context())
		out := wa.Reply{
			Accepted:   true,
			MessageID:  msg.MessageID,
			Verdict:    rep.Verdict,
			Confidence: rep.Confidence,
			Engine:     rep.Engine,
			ElapsedMS:  rep.ElapsedMS,
			Report:     wa.FormatReport(toView(rep)),
		}
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

	// Proxy UI WhatsApp Gateway asli supaya semuanya satu alamat.
	// Path gateway yang sudah memakai prefix /api/whatsapp/ dan /whatsapp
	// diteruskan apa adanya agar JS-nya tidak perlu diubah.
	if s.sup != nil {
		mux.HandleFunc("/api/whatsapp/", s.proxyToGateway)
		mux.HandleFunc("/whatsapp", s.proxyToGateway)
		mux.HandleFunc("/styles.css", s.proxyToGateway)
		mux.HandleFunc("/app.js", s.proxyToGateway)
	}

	return withLogging(mux)
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
		IsChat:     strings.Contains(rep.Engine, "tanpa pengecekan"),
	}
}

func truncateLog(s string, n int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", " "), "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func withLogging(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		h.ServeHTTP(w, r)
		if !strings.HasPrefix(r.URL.Path, "/api/health") {
			log.Printf("%s %s (%s)", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
