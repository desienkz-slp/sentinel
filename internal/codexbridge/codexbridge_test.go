package codexbridge

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestCleanStderr memastikan parser membuang banner sesi Codex dan hanya
// menyisakan jawaban agen.
func TestCleanStderr(t *testing.T) {
	raw := "2026-09-30T07:51:25.070184Z ERROR codex_models_manager::cache: failed to load models cache\n" +
		"OpenAI Codex v0.145.0\n" +
		"--------\n" +
		"workdir: C:\\tmp\\x\n" +
		"model: cx/gpt-5.6-terra\n" +
		"provider: 9router\n" +
		"approval: never\n" +
		"sandbox: danger-full-access\n" +
		"reasoning effort: low\n" +
		"reasoning summaries: none\n" +
		"session id: 01a0f151-5fbc-7f72-b0a1-e50c7014596a\n" +
		"--------\n" +
		"user\n" +
		"Cek jaringan\n" +
		"codex\n" +
		"VERDICT: SEHAT\n" +
		"KEYAKINAN: 95\n" +
		"tokens used\n" +
		"11,605\n"

	out := clean(raw)
	if !strings.Contains(out, "VERDICT: SEHAT") || !strings.Contains(out, "KEYAKINAN: 95") {
		t.Errorf("jawaban agen hilang dari hasil bersih:\n%s", out)
	}
	for _, junk := range []string{"OpenAI Codex v", "workdir:", "session id:", "tokens used", "codex_models_manager"} {
		if strings.Contains(out, junk) {
			t.Errorf("hasil bersih masih memuat %q:\n%s", junk, out)
		}
	}
}

// TestResolvePrefersNativeExe: shim npm (.CMD) merusak argumen multi-baris,
// jadi resolve() harus memilih codex.exe native bila ada.
func TestResolvePrefersNativeExe(t *testing.T) {
	native := findNativeCodex()
	if native == "" {
		t.Skip("codex.exe native tidak terpasang di mesin ini")
	}
	if !strings.HasSuffix(strings.ToLower(native), "codex.exe") {
		t.Errorf("findNativeCodex() = %q, mau berakhiran codex.exe", native)
	}
	b := New("codex", "m", "danger-full-access", 5)
	got := b.resolve()
	if strings.HasSuffix(strings.ToLower(got), ".cmd") || strings.HasSuffix(strings.ToLower(got), ".bat") {
		t.Errorf("resolve() memilih shim batch %q padahal ada %q", got, native)
	}
	if !b.Available() {
		t.Error("Available() = false padahal codex.exe ditemukan")
	}
}

func TestUnavailableWhenPathMissing(t *testing.T) {
	b := New("C:/tidak/ada/codex-xyz.exe", "m", "danger-full-access", 5)
	if b.Available() {
		t.Error("Available() = true untuk path yang tidak ada")
	}
	res := b.Run(context.Background(), "tugas")
	if res.OK || res.Err == "" {
		t.Errorf("Run() harus gagal dengan pesan jelas, dapat: %+v", res)
	}
}

func TestTail(t *testing.T) {
	if got := tail("abcdef", 3); got != "def" {
		t.Errorf("tail = %q", got)
	}
	if got := tail("ab", 5); got != "ab" {
		t.Errorf("tail = %q", got)
	}
}

func TestNewDefaults(t *testing.T) {
	b := New("codex", "m", "", 0)
	if b.Sandbox != "danger-full-access" {
		t.Errorf("sandbox default = %q", b.Sandbox)
	}
	if b.Timeout != 180*time.Second {
		t.Errorf("timeout default = %v", b.Timeout)
	}
	if b.WorkingDir == "" {
		t.Error("WorkingDir harus disiapkan")
	}
}
