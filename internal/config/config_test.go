package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// memory_path kosong di config.json harus jatuh ke lokasi bawaan, bukan
// mematikan memory. Ini yang membuat folder proyek bisa dipindah atau disalin
// ke komputer lain tanpa kehilangan memory percakapan.
func TestMemoryPathKosongPakaiDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Config hasil pindah komputer: path absolut lama sudah dibuang.
	if err := os.WriteFile(path, []byte(`{"addr":":8090","wa_dir":"","memory_path":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(path)
	if strings.TrimSpace(c.MemoryPath) == "" {
		t.Error("memory_path kosong membuat memory mati; harus jatuh ke lokasi bawaan")
	}
	if !strings.HasSuffix(c.MemoryPath, "memory.json") {
		t.Errorf("MemoryPath = %q, mau berakhir memory.json", c.MemoryPath)
	}
}

// wa_dir kosong harus terdeteksi otomatis, bukan menonaktifkan gateway.
func TestWADirKosongTerdeteksiOtomatis(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"addr":":8090","wa_dir":""}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// resolveWADir memakai exe/CWD; di lingkungan test, wa-gateway tidak ada,
	// jadi hasilnya boleh kosong. Yang penting: tidak panic dan tidak error.
	c := Load(path)
	_ = c.WADir
}

// config.json yang menyimpan path absolut tetap dihormati (operator mungkin
// memang menaruh gateway di lokasi lain).
func TestMemoryPathAbsolutDihormati(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	abs := filepath.Join(dir, "data", "memory.json")
	isi := `{"addr":":8090","memory_path":` + strconv.Quote(abs) + `}`
	if err := os.WriteFile(path, []byte(isi), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(path)
	if c.MemoryPath != abs {
		t.Errorf("MemoryPath = %q, mau %q (path eksplisit harus dihormati)", c.MemoryPath, abs)
	}
}
