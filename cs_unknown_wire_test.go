package main

import (
	"context"
	"testing"

	"ainoc/internal/directory"
	"ainoc/internal/memory"
)

// TestLokasiDariPesan: ekstraksi lokasi memakai aturan memory (Jl/Gang/Dusun/
// Desa/RT/RW), dan fallback ke kata pertama untuk input tanpa penanda lokasi.
func TestLokasiDariPesan(t *testing.T) {
	if got := lokasiDariPesan("di Jl. Melati No 5"); !containsStr(got, "melati") {
		t.Errorf("lokasiDariPesan(Jl) = %q, mau memuat 'melati' (penanda dibuang)", got)
	}
	// Tanpa penanda lokasi -> "" (pemanggil yang memutuskan tanya/kueri).
	if got := lokasiDariPesan("internet saya mati total"); got != "" {
		t.Errorf("lokasiDariPesan tanpa penanda = %q, mau kosong", got)
	}
	if got := lokasiDariPesan("RIFKI ZAIN dusun krajan"); got != "krajan" {
		t.Errorf("lokasiDariPesan(dusun krajan) = %q, mau 'krajan'", got)
	}
}

// TestKueriDariBalasanLokasi: fallback 3 kata untuk nama WiFi/area tanpa penanda.
func TestKueriDariBalasanLokasi(t *testing.T) {
	if got := kueriDariBalasanLokasi("RIFKI ZAIN"); got != "RIFKI ZAIN" {
		t.Errorf("kueri = %q, mau RIFKI ZAIN", got)
	}
	if got := kueriDariBalasanLokasi("RIFKI ZAIN dusun krajan"); got != "RIFKI ZAIN dusun" {
		t.Errorf("kueri 3 kata = %q", got)
	}
	if got := kueriDariBalasanLokasi(""); got != "" {
		t.Errorf("kueri kosong = %q", got)
	}
}

// TestBersihkanLokasi: penanda lokasi dibuang supaya jadi kueri billing murni.
func TestBersihkanLokasi(t *testing.T) {
	cases := map[string]string{
		"dusun Jatitengah":     "jatitengah",
		"Jl. Melati No 5":      "melati no 5",
		"dusun krajan":         "krajan",
		"Gang Mawar":           "mawar",
		"Desa Sukamaju":        "sukamaju",
		"Perumahan Griya Asri": "griya asri",
	}
	for in, want := range cases {
		if got := bersihkanLokasi(in); got != want {
			t.Errorf("bersihkanLokasi(%q) = %q, mau %q", in, got, want)
		}
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestResolveUnknownCallerMemintaLokasi: penelepon tak dikenal + keluhan tanpa
// lokasi -> CS meminta lokasi (handled), dan ditandai waiting.
func TestResolveUnknownCallerMemintaLokasi(t *testing.T) {
	s := &Server{
		csUnknown: newCSUnknownState(),
		mem:       memory.New(memory.Config{}),
	}
	caller := directory.Caller{Number: "628999000001", Role: directory.RoleCustomer}

	reply, handled, enriched := s.resolveUnknownCaller(context.Background(), caller, "628999000001", "internet saya mati total")

	if !handled {
		t.Fatal("keluhan tak dikenal tanpa lokasi harus ditangani (minta lokasi)")
	}
	if enriched != nil {
		t.Error("belum ada lokasi -> tidak boleh ada enriched caller")
	}
	if !containsStr(reply, "lokasi") {
		t.Errorf("balasan harus meminta lokasi, dapat: %q", reply)
	}
	if !s.csUnknown.isWaiting("628999000001") {
		t.Error("nomor harus ditandai waiting lokasi")
	}
}

// TestResolveUnknownCallerAbaikanNonKeluhan: sapaan dari nomor tak dikenal
// tidak masuk alur tanya-lokasi (ditangani LLM biasa).
func TestResolveUnknownCallerAbaikanNonKeluhan(t *testing.T) {
	s := &Server{csUnknown: newCSUnknownState(), mem: memory.New(memory.Config{})}
	caller := directory.Caller{Number: "628999000001", Role: directory.RoleCustomer}

	_, handled, _ := s.resolveUnknownCaller(context.Background(), caller, "628999000001", "halo")
	if handled {
		t.Error("sapaan tidak boleh ditangani alur cs-unknown")
	}
}
