package supervisor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNewDefaults(t *testing.T) {
	m := New("", 0, nil, 0)
	if m.Port != 3001 {
		t.Errorf("port default = %d, mau 3001", m.Port)
	}
	if m.LogLines != 80 {
		t.Errorf("LogLines default = %d, mau 80", m.LogLines)
	}
	if m.BaseURL() != "http://127.0.0.1:3001" {
		t.Errorf("BaseURL = %q", m.BaseURL())
	}
}

func TestBaseURLUsesPort(t *testing.T) {
	m := New("x", 3099, nil, 10)
	if m.BaseURL() != "http://127.0.0.1:3099" {
		t.Errorf("BaseURL = %q", m.BaseURL())
	}
}

// Present harus false bila app/server.js tidak ada — ini yang mencegah
// supervisor mencoba menjalankan folder kosong.
func TestPresentRequiresServerJs(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, 3001, nil, 10)
	if m.Present() {
		t.Error("Present() = true padahal app/server.js belum ada")
	}

	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "server.js"), []byte("//x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !m.Present() {
		t.Error("Present() = false padahal app/server.js sudah ada")
	}
}

func TestStatusDisabledWhenAbsent(t *testing.T) {
	m := New(t.TempDir(), 3001, nil, 10)
	st := m.Status(context.Background())
	if st.State != StateDisabled {
		t.Errorf("state = %q, mau %q", st.State, StateDisabled)
	}
	if st.Installed {
		t.Error("Installed = true padahal node_modules tidak ada")
	}
}

// Start harus menolak dengan pesan jelas bila folder gateway tidak ada,
// bukan menggantung atau panic.
func TestStartFailsWithoutDir(t *testing.T) {
	m := New(t.TempDir(), 3001, nil, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := m.Start(ctx); err == nil {
		t.Error("Start() harus gagal bila source gateway tidak ada")
	}
}

// Stop pada manager yang belum pernah jalan harus aman (idempoten).
func TestStopIdempotent(t *testing.T) {
	m := New(t.TempDir(), 3001, nil, 10)
	if err := m.Stop(); err != nil {
		t.Errorf("Stop() pertama error: %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Errorf("Stop() kedua error: %v", err)
	}
	if st := m.Status(context.Background()); st.State != StateDisabled {
		t.Errorf("state = %q", st.State)
	}
}

// Log harus dibatasi jumlah barisnya supaya memori tidak tumbuh tanpa batas.
func TestLogRingBuffer(t *testing.T) {
	m := New("x", 3001, nil, 5)
	for i := 0; i < 50; i++ {
		m.log("baris")
	}
	if len(m.logs) != 5 {
		t.Errorf("panjang log = %d, mau 5", len(m.logs))
	}
}

func TestDepsInstalledDetection(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, 3001, nil, 10)
	if m.depsInstalled() {
		t.Error("depsInstalled() = true padahal node_modules belum ada")
	}
	if err := os.MkdirAll(filepath.Join(dir, "node_modules", "@whiskeysockets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !m.depsInstalled() {
		t.Error("depsInstalled() = false padahal @whiskeysockets ada")
	}
}

// KredensialLengkap mendeteksi sesi WhatsApp yang sudah tertaut lewat field
// "me" di creds.json. Flag "registered" TIDAK dipakai karena tetap false pada
// Baileys 6.x walau sesi sudah CONNECTED.
func TestKredensialLengkap(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, 3001, nil, 10)

	if m.KredensialLengkap() {
		t.Error("KredensialLengkap() = true padahal creds.json belum ada")
	}

	authDir := filepath.Join(dir, "data", "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		nama    string
		isi     string
		harusOK bool
	}{
		{"tertaut (registered false)", `{"registered":false,"me":{"id":"628000000000:79@s.whatsapp.net"}}`, true},
		{"tertaut dengan registered true", `{"registered":true,"me":{"id":"628@s.whatsapp.net"}}`, true},
		{"belum tertaut", `{"registered":false,"me":null}`, false},
		{"me tanpa id", `{"me":{"id":""}}`, false},
		{"tanpa field me", `{"noiseKey":"x"}`, false},
		{"bukan json", `bukan-json`, false},
	}
	for _, c := range cases {
		t.Run(c.nama, func(t *testing.T) {
			if err := os.WriteFile(filepath.Join(authDir, "creds.json"), []byte(c.isi), 0o644); err != nil {
				t.Fatal(err)
			}
			if got := m.KredensialLengkap(); got != c.harusOK {
				t.Errorf("KredensialLengkap() = %v, mau %v", got, c.harusOK)
			}
		})
	}
}

// Status harus melaporkan status kredensial supaya dashboard bisa memperingatkan
// sebelum meminta scan QR ulang.
func TestStatusReportsCredentials(t *testing.T) {
	dir := t.TempDir()
	m := New(dir, 3001, nil, 10)
	if st := m.Status(context.Background()); st.Kredensial {
		t.Error("Status().Kredensial = true padahal belum tertaut")
	}
	authDir := filepath.Join(dir, "data", "auth")
	if err := os.MkdirAll(authDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "creds.json"),
		[]byte(`{"me":{"id":"628000000000:79@s.whatsapp.net"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if st := m.Status(context.Background()); !st.Kredensial {
		t.Error("Status().Kredensial = false padahal sesi tertaut")
	}
}

// StartAman pada folder gateway yang tidak ada harus gagal dengan jelas.
func TestStartAmanFailsWithoutDir(t *testing.T) {
	m := New(t.TempDir(), 3001, nil, 10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.StartAman(ctx); err == nil {
		t.Error("StartAman() harus gagal bila source gateway tidak ada")
	}
}
