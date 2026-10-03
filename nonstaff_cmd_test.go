package main

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ainoc/internal/dedupe"
	"ainoc/internal/handoff"
	"ainoc/internal/wa"
)

// Pelanggan (non-staf) mengirim perintah staf: harus ditolak KODE, tanpa LLM,
// dan ledger tidak berubah. Sebelumnya LLM membalas "tiket sudah ditutup".
func TestNonStafTidakBisaTutupKasus(t *testing.T) {
	s, h := teamTestServer(t)
	s.cfg.TeamRouting = "on"
	s.cfg.TeamHandoff = "on"
	s.cfg.WAAutoReply = true
	s.cfg.OperatorToken = "operator-secret"
	s.ded = &dedupe.Hybrid{Local: dedupe.New(time.Minute, 10), TTL: time.Minute, Mode: func() string { return "off" }}
	s.ho = handoff.New(filepath.Join(t.TempDir(), "h.json"))
	// engine sengaja nil: bila jalur LLM tersentuh, test panik -> gagal.

	body := `{"message_id":"m1","chat_id":"628999000777","sender":"628999000777","message":"tutup CASE-20261003-ABCDEF sudah beres","type":"text"}`
	r := httptest.NewRequest("POST", "http://noc.local/api/wa/webhook", bytes.NewReader([]byte(body)))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = "127.0.0.1:1234"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	out := w.Body.String()
	if !strings.Contains(out, "code-deny") {
		t.Fatalf("harus ditolak oleh kode, dapat: %s", out)
	}
	if !strings.Contains(out, "tim internal") {
		t.Fatalf("balasan penolakan tidak sesuai: %s", out)
	}
	if strings.Contains(strings.ToLower(out), "ditutup") || strings.Contains(out, "ABCDEF") {
		t.Fatalf("tidak boleh mengklaim penutupan / menggaungkan ID kasus: %s", out)
	}
	_ = wa.Reply{}
}
