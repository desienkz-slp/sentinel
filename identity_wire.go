package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/billing"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/identity"
)

// ============================================================================
//  Identifikasi penelepon + RBAC berbasis identitas (master spec §6, §22, §38).
//
//  Saat pesan WA masuk, sistem mencari siapa pengirimnya: direktori staf dulu
//  (admin/NOC/super admin), lalu billing (pelanggan). Hak aksi mengikuti role.
//  Aksi berisiko (delete/edit/putus layanan) butuh verifikasi PIN.
// ============================================================================

// billingShim mengadaptasi *billing.Adapter ke kontrak directory.BillingLookup
// (hindari import cycle: directory tidak kenal paket billing).
type billingShim struct{ a *billing.Adapter }

func (s billingShim) LookupCustomerByPhone(ctx context.Context, phone string) (directory.CustomerInfo, bool, error) {
	if s.a == nil {
		return directory.CustomerInfo{}, false, nil
	}
	c, ok, err := s.a.LookupByPhone(ctx, phone)
	if err != nil || !ok {
		return directory.CustomerInfo{}, ok, err
	}
	return directory.CustomerInfo{
		Name:       c.Name,
		Username:   c.Username,
		Phone:      c.Phone,
		Status:     c.Status,
		IsIsolated: c.IsIsolated,
		Area:       c.Area.Name,
		Package:    c.Package.Name,
	}, true, nil
}

// dirFromConfig membangun directory.Directory dari cfg.StaffMembers.
func dirFromConfig(cfg *config.Config) *directory.Directory {
	members := make([]directory.Member, 0, len(cfg.StaffMembers)+2)
	for _, sm := range cfg.StaffMembers {
		members = append(members, directory.Member{
			Number:  sm.Number,
			Name:    sm.Name,
			Role:    directory.Role(sm.Role),
			Title:   sm.Title,
			Active:  sm.Active,
			PINHash: sm.PINHash,
		})
	}
	return directory.New(members)
}

// migrateLegacyStaff memindahkan nomor NOC/Admin lama (noc_number /
// admin_number / env NOC_NUMBER, NOC_ADMIN_NUMBER) ke StaffMembers sekali jalan,
// supaya Direktori Staf menjadi satu-satunya sumber data. Nomor yang sudah ada
// di direktori tidak ditimpa. Mengembalikan true bila ada yang dipindahkan.
func migrateLegacyStaff(cfg *config.Config) bool {
	moved := false
	add := func(num string, role directory.Role, name string) {
		n := identity.Normalize(num)
		if n == "" {
			return
		}
		for _, sm := range cfg.StaffMembers {
			if identity.Normalize(sm.Number) == n {
				return
			}
		}
		cfg.StaffMembers = append(cfg.StaffMembers, config.StaffMember{
			Number: n, Name: name, Role: string(role), Active: true,
		})
		moved = true
	}
	add(cfg.NOCNumber, directory.RoleNOCSenior, "NOC Senior")
	add(cfg.AdminNumber, directory.RoleAdmin, "Admin")
	return moved
}

// syncDirectory membangun ulang direktori + identifier dari config saat ini.
// Dipanggil saat startup dan setiap config staf berubah.
func (s *Server) syncDirectory() {
	s.dir = dirFromConfig(s.cfg)
	var bl directory.BillingLookup
	if s.billing != nil {
		bl = billingShim{a: s.billing}
	}
	s.idf = directory.NewIdentifier(s.dir, bl)
}

// identifyCaller mengidentifikasi penelepon dari nomornya (aman bila idf nil).
func (s *Server) identifyCaller(ctx context.Context, number string) directory.Caller {
	if s.idf == nil {
		return directory.Caller{Number: identity.Normalize(number), Role: directory.RoleCustomer, Perms: directory.PermsFor(directory.RoleCustomer)}
	}
	return s.idf.Identify(ctx, number)
}

// ---- PIN session store: verifikasi PIN berlaku sementara (anti minta PIN tiap aksi) ----

type pinStore struct {
	mu    sync.Mutex
	until map[string]time.Time
	ttl   time.Duration
}

func newPINStore(ttl time.Duration) *pinStore {
	return &pinStore{until: make(map[string]time.Time), ttl: ttl}
}

// grant menandai nomor sudah lolos PIN sampai now+ttl.
func (p *pinStore) grant(number string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.until[identity.Normalize(number)] = time.Now().Add(p.ttl)
}

// verified melaporkan apakah nomor masih dalam jendela PIN yang valid.
func (p *pinStore) verified(number string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	t, ok := p.until[identity.Normalize(number)]
	return ok && time.Now().Before(t)
}

// ---- HTTP endpoints ----

// registerIdentityRoutes mendaftarkan endpoint identifikasi + manajemen staf.
func (s *Server) registerIdentityRoutes(mux *http.ServeMux) {
	// GET /api/identify?nomor=628xxx — siapa penelepon ini + hak aksesnya.
	mux.HandleFunc("/api/identify", func(w http.ResponseWriter, r *http.Request) {
		num := strings.TrimSpace(r.URL.Query().Get("nomor"))
		if num == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "parameter nomor wajib"})
			return
		}
		ctx, cancel := timeoutCtx(r, 15*time.Second)
		defer cancel()
		c := s.identifyCaller(ctx, num)
		writeJSON(w, 200, map[string]any{
			"ok":            true,
			"number":        c.Number,
			"role":          c.Role,
			"role_label":    directory.Role(c.Role).Label(),
			"name":          c.Name,
			"title":         c.Title,
			"is_staff":      c.IsStaff,
			"is_customer":   c.IsCustomer,
			"customer":      c.Customer,
			"perms":         c.Perms,
			"pin_verified":  s.pinSesi != nil && s.pinSesi.verified(c.Number),
			"billing_error": c.BillingError,
		})
	})

	// GET /api/staff — daftar staf (tanpa hash PIN).
	// POST /api/staff — tambah/ubah staf. Body: {number,name,role,title,active,pin?}
	//   pin (opsional) di-hash; kosong = pertahankan PIN lama.
	// DELETE /api/staff?nomor=628xxx — hapus staf.
	mux.HandleFunc("/api/staff", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, 200, map[string]any{"ok": true, "staff": s.dir.Members(), "roles": staffRoles(), "role_perms": rolePerms()})
		case http.MethodPost:
			s.handleStaffUpsert(w, r)
		case http.MethodDelete:
			s.handleStaffDelete(w, r)
		default:
			writeJSON(w, 405, map[string]any{"ok": false, "error": "metode tidak didukung"})
		}
	})

	// POST /api/staff/verify-pin — verifikasi PIN untuk membuka aksi berisiko.
	// Body: {number, pin}. Sukses -> grant jendela PIN (default 10 menit).
	mux.HandleFunc("/api/staff/verify-pin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		var body struct {
			Number string `json:"number"`
			PIN    string `json:"pin"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		m, ok := s.dir.LookupWithPIN(body.Number)
		if !ok {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "nomor bukan staf terdaftar"})
			return
		}
		if !m.HasPIN() {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "staf ini belum punya PIN"})
			return
		}
		if !m.CheckPIN(body.PIN) {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "PIN salah"})
			return
		}
		s.pinSesi.grant(body.Number)
		writeJSON(w, 200, map[string]any{"ok": true, "message": "PIN terverifikasi", "valid_until": time.Now().Add(s.pinSesi.ttl).Format(time.RFC3339)})
	})

	// POST /api/action/execute — jalankan aksi (termasuk WRITE) atas nama penelepon.
	// Rantai: identify -> authorize (role+izin) -> cek PIN -> audit -> dispatch.
	// Body: {number, tool, args?}
	// Respons NEED_PIN berarti klien harus verify-pin dulu lalu ulangi.
	mux.HandleFunc("/api/action/execute", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeJSON(w, 405, map[string]any{"ok": false, "error": "pakai POST"})
			return
		}
		var body struct {
			Number string         `json:"number"`
			Tool   string         `json:"tool"`
			Args   map[string]any `json:"args"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		body.Tool = strings.TrimSpace(body.Tool)
		if body.Number == "" || body.Tool == "" {
			writeJSON(w, 400, map[string]any{"ok": false, "error": "number dan tool wajib"})
			return
		}
		ctx, cancel := timeoutCtx(r, 25*time.Second)
		defer cancel()

		// 1. Identifikasi penelepon.
		caller := s.identifyCaller(ctx, body.Number)

		// 2. Otorisasi berbasis role + PIN.
		pinOK := s.pinSesi != nil && s.pinSesi.verified(caller.Number)
		authz := directory.Authorize(caller, body.Tool, pinOK)

		// 3. Audit SELALU (baik ditolak maupun dijalankan) — master spec §25.
		s.auditAction(caller, body.Tool, body.Args, authz, pinOK)

		switch authz.Decision {
		case directory.Deny:
			writeJSON(w, 403, map[string]any{
				"ok": false, "decision": "DENY", "error": authz.Reason,
				"role": caller.Role, "perm_needed": authz.Perm,
			})
			return
		case directory.NeedPIN:
			writeJSON(w, 200, map[string]any{
				"ok": false, "decision": "NEED_PIN",
				"message":     "Aksi ini berisiko. Verifikasi PIN dulu via /api/staff/verify-pin lalu ulangi.",
				"perm_needed": authz.Perm, "role": caller.Role,
			})
			return
		}

		// 4. Lolos otorisasi -> teruskan ke dispatcher (registry->policy->adapter).
		//    Dispatcher TETAP gerbang terakhir: bila tool WRITE belum terdaftar/
		//    aktif atau adapter belum implement, hasilnya ditolak jujur (tidak palsu).
		if s.disp == nil {
			writeJSON(w, 200, map[string]any{"ok": false, "error": "tool gateway tidak siap"})
			return
		}
		res := s.disp.Invoke(ctx, body.Tool, body.Args)
		// Audit hasil eksekusi.
		s.auditActionResult(caller, body.Tool, res.OK, string(res.Decision), res.Error)
		writeJSON(w, 200, map[string]any{
			"ok":               res.OK,
			"decision":         "AUTHORIZED",
			"actor":            caller.Number,
			"role":             caller.Role,
			"tool":             body.Tool,
			"result":           res.Output,
			"gateway_decision": res.Decision,
			"error":            res.Error,
			"request_id":       res.RequestID,
		})
	})
}

func (s *Server) handleStaffUpsert(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Number string `json:"number"`
		Name   string `json:"name"`
		Role   string `json:"role"`
		Title  string `json:"title"`
		Active *bool  `json:"active"`
		PIN    string `json:"pin"` // plaintext opsional; di-hash di sini
	}
	if err := decodeJSON(r, &body); err != nil {
		writeJSON(w, 400, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	num := identity.Normalize(body.Number)
	if num == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "nomor tidak valid"})
		return
	}
	role := directory.Role(strings.TrimSpace(body.Role))
	if !role.Valid() || role == directory.RoleCustomer {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "role harus admin, noc_senior, atau super_admin"})
		return
	}
	active := true
	if body.Active != nil {
		active = *body.Active
	}
	// Cari entri lama (untuk mempertahankan PIN bila tidak diganti).
	var pinHash string
	for _, sm := range s.cfg.StaffMembers {
		if identity.Normalize(sm.Number) == num {
			pinHash = sm.PINHash
			break
		}
	}
	if strings.TrimSpace(body.PIN) != "" {
		pinHash = directory.HashPIN(body.PIN)
	}
	// Upsert ke config.
	updated := false
	for i := range s.cfg.StaffMembers {
		if identity.Normalize(s.cfg.StaffMembers[i].Number) == num {
			s.cfg.StaffMembers[i] = config.StaffMember{
				Number: num, Name: body.Name, Role: string(role), Title: body.Title, Active: active, PINHash: pinHash,
			}
			updated = true
			break
		}
	}
	if !updated {
		s.cfg.StaffMembers = append(s.cfg.StaffMembers, config.StaffMember{
			Number: num, Name: body.Name, Role: string(role), Title: body.Title, Active: active, PINHash: pinHash,
		})
	}
	if err := s.cfg.Save(); err != nil {
		writeJSON(w, 500, map[string]any{"ok": false, "error": "gagal simpan config: " + err.Error()})
		return
	}
	s.syncDirectory()
	writeJSON(w, 200, map[string]any{"ok": true, "message": fmt.Sprintf("staf %s disimpan", num), "staff": s.dir.Members()})
}

func (s *Server) handleStaffDelete(w http.ResponseWriter, r *http.Request) {
	num := identity.Normalize(r.URL.Query().Get("nomor"))
	if num == "" {
		writeJSON(w, 400, map[string]any{"ok": false, "error": "parameter nomor wajib"})
		return
	}
	out := make([]config.StaffMember, 0, len(s.cfg.StaffMembers))
	removed := false
	for _, sm := range s.cfg.StaffMembers {
		if identity.Normalize(sm.Number) == num {
			removed = true
			continue
		}
		out = append(out, sm)
	}
	if !removed {
		writeJSON(w, 404, map[string]any{"ok": false, "error": "staf tidak ditemukan"})
		return
	}
	prev := s.cfg.StaffMembers
	s.cfg.StaffMembers = out
	if err := s.cfg.Save(); err != nil {
		s.cfg.StaffMembers = prev // jangan biarkan memori menyimpang dari disk
		writeJSON(w, 500, map[string]any{"ok": false, "error": "gagal simpan config: " + err.Error()})
		return
	}
	s.syncDirectory()
	writeJSON(w, 200, map[string]any{"ok": true, "removed": true, "staff": s.dir.Members()})
}

// staffRoles mengembalikan daftar role internal yang bisa dipilih di dashboard.
func staffRoles() []map[string]string {
	return []map[string]string{
		{"value": string(directory.RoleAdmin), "label": directory.RoleAdmin.Label()},
		{"value": string(directory.RoleNOCSenior), "label": directory.RoleNOCSenior.Label()},
		{"value": string(directory.RoleSuperAdmin), "label": directory.RoleSuperAdmin.Label()},
	}
}

// rolePerms memetakan role -> izin bawaan (untuk ditampilkan di dashboard).
func rolePerms() map[string][]directory.Permission {
	out := map[string][]directory.Permission{}
	for _, r := range []directory.Role{directory.RoleAdmin, directory.RoleNOCSenior, directory.RoleSuperAdmin} {
		out[string(r)] = directory.PermsFor(r)
	}
	return out
}

// decodeJSON mem-parse body JSON request ke v dengan batas ukuran wajar.
func decodeJSON(r *http.Request, v any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)) // 1 MB
	return dec.Decode(v)
}

// ---- Context: selipkan identitas penelepon untuk dibaca downstream ----

// withCaller menanam Caller ke context agar agent/handler hilir tahu siapa &
// perannya (tanpa mengubah tanda tangan engine.Run yang sudah ada).
func withCaller(ctx context.Context, c directory.Caller) context.Context {
	return directory.WithCaller(ctx, c)
}

// callerFrom mengambil Caller dari context; ok=false bila tidak ada.
func callerFrom(ctx context.Context) (directory.Caller, bool) {
	return directory.CallerFrom(ctx)
}

// auditAction mencatat keputusan otorisasi aksi (master spec §25). Argumen
// disanitasi: args disalin apa adanya (tanpa kredensial — tool read/write kita
// tidak menerima kredensial di args).
func (s *Server) auditAction(c directory.Caller, tool string, args map[string]any, authz directory.AuthResult, pinOK bool) {
	if s.aud == nil {
		return
	}
	human := "pin_verified=" + boolStr(pinOK)
	s.aud.Record(audit.Entry{
		EventType:      "action_authz",
		OccurredAt:     time.Now().UTC(),
		Actor:          c.Number + " (" + string(c.Role) + ")",
		Tool:           tool,
		Arguments:      args,
		PolicyDecision: string(authz.Decision),
		HumanApproval:  human,
		Note:           authz.Reason,
	})
}

// auditActionResult mencatat hasil eksekusi aksi yang sudah lolos otorisasi.
func (s *Server) auditActionResult(c directory.Caller, tool string, ok bool, gatewayDecision, errMsg string) {
	if s.aud == nil {
		return
	}
	status := "success"
	if !ok {
		status = "failed"
	}
	s.aud.Record(audit.Entry{
		EventType:       "action_execute",
		OccurredAt:      time.Now().UTC(),
		Actor:           c.Number + " (" + string(c.Role) + ")",
		Tool:            tool,
		PolicyDecision:  gatewayDecision,
		ExecutionStatus: status,
		Error:           errMsg,
	})
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
