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
	Addr        string `json:"addr"`
	LLMBaseURL  string `json:"llm_base_url"`
	LLMAPIKey   string `json:"llm_api_key"`
	LLMModel    string `json:"llm_model"`
	LLMTimeout  int    `json:"llm_timeout_sec"`
	MaxSteps    int    `json:"max_steps"`
	CodexPath   string `json:"codex_path"`
	CodexModel  string `json:"codex_model"`
	CodexSbx    string `json:"codex_sandbox"`
	DiagTimeout int    `json:"diag_timeout_sec"`
	Org         string `json:"org"`

	// Integrasi WhatsApp Gateway (Node/Baileys, project ai-noc).
	WABaseURL   string   `json:"wa_base_url"`
	WATimeout   int      `json:"wa_timeout_sec"`
	WAAllowlist []string `json:"wa_allowlist"`
	WAAutoReply bool     `json:"wa_auto_reply"`
	WAAsync     bool     `json:"wa_async"`
	WADir       string   `json:"wa_dir"`
	WAAutoStart bool     `json:"wa_autostart"`
	WAGroup     bool     `json:"wa_group"` // balas pesan dari grup juga
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

	// path adalah lokasi file config yang sedang dipakai. Disimpan supaya
	// perubahan dari dashboard bisa ditulis kembali ke file yang SAMA.
	// Tanpa ini, pengaturan hanya hidup di memori dan hilang saat restart.
	path string
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
		Addr:        ":8090",
		LLMBaseURL:  "http://127.0.0.1:20128/v1",
		LLMModel:    "ag/gemini-3.8-flash-high",
		LLMTimeout:  120,
		MaxSteps:    12,
		CodexPath:   "codex",
		CodexModel:  "cx/gpt-5.6-terra",
		CodexSbx:    "danger-full-access",
		DiagTimeout: 30,
		Org:         "NetLayer",

		WABaseURL:   "http://127.0.0.1:3001",
		WATimeout:   45,
		WAAutoReply: true,
		WAAsync:     false,
		WAAutoStart: true,
		CacheTTLMin: 10,
		SesiTTLMin:  120,
		MemoryPath:  defaultMemoryPath(),
	}
}

// defaultMemoryPath menentukan lokasi memory.json.
func defaultMemoryPath() string {
	if exe, err := os.Executable(); err == nil {
		base := filepath.Dir(exe)
		// bin/ai-noc-go.exe -> simpan di <root>/data/memory.json
		root := filepath.Dir(base)
		if filepath.Base(base) == "bin" {
			return filepath.Join(root, "data", "memory.json")
		}
		return filepath.Join(base, "data", "memory.json")
	}
	return filepath.Join("data", "memory.json")
}

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

	if v := getenv("NOC_ADDR"); v != "" {
		c.Addr = v
	}
	if v := getenv("NOC_LLM_BASE_URL", "LLM_BASE_URL"); v != "" {
		c.LLMBaseURL = v
	}
	if v := getenv("NOC_LLM_MODEL", "LLM_MODEL"); v != "" {
		c.LLMModel = v
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
	if v := getenv("NOC_WA_BASE_URL", "N8N_WEBHOOK_URL"); v != "" {
		c.WABaseURL = v
	}
	c.WATimeout = atoi(getenv("NOC_WA_TIMEOUT"), c.WATimeout)
	if v := getenv("NOC_WA_ALLOWLIST"); v != "" {
		for _, p := range strings.Split(v, ",") {
			if p = strings.TrimSpace(p); p != "" {
				c.WAAllowlist = append(c.WAAllowlist, p)
			}
		}
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
		"addr":          c.Addr,
		"llm_base_url":  c.LLMBaseURL,
		"llm_model":     c.LLMModel,
		"llm_key":       k,
		"llm_key_set":   c.LLMAPIKey != "",
		"llm_timeout":   c.LLMTimeout,
		"max_steps":     c.MaxSteps,
		"codex_path":    c.CodexPath,
		"codex_model":   c.CodexModel,
		"codex_sandbox": c.CodexSbx,
		"diag_timeout":  c.DiagTimeout,
		"org":           c.Org,
		"wa_base_url":   c.WABaseURL,
		"wa_timeout":    c.WATimeout,
		"wa_allowlist":  c.WAAllowlist,
		"wa_auto_reply": c.WAAutoReply,
		"wa_async":      c.WAAsync,
		"wa_enabled":    c.WABaseURL != "",
		"wa_dir":        c.WADir,
		"wa_autostart":  c.WAAutoStart,
		"wa_group":      c.WAGroup,
		"wa_embedded":   c.WADir != "",
		"standard_doc":  c.StandardDoc,
		"cache_ttl_min": c.CacheTTLMin,
		"sesi_ttl_min":  c.SesiTTLMin,
		"memory_path":   c.MemoryPath,
	}
}
