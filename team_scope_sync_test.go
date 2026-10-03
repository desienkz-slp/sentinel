package main

import (
	"testing"

	"ainoc/internal/diag"
	"ainoc/internal/registry"
	"ainoc/internal/teamscope"
)

// Daftar tool tim CS harus merujuk tool yang BENAR-BENAR ada. Tanpa uji ini,
// salah ketik nama (mis. "tcp_connect" vs "tcp") membuat tool diam-diam tak
// pernah tersedia — atau lebih buruk, tool baru ikut lolos tanpa ditinjau.
func TestDaftarToolCSMerujukToolNyata(t *testing.T) {
	reg := registry.Load("tools/registry.yaml")
	ada := map[string]bool{}
	for _, tl := range reg.All() {
		ada[tl.Name] = true
	}
	for _, tl := range (&diag.Runner{}).Tools() {
		ada[tl.Function.Name] = true
	}
	for _, name := range teamscope.CSToolNames() {
		if !ada[name] {
			t.Errorf("tool CS %q tidak ada di registry maupun diag", name)
		}
	}
}

// Setiap tool registry harus DIKLASIFIKASIKAN eksplisit: boleh untuk CS, atau
// tercatat sebagai cakupan luas/tulis. Tool BARU di registry tanpa klasifikasi
// membuat uji ini gagal — rilis tidak bisa diam-diam memberi CS tool baru.
func TestSetiapToolRegistryDiklasifikasi(t *testing.T) {
	reg := registry.Load("tools/registry.yaml")
	for _, tl := range reg.All() {
		if teamscope.Classified(tl.Name) {
			continue
		}
		t.Errorf("tool registry %q belum diklasifikasi untuk tim CS (tambahkan ke csTools atau scopeless di internal/teamscope)", tl.Name)
	}
}

// Tool tulis tidak boleh pernah ada di daftar CS.
func TestToolTulisTakPernahUntukCS(t *testing.T) {
	reg := registry.Load("tools/registry.yaml")
	for _, tl := range reg.All() {
		if tl.Permission != registry.PermRead && teamscope.CSAllowsTool(tl.Name) {
			t.Errorf("tool tulis %q ada di daftar CS", tl.Name)
		}
	}
}
