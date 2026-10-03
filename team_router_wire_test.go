package main

import (
	"context"
	"testing"

	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/observability"
	"ainoc/internal/teamcorpus"
)

func routerServer(t *testing.T, mode string) *Server {
	t.Helper()
	s := staffCmdServer(t)
	s.cfg.TeamRouting = mode
	s.teams = observability.NewTeamCollector()
	s.syncDirectory()
	billingHTTP(t, s)
	return s
}

func TestRouterModeOffTidakMenghitung(t *testing.T) {
	s := routerServer(t, "off")
	noc := s.identifyCaller(context.Background(), "628111222333")
	// Sengaja SALAH: klaim "llm" untuk pesan yang ditangani kode. Pada mode off
	// tidak boleh tercatat selisih apa pun.
	if !s.compareRouter(noc, "daftar pelanggan", pathLLM) {
		t.Fatal("mode off harus selalu 'sepakat' (tidak membandingkan)")
	}
	if s.teams.Snapshot().Mismatches != 0 {
		t.Fatal("mode off tidak boleh mencatat selisih")
	}
}

func TestRouterShadowMencatatSelisihTanpaMengubahBalasan(t *testing.T) {
	s := routerServer(t, "shadow")
	noc := s.identifyCaller(context.Background(), "628111222333")

	// Sepakat: router bilang kode, nyatanya kode.
	if !s.compareRouter(noc, "daftar pelanggan", pathCode) {
		t.Fatal("seharusnya sepakat")
	}
	if s.teams.Snapshot().Mismatches != 0 {
		t.Fatal("belum ada selisih")
	}
	// Selisih: router bilang kode, nyatanya LLM.
	if s.compareRouter(noc, "daftar pelanggan", pathLLM) {
		t.Fatal("harus terdeteksi selisih")
	}
	if got := s.teams.Snapshot().Mismatches; got != 1 {
		t.Fatalf("selisih = %d, mau 1", got)
	}

	// Balasan nyata tidak terpengaruh mode shadow.
	off := routerServer(t, "off")
	nocOff := off.identifyCaller(context.Background(), "628111222333")
	a, _ := s.handleStaffCommand(context.Background(), noc, "k1", "daftar pelanggan")
	b, _ := off.handleStaffCommand(context.Background(), nocOff, "k1", "daftar pelanggan")
	if a != b || a == "" {
		t.Fatalf("balasan shadow != off:\nshadow=%q\noff=%q", a, b)
	}
}

// Untuk SELURUH korpus, jalur nyata (handleStaffCommand) dan keputusan router
// harus sepakat di mode shadow. Ini syarat lulus Fase 1: nol selisih.
func TestRouterShadowNolSelisihPadaKorpus(t *testing.T) {
	s := routerServer(t, "shadow")
	noc := s.identifyCaller(context.Background(), "628111222333")
	cust := s.identifyCaller(context.Background(), "628999000111")

	for _, k := range teamcorpus.Staf {
		_, handled := s.handleStaffCommand(context.Background(), noc, "sesi-"+k.Nama, k.Pesan)
		actual := pathLLM
		if handled {
			actual = pathCode
		}
		if !s.compareRouter(noc, k.Pesan, actual) {
			t.Errorf("selisih staf [%s] %q", k.Nama, k.Pesan)
		}
	}
	for _, k := range teamcorpus.PelangganKasus {
		_, handled := s.handleStaffCommand(context.Background(), cust, "sesi-c-"+k.Nama, k.Pesan)
		actual := pathLLM
		if handled {
			actual = pathCode
		}
		if !s.compareRouter(cust, k.Pesan, actual) {
			t.Errorf("selisih pelanggan [%s] %q", k.Nama, k.Pesan)
		}
	}
	for _, p := range teamcorpus.KeluhanPanjang {
		_, handled := s.handleStaffCommand(context.Background(), noc, "sesi-kp", p)
		actual := pathLLM
		if handled {
			actual = pathCode
		}
		if !s.compareRouter(noc, p, actual) {
			t.Errorf("selisih keluhan panjang %q", p)
		}
	}
	if m := s.teams.Snapshot().Mismatches; m != 0 {
		t.Fatalf("selisih shadow pada korpus = %d, harus 0", m)
	}
}

func TestRouterModeDinormalisasi(t *testing.T) {
	for in, want := range map[string]config.TeamMode{"": config.TeamOff, "ngawur": config.TeamOff, "shadow": config.TeamShadow, "on": config.TeamOn} {
		s := &Server{cfg: &config.Config{TeamRouting: in}}
		if got := s.routerMode(); got != want {
			t.Errorf("routerMode(%q) = %s, mau %s", in, got, want)
		}
	}
	if (&Server{}).routerMode() != config.TeamOff {
		t.Error("cfg nil harus off")
	}
}

// Pelanggan yang mengetik perintah staf TIDAK mendapat jalur kode di mode apa pun.
func TestRouterPelangganTakPernahJalurKodeDiSemuaMode(t *testing.T) {
	for _, mode := range []string{"off", "shadow", "on"} {
		s := routerServer(t, mode)
		cust := s.identifyCaller(context.Background(), "628999000111")
		if cust.IsStaff {
			t.Fatal("nomor uji pelanggan tidak boleh staf")
		}
		for _, p := range []string{"daftar pelanggan isolir", "cek billing pelanggan-uji", "sudah bisa terhubung ke billing?", "riwayat pelanggan-uji"} {
			if text, handled := s.handleStaffCommand(context.Background(), cust, "x", p); handled {
				t.Errorf("mode %s: pelanggan %q ditangani jalur staf: %q", mode, p, text)
			}
		}
	}
	_ = directory.RoleCustomer
}
