// settings_crud_wire.go — list + edit Skill (resep) dan Rule (policy) dari UI Settings.
//
// Read (GET) terbuka untuk yang sudah login; WRITE (edit/hapus/tambah) WAJIB
// superadmin + PIN (konsisten dgn gerbang keamanan operator). Body tulis membawa
// `number` (nomor WA superadmin) + `pin`, sama seperti customtool/netinstall.
package main

import (
	"net/http"
	"strings"

	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/learning"
	"ainoc/internal/policy"
)

// registerSettingsCRUDRoutes mendaftarkan endpoint CRUD rules (policy) & resep.
func (s *Server) registerSettingsCRUDRoutes(mux *http.ServeMux) {
	// ---- RULES (policy) ----
	mux.HandleFunc("/api/rules", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			writeJSON(w, 200, map[string]any{
				"mode":    s.pol.Mode(),
				"rules":   s.pol.Rules(),
				"path":    s.cfg.PolicyPath,
				"overlay": config.PolicyOverlayPath(),
			})
			return
		}
		if r.Method == http.MethodPost || r.Method == http.MethodPut {
			var body struct {
				Number string        `json:"number"`
				PIN    string        `json:"pin"`
				Rules  []policy.Rule `json:"rules"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if !s.superadminVerified(w, r, body.Number, body.PIN) {
				return
			}
			for i := range body.Rules {
				rr := &body.Rules[i]
				if strings.TrimSpace(rr.ID) == "" {
					writeJSON(w, 400, map[string]any{"ok": false, "error": "id rule tidak boleh kosong"})
					return
				}
				rr.Decision = strings.ToUpper(strings.TrimSpace(rr.Decision))
				switch rr.Decision {
				case "", "ALLOW", "APPROVAL_REQUIRED", "DENY", "REMOVE":
				default:
					writeJSON(w, 400, map[string]any{"ok": false, "error": "decision tidak sah pada rule " + rr.ID})
					return
				}
			}
			overlay := config.DefaultPolicyOverlayPath()
			if err := policy.WriteOverlay(overlay, body.Rules); err != nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			s.pol = policy.LoadMerged(s.cfg.PolicyPath, overlay)
			s.auditAuth("rules_update", body.Number, "rules diperbarui via UI")
			writeJSON(w, 200, map[string]any{"ok": true, "overlay": overlay})
			return
		}
		writeJSON(w, 405, map[string]any{"ok": false, "error": "GET atau POST"})
	})

	// ---- SKILL (resep) ----
	mux.HandleFunc("/api/recipes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if s.engine == nil || s.engine.Recipes == nil {
				writeJSON(w, 200, map[string]any{"statistik": map[string]any{"resep": 0, "signature": 0, "tersimpan": false}, "resep": []map[string]any{}})
				return
			}
			writeJSON(w, 200, map[string]any{
				"statistik": s.engine.Recipes.Stats(),
				"resep":     s.engine.Recipes.Semua(),
			})
			return
		}
		// DELETE /api/recipes — body {number, pin, signature, tools}.
		if r.Method == http.MethodDelete {
			var body struct {
				Number    string `json:"number"`
				PIN       string `json:"pin"`
				Signature string `json:"signature"`
				Tools     string `json:"tools"`
			}
			if err := decodeJSON(r, &body); err != nil {
				writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			if !s.superadminVerified(w, r, body.Number, body.PIN) {
				return
			}
			sig := learning.Signature(strings.TrimSpace(body.Signature))
			if sig == "" || strings.TrimSpace(body.Tools) == "" {
				writeJSON(w, 400, map[string]any{"ok": false, "error": "signature & tools wajib"})
				return
			}
			tools := strings.Split(body.Tools, "|")
			if s.engine == nil || s.engine.Recipes == nil {
				writeJSON(w, 500, map[string]any{"ok": false, "error": "store resep belum siap"})
				return
			}
			if err := s.engine.Recipes.Remove(sig, tools); err != nil {
				writeJSON(w, 404, map[string]any{"ok": false, "error": err.Error()})
				return
			}
			s.auditAuth("recipe_delete", body.Number, string(sig))
			writeJSON(w, 200, map[string]any{"ok": true})
			return
		}
		writeJSON(w, 405, map[string]any{"ok": false, "error": "GET atau DELETE"})
	})
}

// superadminVerified memastikan number adalah superadmin AKTIF + PIN valid.
func (s *Server) superadminVerified(w http.ResponseWriter, r *http.Request, number, pin string) bool {
	caller := s.identifyCaller(r.Context(), number)
	if !caller.IsStaff || caller.Role != directory.RoleSuperAdmin {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "hanya super admin"})
		return false
	}
	if !s.pinVerified(caller, pin) {
		writeJSON(w, 403, map[string]any{"ok": false, "error": "PIN salah atau belum diverifikasi"})
		return false
	}
	return true
}
