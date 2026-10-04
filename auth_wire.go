// auth_wire.go — login dashboard (cookie session) + reset via WhatsApp superadmin.
//
// Alur:
//   - Bootstrap: bila config.AuthUsers kosong, isi superadmin bawaan
//     (desienkz / 628268Matamu). Setelah itu login wajib untuk semua request
//     dashboard & API (kecuali loopback tepercaya).
//   - POST /api/auth/login {username,password} -> cookie sesi.
//   - POST /api/auth/logout -> hapus cookie.
//   - GET  /api/auth/session -> siapa yang login (atau 401).
//   - Reset password via WA: superadmin kirim "reset login <username> <passbaru>"
//     -> dijalankan KODE (bukan LLM), hanya superadmin.
package main

import (
	"net"
	"net/http"
	"strings"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/auth"
	"ainoc/internal/directory"
)

// bootstrapAuth mengisi pengguna superadmin default bila belum ada, dan
// memastikan store auth selaras dengan config (hash + salt tersimpan).
func (s *Server) bootstrapAuth() {
	salt := s.cfg.AuthSalt
	users := s.cfg.AuthUsers
	s.auth = auth.New(salt, users, 12*time.Hour)

	// Superadmin default: pastikan selalu ada di bootstrap pertama.
	if !s.auth.HasUsers() {
		s.auth.SetUser(auth.DefaultSuperUser, auth.DefaultSuperPass)
		logAuth("bootstrap superadmin default: " + auth.DefaultSuperUser)
	}

	// Tulis balik salt + hash ke config (persist, gitignored).
	s.cfg.AuthSalt = s.auth.Salt()
	s.cfg.AuthUsers = s.auth.Users()
	if err := s.cfg.Save(); err != nil {
		logAuth("gagal simpan kredensial login: " + err.Error())
	}
}

// authRequired mengembalikan middleware yang memaksa login cookie valid, kecuali
// loopback/tepercaya (keputusan operator: akses lokal tetap tanpa login supaya
// CLI/local webhook tidak pecah).
func (s *Server) authRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Endpoint auth selalu terbuka.
		p := r.URL.Path
		if p == "/api/auth/login" || p == "/api/auth/session" || p == "/api/auth/logout" || p == "/login.html" {
			next.ServeHTTP(w, r)
			return
		}
		// Tanpa pengguna terdaftar -> tidak ada login (perilaku lama).
		if s.auth == nil || !s.auth.HasUsers() {
			next.ServeHTTP(w, r)
			return
		}
		if s.auth.ValidateSession(auth.FromRequest(r)) {
			next.ServeHTTP(w, r)
			return
		}
		// Loopback/tepercaya tetap diizinkan tanpa cookie (kompatibilitas CLI).
		if s.guardTrusted(r) {
			next.ServeHTTP(w, r)
			return
		}
		if isHTMLRequest(r) {
			http.Redirect(w, r, "/login.html", http.StatusFound)
			return
		}
		writeJSON(w, 401, map[string]any{"ok": false, "error": "login required"})
	})
}

// isHTMLRequest melaporkan apakah request menargetkan halaman (bukan API).
func isHTMLRequest(r *http.Request) bool {
	p := r.URL.Path
	return p == "/" || strings.HasSuffix(p, ".html") || !strings.HasPrefix(p, "/api/")
}

// guardTrusted memeriksa loopback/tepercaya (dipakai authRequired).
func (s *Server) guardTrusted(r *http.Request) bool {
	host := r.RemoteAddr
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	if host == "127.0.0.1" || host == "::1" || strings.EqualFold(host, "localhost") {
		return true
	}
	if s.cfg != nil {
		ip := net.ParseIP(host)
		if ip == nil {
			return false
		}
		for _, cidr := range s.cfg.TrustedCIDRs {
			if _, ipnet, err := net.ParseCIDR(cidr); err == nil && ipnet.Contains(ip) {
				return true
			}
		}
	}
	return false
}

// registerAuthRoutes mendaftarkan endpoint login/logout/session.
func (s *Server) registerAuthRoutes(mux *http.ServeMux) {
	// POST /api/auth/login {username,password}
	mux.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		if s.auth == nil || !s.auth.Verify(body.Username, body.Password) {
			writeJSON(w, 401, map[string]any{"ok": false, "error": "username/password salah"})
			return
		}
		token := s.auth.IssueSession(body.Username)
		auth.SetCookie(w, token, 12*time.Hour)
		s.auditAuth("login", body.Username, "ok")
		writeJSON(w, 200, map[string]any{"ok": true, "username": body.Username})
	})

	// POST /api/auth/logout
	mux.HandleFunc("/api/auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		if s.auth != nil {
			s.auth.RevokeSession(auth.FromRequest(r))
		}
		auth.ClearCookie(w)
		writeJSON(w, 200, map[string]any{"ok": true})
	})

	// GET /api/auth/session
	mux.HandleFunc("/api/auth/session", func(w http.ResponseWriter, r *http.Request) {
		tok := auth.FromRequest(r)
		if s.auth == nil || !s.auth.ValidateSession(tok) {
			writeJSON(w, 401, map[string]any{"ok": false})
			return
		}
		writeJSON(w, 200, map[string]any{"ok": true, "authenticated": true})
	})
}

// resetLoginViaWA menangani perintah "reset login <username> <passbaru>" dari
// superadmin lewat WhatsApp. Dijalankan KODE (bukan LLM), hanya superadmin.
func (s *Server) resetLoginViaWA(caller directory.Caller, msg string) (string, bool) {
	fields := strings.Fields(strings.TrimSpace(msg))
	if len(fields) < 4 || !strings.EqualFold(fields[0], "reset") || !strings.EqualFold(fields[1], "login") {
		return "", false
	}
	if !caller.IsStaff || caller.Role != directory.RoleSuperAdmin {
		return "Reset password login hanya diizinkan untuk superadmin.", true
	}
	username := fields[2]
	newPass := fields[3]
	if s.auth == nil {
		return "Autentikasi belum aktif.", true
	}
	if err := s.auth.ResetPassword(username, newPass); err != nil {
		return err.Error(), true
	}
	s.cfg.AuthUsers = s.auth.Users()
	s.cfg.AuthSalt = s.auth.Salt()
	if err := s.cfg.Save(); err != nil {
		return "Password direset tapi gagal disimpan: " + err.Error(), true
	}
	s.auditAuth("reset_password", username, "via WA")
	return "Password login untuk '" + username + "' berhasil direset.", true
}

func (s *Server) auditAuth(event, actor, note string) {
	if s.aud == nil {
		return
	}
	s.aud.Record(audit.Entry{
		EventType:  "auth_" + event,
		OccurredAt: time.Now().UTC(),
		Actor:      actor,
		EntityType: "auth",
		Note:       note,
	})
}

func logAuth(msg string) { println("[auth] " + msg) }
