// Package codexbridge menjalankan Codex CLI secara non-interaktif dari program Go.
// Dipakai sebagai "escalation engine" saat agen butuh analisis lebih dalam.
package codexbridge

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type Result struct {
	OK        bool   `json:"ok"`
	Output    string `json:"output"`
	Err       string `json:"err,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
	ExitCode  int    `json:"exit_code"`
	Command   string `json:"command"`
}

type Bridge struct {
	CodexPath  string
	Model      string
	Sandbox    string
	Timeout    time.Duration
	WorkingDir string
	// Endpoint override (opsional). Kosong = pakai ~/.codex/config.toml.
	// Bila diisi, diteruskan sebagai -c key=value per-invoke (tidak menimpa
	// config.toml user). Provider = nama model_providers di config.toml.
	BaseURL  string
	Provider string
	APIKey   string
	// WireAPI: format komunikasi native (chat | responses | messages).
	// Default "responses" (sesuai config.toml user saat ini).
	WireAPI string
}

func New(codexPath, model, sandbox string, timeoutSec int) *Bridge {
	if timeoutSec <= 0 {
		timeoutSec = 180
	}
	if sandbox == "" {
		sandbox = "danger-full-access"
	}
	wd, _ := os.MkdirTemp("", "ainoc-codex-*")
	return &Bridge{
		CodexPath:  codexPath,
		Model:      model,
		Sandbox:    sandbox,
		Timeout:    time.Duration(timeoutSec) * time.Second,
		WorkingDir: wd,
	}
}

// Available melaporkan apakah binary codex dapat ditemukan (tanpa menjalankannya).
func (b *Bridge) Available() bool {
	return b.resolve() != ""
}

// resolve mencari binary Codex.
//
// PENTING (Windows): exec.LookPath("codex") mengembalikan shim npm
// `codex.CMD`, yang berjalan lewat cmd.exe dan MERUSAK argumen multi-baris
// (prompt panjang terpotong / berubah). Utamakan codex.exe native bila ada.
func (b *Bridge) resolve() string {
	if filepath.IsAbs(b.CodexPath) {
		if _, err := os.Stat(b.CodexPath); err == nil {
			return b.CodexPath
		}
		return ""
	}
	if p, err := exec.LookPath(b.CodexPath); err == nil {
		if !isBatchShim(p) {
			return p
		}
		if native := findNativeCodex(); native != "" {
			return native
		}
		return p // fallback: shim tetap dipakai (prompt dikirim via stdin)
	}
	return findNativeCodex()
}

func isBatchShim(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	return ext == ".cmd" || ext == ".bat"
}

// findNativeCodex mencari codex.exe di lokasi instalasi Codex untuk Windows.
func findNativeCodex() string {
	// 1. Dari config.toml Codex (CODEX_CLI_PATH) bila tersedia.
	if home, err := os.UserHomeDir(); err == nil {
		if raw, err := os.ReadFile(filepath.Join(home, ".codex", "config.toml")); err == nil {
			re := regexp.MustCompile(`CODEX_CLI_PATH\s*=\s*"([^"]+)"`)
			if m := re.FindSubmatch(raw); m != nil {
				p := strings.ReplaceAll(string(m[1]), `\\`, `\`)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}
	// 2. Pindai %LOCALAPPDATA%\OpenAI\Codex\bin\*\codex.exe, ambil yang terbaru.
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		return ""
	}
	pattern := filepath.Join(base, "OpenAI", "Codex", "bin", "*", "codex.exe")
	matches, _ := filepath.Glob(pattern)
	best, bestTime := "", time.Time{}
	for _, m := range matches {
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(bestTime) {
			best, bestTime = m, fi.ModTime()
		}
	}
	return best
}

// Run mengeksekusi `codex exec` dan mengembalikan jawaban agen.
// Prompt dikirim lewat STDIN (`codex exec -`) supaya aman terhadap quoting
// shell/cmd.exe dan prompt multi-baris.
func (b *Bridge) Run(ctx context.Context, task string) Result {
	start := time.Now()
	res := Result{}

	bin := b.resolve()
	if bin == "" {
		res.Err = "codex CLI tidak ditemukan (set NOC_CODEX_PATH ke codex.exe)"
		res.ElapsedMS = time.Since(start).Milliseconds()
		return res
	}
	if err := b.ensureGitRepo(); err != nil {
		res.Err = "gagal menyiapkan working dir git: " + err.Error()
		res.ElapsedMS = time.Since(start).Milliseconds()
		return res
	}

	args := []string{"exec", "--skip-git-repo-check", "--sandbox", b.Sandbox}
	// Endpoint override: -c key=value berlaku per-invoke tanpa menyentuh
	// config.toml user. Hanya dikirim bila BaseURL diisi (eksplisit).
	args = append(args, b.endpointOverrides()...)
	if b.Model != "" {
		args = append(args, "-m", b.Model)
	}
	args = append(args, "-") // baca prompt dari stdin

	res.Command = "codex exec --sandbox " + b.Sandbox + " -m " + b.Model + " -"
	ctx, cancel := context.WithTimeout(ctx, b.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = b.WorkingDir
	cmd.Env = append(os.Environ(), "NO_COLOR=1")
	cmd.Stdin = strings.NewReader(task)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	res.ElapsedMS = time.Since(start).Milliseconds()
	res.ExitCode = cmd.ProcessState.ExitCode()

	// PENTING: banner sesi, gema prompt, dan "tokens used" ditulis Codex ke STDERR.
	// STDOUT berisi jawaban akhir agen saja -> selalu utamakan stdout.
	out := strings.TrimSpace(strings.ReplaceAll(stdout.String(), "\r\n", "\n"))
	if out == "" {
		out = clean(stderr.String())
	}
	res.Output = out

	if ctx.Err() == context.DeadlineExceeded {
		res.Err = "codex timeout setelah " + b.Timeout.String()
		return res
	}
	if err != nil && out == "" {
		res.Err = "codex gagal: " + err.Error() + " :: " + tail(clean(stderr.String()), 400)
		return res
	}
	if out == "" {
		res.Err = "codex tidak menghasilkan output"
		return res
	}
	res.OK = true
	return res
}

// endpointOverrides membangun argumen -c untuk mengarahkan Codex ke endpoint
// alternatif tanpa menimpa ~/.codex/config.toml user. Kosong bila BaseURL kosong
// (berarti pakai config.toml seperti biasa).
func (b *Bridge) endpointOverrides() []string {
	if b.BaseURL == "" {
		return nil
	}
	// Nama provider bersifat INTERNAL & arbitrer: flag -c model_providers.<nama>
	// mendefinisikan provider inline, tanpa perlu ada di config.toml. Gunakan
	// nama tetap yang netral (bukan "9router" yang menyesatkan). Operator bisa
	// override via NOC_CODEX_PROVIDER bila butuh nama spesifik.
	provider := b.Provider
	if provider == "" {
		provider = "noc-codex"
	}
	out := []string{
		"-c", "model_provider=" + provider,
		"-c", "model_providers." + provider + ".base_url=" + strings.TrimRight(b.BaseURL, "/"),
		"-c", "model_providers." + provider + ".wire_api=" + b.wire(),
	}
	if b.APIKey != "" {
		out = append(out, "-c", "model_providers."+provider+".http_headers.Authorization=Bearer "+b.APIKey)
	}
	return out
}

// wire menormalkan WireAPI Codex ke salah satu dari chat/responses/messages.
// Codex memahami "responses" (default), "chat" (chat completions), dan
// "messages" (anthropic).
func (b *Bridge) wire() string {
	switch strings.ToLower(strings.TrimSpace(b.WireAPI)) {
	case "chat", "chat_completions":
		return "chat"
	case "messages", "anthropic":
		return "messages"
	default:
		return "responses"
	}
}

func (b *Bridge) ensureGitRepo() error {
	if b.WorkingDir == "" {
		return os.ErrInvalid
	}
	if err := os.MkdirAll(b.WorkingDir, 0o755); err != nil {
		return err
	}
	gitDir := filepath.Join(b.WorkingDir, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		return nil
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = b.WorkingDir
	_ = cmd.Run()
	readme := filepath.Join(b.WorkingDir, "README.md")
	if _, err := os.Stat(readme); err != nil {
		_ = os.WriteFile(readme, []byte("# ai-noc-go escalation workspace\n"), 0o644)
	}
	return nil
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// clean membuang ANSI escape, banner Codex, dan baris ERROR yang tidak relevan,
// lalu mengambil jawaban akhir agen.
func clean(s string) string {
	s = ansiRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "\r\n", "\n")

	var lines []string
	skipPrefixes := []string{
		"--------", "workdir:", "model:", "provider:", "approval:", "sandbox:",
		"reasoning effort:", "reasoning summaries:", "session id:", "tokens used",
		"user", "codex", "ERROR codex_models_manager", "ERROR rmcp::",
	}
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		skip := false
		for _, p := range skipPrefixes {
			if strings.HasPrefix(t, p) {
				skip = true
				break
			}
		}
		if skip || strings.HasPrefix(t, "Reading additional input") {
			continue
		}
		if strings.HasPrefix(t, "2026-") && strings.Contains(t, "ERROR") {
			continue
		}
		lines = append(lines, t)
	}
	if len(lines) == 0 {
		return ""
	}
	// Buang baris prompt (pertanyaan asli) bila ikut tercetak.
	body := lines
	if len(body) > 1 && len(body[0]) < 400 && !strings.Contains(body[0], "VERDICT") {
		// baris pertama biasanya gema prompt; hanya dibuang jika diikuti konten nyata
		if len(body) > 2 {
			body = body[1:]
		}
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
