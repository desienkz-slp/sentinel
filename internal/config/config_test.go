package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Save harus menulis ke file yang SAMA dengan yang di-load. Tanpa ini,
// pengaturan dari dashboard hanya hidup di memori dan hilang saat restart.
func TestSaveMenulisKeFileYangSama(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	awal := `{"addr":":8090","llm_model":"ag/gemini-3.8-flash-high","wa_allowlist":[]}`
	if err := os.WriteFile(path, []byte(awal), 0o644); err != nil {
		t.Fatal(err)
	}

	c := Load(path)
	if c.Path() != path {
		t.Fatalf("Path()=%q, mau %q", c.Path(), path)
	}

	// Ubah seperti yang dilakukan dashboard, lalu simpan.
	c.WAAllowlist = []string{"628123456789", "628999888777"}
	c.WAGroup = true
	if err := c.Save(); err != nil {
		t.Fatalf("Save gagal: %v", err)
	}

	// Baca ulang dari disk: nilai baru harus bertahan.
	c2 := Load(path)
	if len(c2.WAAllowlist) != 2 || c2.WAAllowlist[0] != "628123456789" {
		t.Errorf("allowlist tidak tersimpan: %v", c2.WAAllowlist)
	}
	if !c2.WAGroup {
		t.Error("wa_group tidak tersimpan")
	}
	// Field lain tidak boleh hilang saat ditulis ulang.
	if c2.Addr != ":8090" {
		t.Errorf("addr hilang/berubah: %q", c2.Addr)
	}
	if c2.LLMModel != "ag/gemini-3.8-flash-high" {
		t.Errorf("llm_model hilang/berubah: %q", c2.LLMModel)
	}
}

// Save tanpa file config harus mengembalikan error yang jelas, bukan panic
// dan bukan diam-diam berhasil.
func TestSaveTanpaPathError(t *testing.T) {
	c := Default()
	if err := c.Save(); err == nil {
		t.Fatal("Save tanpa path harus error")
	}
}

// File sementara tidak boleh tertinggal setelah Save berhasil.
func TestSaveTidakMeninggalkanFileTmp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"addr":":8090"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(path)
	c.MaxSteps = 12
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("file .tmp masih ada setelah Save")
	}
}

// Hasil Save harus JSON yang valid dan bisa dibaca kembali.
func TestSaveHasilJSONValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(path)
	c.WAAllowlist = []string{"628111"}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("hasil Save bukan JSON valid: %v\n%s", err, b)
	}
	// Field privat (path) tidak boleh ikut tertulis ke file.
	if _, ada := m["path"]; ada {
		t.Error("field privat 'path' ikut tertulis ke config.json")
	}
}
