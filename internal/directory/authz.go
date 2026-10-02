package directory

// Otorisasi aksi berbasis identitas (master spec §24, §38).
//
// Memetakan nama tool -> izin yang dibutuhkan, lalu memutuskan apakah seorang
// Caller boleh menjalankannya: izin cukup? perlu PIN? PIN sudah diverifikasi?
//
// Ini lapisan DI ATAS gerbang dispatcher (registry->policy->adapter). Dispatcher
// tetap deny-by-default; lapisan ini menambah dimensi "siapa yang meminta".

// Decision adalah hasil otorisasi aksi.
type Decision string

const (
	Allow   Decision = "ALLOW"    // boleh langsung
	NeedPIN Decision = "NEED_PIN" // izin cukup, tapi wajib verifikasi PIN dulu
	Deny    Decision = "DENY"     // tidak berwenang
)

// AuthResult menjelaskan keputusan + alasannya (untuk audit & balasan).
type AuthResult struct {
	Decision Decision
	Perm     Permission
	Reason   string
}

// toolPerms memetakan nama tool -> izin minimal yang dibutuhkan untuk
// menjalankannya. Tool baca (get_*) butuh izin baca sesuai domain; tool WRITE
// butuh izin tulis yang sesuai. Tool yang tidak terdaftar di sini dianggap
// berisiko tinggi -> butuh izin manage_staff (praktis: hanya super admin).
var toolPerms = map[string]Permission{
	// --- Networking READ ---
	"mikrotik.get_pppoe_status":    PermNetworkingRead,
	"mikrotik.get_interface_stats": PermNetworkingRead,
	"mikrotik.get_interface_live":  PermNetworkingRead,
	"mikrotik.get_customer_traffic": PermNetworkingRead,
	"radius.get_session":           PermNetworkingRead,
	"radius.get_user":              PermNetworkingRead,
	"radius.get_system_stats":      PermNetworkingRead,
	"genieacs.get_device_state":    PermNetworkingRead,
	"genieacs.get_devices":         PermNetworkingRead,
	// --- Billing READ ---
	"billing.get_customer": PermReadAll,
	// --- Networking WRITE (berisiko) ---
	"mikrotik.disconnect_pppoe": PermNetworkingWrite,
}

// PermForTool mengembalikan izin yang dibutuhkan untuk sebuah tool. Tool tak
// dikenal -> PermManageStaff (paling ketat) agar default-nya aman.
func PermForTool(tool string) Permission {
	if p, ok := toolPerms[tool]; ok {
		return p
	}
	return PermManageStaff
}

// Authorize memutuskan apakah caller boleh menjalankan tool.
//
//   - caller tidak punya izin -> Deny.
//   - izin cukup tapi aksi berisiko & PIN belum diverifikasi -> NeedPIN.
//   - izin cukup & (tidak berisiko | PIN sudah diverifikasi) -> Allow.
func Authorize(c Caller, tool string, pinVerified bool) AuthResult {
	perm := PermForTool(tool)
	if !c.Can(perm) {
		return AuthResult{Decision: Deny, Perm: perm,
			Reason: "role " + string(c.Role) + " tidak memiliki izin " + string(perm)}
	}
	if NeedsPIN(perm) && !pinVerified {
		return AuthResult{Decision: NeedPIN, Perm: perm,
			Reason: "aksi berisiko (" + string(perm) + ") butuh verifikasi PIN"}
	}
	return AuthResult{Decision: Allow, Perm: perm, Reason: "diizinkan"}
}
