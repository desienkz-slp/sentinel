// customtool_wire.go — wiring tool custom read-only yang diusulkan Endpoint B
// lalu disetujui superadmin.
//
// Keamanan (keputusan operator: "kode rawan -> konfirmasi superadmin dulu"):
//   - Endpoint B TIDAK pernah menulis kode; ia hanya mengusulkan Spec deklaratif
//     (domain + path GET relatif + ekstraksi field), divalidasi ketat (GET-only,
//     domain terbatas, anti-traversal). Lihat internal/customtool/spec.go.
//   - Spec tersimpan sebagai DRAFT. Hanya SUPERADMIN (dengan PIN) yang bisa
//     approve -> tool aktif.
//   - Tool aktif dieksekusi adapter GENERIK (customtool.Executor) memakai base
//     URL + auth domain yang sudah ada — bukan kode baru.
package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"ainoc/internal/adapter"
	"ainoc/internal/audit"
	"ainoc/internal/customtool"
	"ainoc/internal/directory"
)

// syncCustomToolExecutor membangun executor tool custom dari konfigurasi domain
// yang aktif, lalu mendaftarkan tool active ke dispatcher (deny-by-default tetap
// via registry: hanya tool yang TERSEDIA di registry yang bisa dipanggil — tapi
// tool custom adalah spec, bukan registry entry; executor langsung terdaftar
// sebagai adapter generic. Gerbang approval superadmin adalah satu-satunya jalan
// spec jadi aktif).
func (s *Server) syncCustomToolExecutor() {
	if s.customTools == nil {
		return
	}
	// Bangun peta domain -> config (base URL + auth) dari cfg aktif.
	domainConf := map[string]adapter.Config{}
	if s.cfg.BillingURL != "" {
		domainConf["billing"] = adapter.Config{BaseURL: s.cfg.BillingURL + "/api/noc/v1", Token: s.cfg.BillingToken, HeaderName: "X-NOC-API-Key"}
	}
	if s.cfg.RadiusURL != "" {
		domainConf["radius"] = adapter.Config{BaseURL: s.cfg.RadiusURL, Token: s.cfg.RadiusToken}
	}
	// MikroTik pakai Basic Auth + REST (port dari cfg).
	if s.cfg.MikrotikHost != "" {
		domainConf["mikrotik"] = adapter.Config{
			BaseURL:    mikrotikBaseURL(s.cfg.MikrotikHost, s.cfg.MikrotikPort, s.cfg.MikrotikTLS),
			BasicUser:  s.cfg.MikrotikUser,
			BasicPass:  s.cfg.MikrotikPass,
			InsecureTLS: s.cfg.MikrotikTLS,
		}
	}
	if s.cfg.GenieACSURL != "" {
		domainConf["genieacs"] = adapter.Config{BaseURL: s.cfg.GenieACSURL}
	}

	s.customExec = customtool.NewExecutor(domainConf)

	// Daftarkan tool active sebagai adapter generic ke dispatcher.
	for _, spec := range s.customTools.Active() {
		if ad, err := s.customExec.Resolve(spec); err == nil {
			s.disp.Register(ad)
		}
	}
}

func mikrotikBaseURL(host string, port int, tls bool) string {
	scheme := "http"
	if tls {
		scheme = "https"
	}
	if port <= 0 {
		port = 80
	}
	return scheme + "://" + host + ":" + strconv.Itoa(port) + "/rest"
}

// registerCustomToolRoutes mendaftarkan endpoint tool custom.
func (s *Server) registerCustomToolRoutes(mux *http.ServeMux) {
	// GET /api/customtool — daftar semua spec (draft/active/rejected) + statistik.
	mux.HandleFunc("/api/customtool", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai GET"})
			return
		}
		if s.customTools == nil {
			writeJSON(w, 200, map[string]any{"statistik": map[string]any{}, "tools": []customtool.Spec{}})
			return
		}
		writeJSON(w, 200, map[string]any{"statistik": s.customTools.Stats(), "tools": s.customTools.All()})
	})

	// POST /api/customtool/propose — Endpoint B mengusulkan spec (jadi draft).
	// Body: {name, domain, description, path, method, extract, parameters}.
	mux.HandleFunc("/api/customtool/propose", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		var spec customtool.Spec
		if err := decodeJSON(r, &spec); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		spec.Method = "GET" // paksa read-only
		if s.customTools == nil {
			writeJSON(w, 500, map[string]any{"ok": false, "error": "store tool custom tidak siap"})
			return
		}
		if err := s.customTools.Propose(spec, "endpoint-b"); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		_ = s.customTools.Save()
		s.auditCustomTool("propose", "endpoint-b", spec.Name, "")
		writeJSON(w, 200, map[string]any{"ok": true, "message": "tool diusulkan sebagai draft, menunggu approval superadmin"})
	})

	// POST /api/customtool/approve — SUPERADMIN (dengan PIN) menyetujui draft.
	// Body: {number, pin, name}.
	mux.HandleFunc("/api/customtool/approve", func(w http.ResponseWriter, r *http.Request) {
		s.customToolDecision(w, r, true)
	})

	// POST /api/customtool/reject — SUPERADMIN menolak draft.
	// Body: {number, pin, name, reason}.
	mux.HandleFunc("/api/customtool/reject", func(w http.ResponseWriter, r *http.Request) {
		s.customToolDecision(w, r, false)
	})
}

// customToolDecision menangani approve/reject dengan gerbang: staf superadmin
// AKTIF + PIN terverifikasi. Ini satu-satunya jalan spec draft -> aktif/ditolak.
func (s *Server) customToolDecision(w http.ResponseWriter, r *http.Request, approve bool) {
	if r.Method != http.MethodPost {
		writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
		return
	}
	var body struct {
		Number string `json:"number"`
		PIN    string `json:"pin"`
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}

	// 1. Identifikasi penelepon dari nomor.
	caller := s.identifyCaller(r.Context(), body.Number)

	// 2. Wajib superadmin.
	if !caller.IsStaff || caller.Role != directory.RoleSuperAdmin {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "hanya super admin yang boleh menyetujui/menolak tool custom"})
		return
	}

	// 3. Wajib PIN terverifikasi (aksi berisiko: mengaktifkan tool baru).
	if !s.pinVerified(caller, body.PIN) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "PIN salah atau belum diverifikasi"})
		return
	}

	// 4. Terapkan keputusan.
	var err error
	action := "approve"
	if approve {
		err = s.customTools.Approve(body.Name)
	} else {
		err = s.customTools.Reject(body.Name, body.Reason)
		action = "reject"
	}
	if err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	_ = s.customTools.Save()
	s.auditCustomTool(action, caller.Number, body.Name, body.Reason)

	// Setelah approve, daftarkan adapter ke dispatcher supaya langsung aktif.
	if approve {
		if spec, ok := s.customTools.Get(body.Name); ok {
			if ad, err := s.customExec.Resolve(spec); err == nil {
				s.disp.Register(ad)
			}
		}
	}
	writeJSON(w, 200, map[string]any{"ok": true, "action": action, "name": body.Name})
}

func (s *Server) auditCustomTool(event, actor, name, note string) {
	if s.aud == nil {
		return
	}
	s.aud.Record(audit.Entry{
		EventType:  "custom_tool_" + event,
		OccurredAt: time.Now().UTC(),
		Actor:      actor,
		EntityType: "custom_tool",
		EntityID:   name,
		Note:       note,
	})
}

var _ = json.Marshal
