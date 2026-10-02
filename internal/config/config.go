// Package config memuat konfigurasi NOC dari environment / file / auth Codex.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Config struct {
	// Addr defaults to loopback. A non-local deployment must configure the
	// operator and webhook tokens below; the HTTP guard refuses unauthenticated
	// non-local control traffic.
	Addr          string `json:"addr"`
	OperatorToken string `json:"operator_token"`
	WebhookToken  string `json:"webhook_token"`
	// TrustedCIDRs: jaringan yang dipercaya seperti loopback (tanpa token
	// operator). Contoh "172.18.20.0/25". Kosong = hanya loopback dipercaya.
	TrustedCIDRs []string `json:"trusted_cidrs"`
	// DiagAllowlist berisi CIDR/IP/hostname yang boleh menjadi target diagnostik manual.
	// Kosong = semua diagnostic network ditolak.
	DiagAllowlist []string `json:"diag_allowlist"`
	LLMBaseURL    string `json:"llm_base_url"`
	LLMAPIKey     string `json:"llm_api_key"`
	LLMModel      string `json:"llm_model"`
	// LLMWireAPI: format komunikasi native (chat | responses | messages).
	// Default "chat" (/chat/completions).
	LLMWireAPI string `json:"llm_wire_api"`
	LLMTimeout int    `json:"llm_timeout_sec"`
	MaxSteps   int    `json:"max_steps"`
	CodexPath  string `json:"codex_path"`
	CodexModel string `json:"codex_model"`
	CodexSbx   string `json:"codex_sandbox"`
	// Endpoint Codex (opsional). Kosong = Codex memakai ~/.codex/config.toml
	// sendiri. Bila diisi, diteruskan sebagai -c override per-invoke (tanpa
	// menimpa config.toml user). Provider = nama model_providers di config.toml.
	CodexBaseURL  string `json:"codex_base_url"`
	CodexProvider string `json:"codex_provider"`
	CodexAPIKey   string `json:"codex_api_key"`
	// CodexWireAPI: format wire untuk Codex (chat | responses | messages).
	// Default "responses" (sama dengan config.toml user saat ini).
	CodexWireAPI string `json:"codex_wire_api"`
	DiagTimeout  int    `json:"diag_timeout_sec"`
	Org          string `json:"org"`

	// Integrasi WhatsApp Gateway (Node/Baileys, project ai-noc).
	WABaseURL   string   `json:"wa_base_url"`
	WATimeout   int      `json:"wa_timeout_sec"`
	WABlocklist []string `json:"wa_blocklist"`
	// Nomor kontak internal untuk eskalasi/notifikasi.
	NOCNumber   string `json:"noc_number"`
	AdminNumber string `json:"admin_number"`
	WAAutoReply bool   `json:"wa_auto_reply"`
	WAAsync     bool   `json:"wa_async"`
	WADir       string `json:"wa_dir"`
	WAAutoStart bool   `json:"wa_autostart"`
	WAGroup     bool   `json:"wa_group"` // balas pesan dari grup juga
	// StandardDoc: path file standar konteks yang bisa diedit operator tanpa
	// build ulang. Kosong = pakai standar bawaan (di-embed).
	StandardDoc string `json:"standard_doc"`
	// CacheTTLMin: masa berlaku cache jawaban per nomor (menit).
	CacheTTLMin int `json:"cache_ttl_min"`
	// SesiTTLMin: percakapan dianggap selesai setelah idle selama ini (menit).
	SesiTTLMin int `json:"sesi_ttl_min"`
	// MemoryPath: file JSON tempat memory (fakta & riwayat per pengirim) disimpan.
	// Kosong = pakai data/memory.json di samping binary.
	MemoryPath string `json:"memory_path"`

	// ---- Blueprint upgrade: policy, registry, workflow, incident, audit ----
	// Semua path kosong = pakai lokasi bawaan relatif ke binary. Komponen ini
	// deny-by-default: tanpa file policy/registry, tidak ada tool eksternal
	// maupun mutasi yang aktif.
	PolicyPath   string `json:"policy_path"`
	RegistryPath string `json:"registry_path"`
	WorkflowDir  string `json:"workflow_dir"`
	IncidentPath string `json:"incident_path"`
	AuditPath    string `json:"audit_path"`

	// ---- Endpoint adaptor eksternal (Billing/RADIUS/MikroTik/GenieACS) ----
	// Semua kosong = adaptor tidak aktif (deny-by-default). URL + token disimpan
	// lewat dashboard Pengaturan ke config.json (gitignored, aman). Env
	// NOC_*_URL / NOC_*_TOKEN tetap didukung dan MENANG bila diset.
	// Token TIDAK pernah muncul di Redacted()/API — hanya masked.
	BillingURL   string `json:"billing_url"`
	BillingToken string `json:"billing_token"`
	RadiusURL    string `json:"radius_url"`
	RadiusToken  string `json:"radius_token"`
	// MikroTik memakai API native (protokol biner) + Basic Auth (user+password),
	// BUKAN API key. Mendukung MULTI router: tiap entri punya nama+host+port+
	// user+pass+tls. Entri tunggal lama (mikrotik_host dll.) tetap didukung via
	// kompatibilitas mundur di Load().
	MikrotikHost string `json:"mikrotik_host"`
	MikrotikPort int    `json:"mikrotik_port"`
	MikrotikUser string `json:"mikrotik_user"`
	MikrotikPass string `json:"mikrotik_pass"`
	MikrotikTLS  bool   `json:"mikrotik_tls"`
	// MikrotikRouters adalah daftar router untuk fitur multi-MikroTik.
	MikrotikRouters []MikrotikRouter `json:"mikrotik_routers"`
	GenieACSURL     string           `json:"genieacs_url"`
	// Catatan: GenieACS NBI tidak punya token auth (keamanan via jaringan).
	// Tidak ada field token — hanya host.

	// path adalah lokasi file config yang sedang dipakai. Disimpan supaya
	// perubahan dari dashboard bisa ditulis kembali ke file yang SAMA.
	// Tanpa ini, pengaturan hanya hidup di memori dan hilang saat restart.
	path string
}

// MikrotikRouter adalah satu router MikroTik dalam daftar multi-router.
type MikrotikRouter struct {
	Name string `json:"name"` // label, mis. "Router Pusat"
	Host string `json:"host"` // IP/hostname
	Port int    `json:"port"` // 0 = default 8728/8729
	User string `json:"user"`
	Pass string `json:"pass"`
	TLS  bool   `json:"tls"`
}

// SetPath mencatat file config yang sedang dipakai, agar Save() menulis ke sana.
func (c *Config) SetPath(p string) { c.path = p }

// Path mengembalikan file config yang sedang dipakai.
func (c *Config) Path() string { return c.path }

// Save menulis konfigurasi kembali ke file asalnya. Perubahan dari dashboard
// WAJIB dipanggil ini, kalau tidak pengaturan hilang begitu aplikasi di-restart.
//
// Ditulis lewat file sementara lalu di-rename, supaya config yang sedang dipakai
// tidak pernah rusak separuh bila proses mati di tengah penulisan.
func (c *Config) Save() error {
	if c.path == "" {
		return fmt.Errorf("lokasi file config tidak diketahui (dijalankan tanpa file config)")
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

func atob(s string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	}
	return def
}

func getenv(keys ...string) string {
	for _, k := range keys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func atoi(s string, def int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil && n > 0 {
		return n
	}
	return def
}

// Default mengembalikan konfigurasi awal sebelum file/env diterapkan.
func Default() *Config {
	return &Config{
		Addr:         "127.0.0.1:8090",
		LLMBaseURL:   "http://127.0.0.1:20128/v1",
		LLMModel:     "ag/gemini-3.8-flash-high",
		LLMWireAPI:   "chat",
		LLMTimeout:   120,
		MaxSteps:     12,
		CodexPath:    "codex",
		CodexModel:   "cx/gpt-5.6-terra",
		CodexSbx:     "read-only",
		CodexWireAPI: "responses",
		DiagTimeout:  30,
		Org:          "NetLayer",

		WABaseURL:   "http://127.0.0.1:3001",
		WATimeout:   45,
		WAAutoReply: true,
		WAAsync:     false,
		WAAutoStart: true,
		CacheTTLMin: 10,
		SesiTTLMin:  120,
		MemoryPath:  defaultMemoryPath(),

		// Blueprint upgrade: deny-by-default, path relatif ke binary.
		PolicyPath:   defaultPolicyPath(),
		RegistryPath: defaultRegistryPath(),
		WorkflowDir:  defaultWorkflowDir(),
		IncidentPath: defaultIncidentPath(),
		AuditPath:    defaultAuditPath(),
	}
}

// defaultMemoryPath menentukan lokasi memory.json.
func defaultMemoryPath() string {
	return filepath.Join(dataDir(), "memory.json")
}

// defaultDataDir mengembalikan folder data/ relatif ke binary (bukan CWD),
// supaya lokasi konsisten walau aplikasi dijalankan dari folder berbeda.
func defaultDataDir() string {
	return dataDir()
}

// dataDir menghitung folder data/ di samping binary atau root proyek.
func dataDir() string {
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		root := filepath.Dir(base)
		if filepath.Base(base) == "bin" {
			return filepath.Join(root, "data")
		}
		return filepath.Join(base, "data")
	}
	return filepath.Join("data")
}

// defaultPolicyPath, dll. mengembalikan path bawaan untuk komponen blueprint.
// Semua relatif ke root proyek (di samping binary), bukan CWD.
func projectRoot() string {
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		if filepath.Base(base) == "bin" {
			return filepath.Dir(base)
		}
		return base
	}
	return "."
}

func defaultPolicyPath() string {
	return filepath.Join(projectRoot(), "policies", "default-policy.yaml")
}
// defaultRegistryPath mengembalikan path registry tool. Bila ada overlay lokal
// `tools/registry.local.yaml` (gitignored, step-2 operator untuk mengaktifkan
// tool READ setelah endpoint+kredensial nyata tersedia), file itu diutamakan.
// File yang di-commit (registry.yaml) TETAP deny-by-default demi kontrak keamanan.
func defaultRegistryPath() string {
	local := filepath.Join(projectRoot(), "tools", "registry.local.yaml")
	if _, err := os.Stat(local); err == nil {
		return local
	}
	return filepath.Join(projectRoot(), "tools", "registry.yaml")
}
func defaultWorkflowDir() string  { return filepath.Join(projectRoot(), "workflows") }
func defaultIncidentPath() string { return filepath.Join(dataDir(), "incidents.json") }
func defaultAuditPath() string    { return filepath.Join(dataDir(), "audit.json") }

// resolveWADir mencari folder gateway WA: eksplisit -> di samping exe -> CWD.
func resolveWADir(explicit string) string {
	candidates := []string{}
	if explicit != "" {
		candidates = append(candidates, explicit)
	}
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(base, "wa-gateway"),
			filepath.Join(filepath.Dir(base), "wa-gateway"),
		)
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "wa-gateway"),
			filepath.Join(filepath.Dir(wd), "wa-gateway"),
		)
	}
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(c, "app", "server.js")); err == nil {
			return c
		}
	}
	return ""
}

// keyFromCodexAuth membaca OPENAI_API_KEY dari ~/.codex/auth.json bila ada.
func keyFromCodexAuth() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(home, ".codex", "auth.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		AuthMode     string `json:"auth_mode"`
		OpenAIAPIKey string `json:"OPENAI_API_KEY"`
	}
	if json.Unmarshal(b, &doc) != nil {
		return ""
	}
	return strings.TrimSpace(doc.OpenAIAPIKey)
}

// Load: default -> config.json (opsional) -> environment -> auth Codex.
func Load(path string) *Config {
	c := Default()

	if path == "" {
		path = getenv("NOC_CONFIG")
	}
	if path == "" {
		if exe, err := os.Executable(); err == nil {
			p := filepath.Join(filepath.Dir(exe), "config.json")
			if _, err := os.Stat(p); err == nil {
				path = p
			}
		}
	}
	if path == "" {
		if _, err := os.Stat("config.json"); err == nil {
			path = "config.json"
		}
	}
	if path != "" {
		if b, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(b, c)
		}
	}
	// memory_path kosong = pakai lokasi bawaan (relatif ke binary). Tanpa ini,
	// config.json yang menyimpan path absolut akan mematikan memory begitu
	// folder proyek dipindahkan atau disalin ke komputer lain.
	if strings.TrimSpace(c.MemoryPath) == "" {
		c.MemoryPath = defaultMemoryPath()
	}
	// Path komponen blueprint yang kosong = pakai lokasi bawaan.
	if strings.TrimSpace(c.PolicyPath) == "" {
		c.PolicyPath = defaultPolicyPath()
	}
	if strings.TrimSpace(c.RegistryPath) == "" {
		c.RegistryPath = defaultRegistryPath()
	}
	if strings.TrimSpace(c.WorkflowDir) == "" {
		c.WorkflowDir = defaultWorkflowDir()
	}
	if strings.TrimSpace(c.IncidentPath) == "" {
		c.IncidentPath = defaultIncidentPath()
	}
	if strings.TrimSpace(c.AuditPath) == "" {
		c.AuditPath = defaultAuditPath()
	}

	if v := getenv("NOC_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("NOC_OPERATOR_TOKEN"); v != "" {
		c.OperatorToken = v
	}
	if v := getenv("NOC_WEBHOOK_TOKEN"); v != "" {
		c.WebhookToken = v
	}
	if v := getenv("NOC_TRUSTED_CIDRS"); v != "" {
		c.TrustedCIDRs = nil
		for _, entry := range strings.Split(v, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				c.TrustedCIDRs = append(c.TrustedCIDRs, entry)
			}
		}
	}
	if v := getenv("NOC_DIAG_ALLOWLIST"); v != "" {
		c.DiagAllowlist = nil
		for _, entry := range strings.Split(v, ",") {
			if entry = strings.TrimSpace(entry); entry != "" {
				c.DiagAllowlist = append(c.DiagAllowlist, entry)
			}
		}
	}
	if v := getenv("NOC_LLM_BASE_URL", "LLM_BASE_URL"); v != "" {
		c.LLMBaseURL = v
	}
	if v := getenv("NOC_LLM_MODEL", "LLM_MODEL"); v != "" {
		c.LLMModel = v
	}
	if v := getenv("NOC_LLM_WIRE_API"); v != "" {
		c.LLMWireAPI = v
	}
	c.LLMTimeout = atoi(getenv("NOC_LLM_TIMEOUT"), c.LLMTimeout)
	c.MaxSteps = atoi(getenv("NOC_MAX_STEPS"), c.MaxSteps)
	c.DiagTimeout = atoi(getenv("NOC_DIAG_TIMEOUT"), c.DiagTimeout)
	if v := getenv("NOC_CODEX_PATH"); v != "" {
		c.CodexPath = v
	}
	if v := getenv("NOC_CODEX_SANDBOX"); v != "" {
		c.CodexSbx = v
	}
	if v := getenv("NOC_CODEX_BASE_URL"); v != "" {
		c.CodexBaseURL = v
	}
	if v := getenv("NOC_CODEX_PROVIDER"); v != "" {
		c.CodexProvider = v
	}
	if v := getenv("NOC_CODEX_API_KEY"); v != "" {
		c.CodexAPIKey = v
	}
	if v := getenv("NOC_CODEX_WIRE_API"); v != "" {
		c.CodexWireAPI = v
	}
	if v := getenv("NOC_WA_BASE_URL"); v != "" {
		c.WABaseURL = v
	}
	c.WATimeout = atoi(getenv("NOC_WA_TIMEOUT"), c.WATimeout)
	if v := getenv("NOC_WA_BLOCKLIST"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.WABlocklist = append(c.WABlocklist, p)
			}
		}
	}
	if v := getenv("NOC_NUMBER"); v != "" {
		c.NOCNumber = strings.TrimSpace(v)
	}
	if v := getenv("NOC_ADMIN_NUMBER"); v != "" {
		c.AdminNumber = strings.TrimSpace(v)
	}
	c.WAAutoReply = atob(getenv("NOC_WA_AUTO_REPLY"), c.WAAutoReply)
	c.WAAsync = atob(getenv("NOC_WA_ASYNC"), c.WAAsync)
	c.WAAutoStart = atob(getenv("NOC_WA_AUTOSTART"), c.WAAutoStart)
	c.WAGroup = atob(getenv("NOC_WA_GROUP"), c.WAGroup)
	if v := getenv("NOC_STANDARD_DOC"); v != "" {
		c.StandardDoc = v
	}
	c.CacheTTLMin = atoi(getenv("NOC_CACHE_TTL_MIN"), c.CacheTTLMin)
	c.SesiTTLMin = atoi(getenv("NOC_SESI_TTL_MIN"), c.SesiTTLMin)
	if v := getenv("NOC_MEMORY_PATH"); v != "" {
		c.MemoryPath = v
	}
	if v := getenv("NOC_POLICY_PATH"); v != "" {
		c.PolicyPath = v
	}
	if v := getenv("NOC_REGISTRY_PATH"); v != "" {
		c.RegistryPath = v
	}
	if v := getenv("NOC_WORKFLOW_DIR"); v != "" {
		c.WorkflowDir = v
	}
	if v := getenv("NOC_INCIDENT_PATH"); v != "" {
		c.IncidentPath = v
	}
	if v := getenv("NOC_AUDIT_PATH"); v != "" {
		c.AuditPath = v
	}
	// Endpoint adaptor eksternal — config.json menyimpan, env MENANG bila diset.
	if v := getenv("NOC_BILLING_URL"); v != "" {
		c.BillingURL = v
	}
	if v := getenv("NOC_BILLING_TOKEN"); v != "" {
		c.BillingToken = v
	}
	if v := getenv("NOC_RADIUS_URL"); v != "" {
		c.RadiusURL = v
	}
	if v := getenv("NOC_RADIUS_TOKEN"); v != "" {
		c.RadiusToken = v
	}
	if v := getenv("NOC_MIKROTIK_HOST"); v != "" {
		c.MikrotikHost = v
	}
	if v := getenv("NOC_MIKROTIK_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			c.MikrotikPort = p
		}
	}
	if v := getenv("NOC_MIKROTIK_USER"); v != "" {
		c.MikrotikUser = v
	}
	if v := getenv("NOC_MIKROTIK_PASS"); v != "" {
		c.MikrotikPass = v
	}
	if v := getenv("NOC_MIKROTIK_TLS"); v != "" {
		c.MikrotikTLS = v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
	}
	if v := getenv("NOC_GENIEACS_URL"); v != "" {
		c.GenieACSURL = v
	}
	if v := getenv("NOC_WA_DIR"); v != "" {
		c.WADir = v
	}
	c.WADir = resolveWADir(c.WADir)
	// Catat file config yang dipakai, supaya perubahan dari dashboard bisa
	// ditulis kembali ke file yang sama (lihat Config.Save).
	if _, err := os.Stat(path); err == nil {
		c.SetPath(path)
	}

	// Urutan pencarian API key: env NOC -> env Hermes 9router -> auth.json Codex.
	if c.LLMAPIKey == "" {
		c.LLMAPIKey = getenv("NOC_LLM_API_KEY", "LLM_API_KEY", "HERMES_CUSTOM_9ROUTER_API_KEY", "OPENAI_API_KEY")
	}
	if c.LLMAPIKey == "" {
		c.LLMAPIKey = keyFromCodexAuth()
	}
	// Phase 0: Codex is analysis-only. Configuration cannot widen its sandbox.
	if c.CodexSbx != "read-only" {
		c.CodexSbx = "read-only"
	}
	c.LLMBaseURL = strings.TrimRight(c.LLMBaseURL, "/")
	return c
}

// Redacted untuk ditampilkan ke UI (jangan pernah kirim key mentah ke browser).
func (c *Config) Redacted() map[string]any {
	k := ""
	if c.LLMAPIKey != "" {
		if len(c.LLMAPIKey) > 8 {
			k = c.LLMAPIKey[:8] + "..." + c.LLMAPIKey[len(c.LLMAPIKey)-4:]
		} else {
			k = "***"
		}
	}
	return map[string]any{
		"addr":               c.Addr,
		"operator_token_set": c.OperatorToken != "",
		"webhook_token_set":  c.WebhookToken != "",
		"llm_base_url":       c.LLMBaseURL,
		"llm_model":          c.LLMModel,
		"llm_wire_api":       c.LLMWireAPI,
		"llm_key":            k,
		"llm_key_set":        c.LLMAPIKey != "",
		"llm_timeout":        c.LLMTimeout,
		"max_steps":          c.MaxSteps,
		"codex_path":         c.CodexPath,
		"codex_model":        c.CodexModel,
		"codex_sandbox":      c.CodexSbx,
		"codex_base_url":     c.CodexBaseURL,
		"codex_provider":     c.CodexProvider,
		"codex_wire_api":     c.CodexWireAPI,
		"codex_key_set":      c.CodexAPIKey != "",
		"codex_key":          maskSecret(c.CodexAPIKey),
		"diag_timeout":       c.DiagTimeout,
		"org":                c.Org,
		"wa_base_url":        c.WABaseURL,
		"wa_timeout":         c.WATimeout,
		"wa_blocklist":       c.WABlocklist,
		"noc_number":         c.NOCNumber,
		"admin_number":       c.AdminNumber,
		"wa_auto_reply":      c.WAAutoReply,
		"wa_async":           c.WAAsync,
		"wa_enabled":         c.WABaseURL != "",
		"wa_dir":             c.WADir,
		"wa_autostart":       c.WAAutoStart,
		"wa_group":           c.WAGroup,
		"wa_embedded":        c.WADir != "",
		"standard_doc":       c.StandardDoc,
		"cache_ttl_min":      c.CacheTTLMin,
		"sesi_ttl_min":       c.SesiTTLMin,
		"memory_path":        c.MemoryPath,
		"policy_path":        c.PolicyPath,
		"registry_path":      c.RegistryPath,
		"workflow_dir":       c.WorkflowDir,
		"incident_path":      c.IncidentPath,
		"audit_path":         c.AuditPath,
		// Endpoint adaptor eksternal (tanpa token mentah — hanya masked).
		"billing_url":          c.BillingURL,
		"radius_url":           c.RadiusURL,
		"mikrotik_host":        c.MikrotikHost,
		"mikrotik_port":        c.MikrotikPort,
		"mikrotik_user":        c.MikrotikUser, // username bukan rahasia (tetap ditampilkan)
		"mikrotik_tls":         c.MikrotikTLS,
		"mikrotik_routers":     c.redactedRouters(),
		"mikrotik_count":       len(c.Routers()),
		"genieacs_url":         c.GenieACSURL,
		"billing_set":          c.BillingURL != "" && c.BillingToken != "",
		"radius_set":           c.RadiusURL != "" && c.RadiusToken != "",
		"mikrotik_set":         len(c.Routers()) > 0,
		"genieacs_set":         c.GenieACSURL != "",
		"billing_token_masked": maskSecret(c.BillingToken),
		"radius_token_masked":  maskSecret(c.RadiusToken),
		"mikrotik_pass_masked": maskSecret(c.MikrotikPass),
	}
}

// maskSecret mengembalikan bentuk aman sebuah secret untuk ditampilkan ke UI:
// kosong bila kosong, "••••abcd" (4 karakter terakhir) bila ada.
func maskSecret(s string) string {
	if s == "" {
		return ""
	}
	if len(s) <= 4 {
		return "••••"
	}
	return "••••" + s[len(s)-4:]
}

// Routers mengembalikan daftar router MikroTik yang terkonfigurasi. Bila
// MikrotikRouters terisi, pakai itu; bila tidak, jatuh ke field tunggal lama
// (kompatibilitas mundur).
func (c *Config) Routers() []MikrotikRouter {
	if len(c.MikrotikRouters) > 0 {
		return c.MikrotikRouters
	}
	if c.MikrotikHost != "" && c.MikrotikUser != "" {
		return []MikrotikRouter{{
			Name: c.MikrotikHost,
			Host: c.MikrotikHost,
			Port: c.MikrotikPort,
			User: c.MikrotikUser,
			Pass: c.MikrotikPass,
			TLS:  c.MikrotikTLS,
		}}
	}
	return nil
}

// redactedRouters mengembalikan daftar router untuk UI tanpa password mentah.
func (c *Config) redactedRouters() []map[string]any {
	rs := c.Routers()
	out := make([]map[string]any, 0, len(rs))
	for _, r := range rs {
		out = append(out, map[string]any{
			"name":        r.Name,
			"host":        r.Host,
			"port":        r.Port,
			"user":        r.User,
			"tls":         r.TLS,
			"pass_masked": maskSecret(r.Pass),
		})
	}
	return out
}
