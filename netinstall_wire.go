// netinstall_wire.go — endpoint pemasangan tool jaringan untuk inspeksi NOC.
//
// Kebijakan (keputusan operator): pemasangan = aksi rawan -> WAJIB persetujuan
// superadmin (dengan PIN), tidak pernah otomatis oleh Endpoint B.
// Katalog tool dibatasi allowlist tetap (internal/netinstall) supaya Endpoint B
// tidak bisa meminta paket arbitrer. Cara pakai tiap tool tersimpan di katalog
// (bertindak sebagai "skill" yang bisa dibaca).
package main

import (
	"net/http"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/directory"
	"ainoc/internal/netinstall"
)

// registerNetInstallRoutes mendaftarkan endpoint pemasangan tool jaringan.
func (s *Server) registerNetInstallRoutes(mux *http.ServeMux) {
	// GET /api/netinstall — katalog tool (nama, paket, deskripsi, cara pakai) +
	// status terpasang di server ini.
	mux.HandleFunc("/api/netinstall", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai GET"})
			return
		}
		names := netinstall.Names()
		installed := netinstall.Installed(names)
		type view struct {
			Name        string `json:"name"`
			Pkg         string `json:"pkg"`
			Category    string `json:"category"`
			Description string `json:"description"`
			Usage       string `json:"usage"`
			Installed   bool   `json:"installed"`
		}
		out := make([]view, 0, len(netinstall.Catalog))
		for _, t := range netinstall.Catalog {
			out = append(out, view{Name: t.Name, Pkg: t.Pkg, Category: t.Category,
				Description: t.Description, Usage: t.Usage, Installed: installed[t.Name]})
		}
		writeJSON(w, 200, map[string]any{"tools": out})
	})

	// POST /api/netinstall/install — superadmin (dengan PIN) memasang tool.
	// Body: {number, pin, tool}.
	mux.HandleFunc("/api/netinstall/install", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		var body struct {
			Number string `json:"number"`
			PIN    string `json:"pin"`
			Tool   string `json:"tool"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}

		// 1. Tool harus ada di allowlist.
		if !netinstall.IsAllowed(body.Tool) {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "tool tidak ada di daftar yang diizinkan"})
			return
		}

		// 2. Wajib superadmin.
		caller := s.identifyCaller(r.Context(), body.Number)
		if !caller.IsStaff || caller.Role != directory.RoleSuperAdmin {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "hanya super admin yang boleh memasang tool"})
			return
		}

		// 3. Wajib PIN (aksi rawan).
		if !s.pinVerified(caller, body.PIN) {
			writeJSON(w, 403, map[string]any{"ok": false, "error": "PIN salah atau belum diverifikasi"})
			return
		}

		// 4. Audit + eksekusi.
		s.auditNetInstall(caller.Number, body.Tool, "attempt")
		if err := netinstall.Install(body.Tool); err != nil {
			s.auditNetInstall(caller.Number, body.Tool, "failed: "+err.Error())
			writeJSON(w, 500, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		s.auditNetInstall(caller.Number, body.Tool, "ok")
		if info, ok := netinstall.Lookup(body.Tool); ok {
			writeJSON(w, 200, map[string]any{"ok": true, "tool": body.Tool, "usage": info.Usage})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "tool": body.Tool})
	})
}

// pinVerified memeriksa PIN superadmin: jendela aktif, atau verifikasi langsung.
func (s *Server) pinVerified(caller directory.Caller, pin string) bool {
	if s.pinSesi != nil && s.pinSesi.verified(caller.Number) {
		return true
	}
	m, ok := s.dir.LookupWithPIN(caller.Number)
	if !ok || !m.HasPIN() {
		return false
	}
	if !m.CheckPIN(pin) {
		return false
	}
	if s.pinSesi != nil {
		s.pinSesi.grant(caller.Number)
	}
	return true
}

func (s *Server) auditNetInstall(actor, tool, note string) {
	if s.aud == nil {
		return
	}
	s.aud.Record(audit.Entry{
		EventType:  "net_install",
		OccurredAt: time.Now().UTC(),
		Actor:      actor,
		EntityType: "network_tool",
		EntityID:   tool,
		Note:       note,
	})
}
