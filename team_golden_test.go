package main

import (
	"context"
	"strings"
	"testing"

	"ainoc/internal/directory"
	"ainoc/internal/teamcorpus"
)

// jalurSaatIni menentukan jalur penanganan menurut KODE SEKARANG (v0.2.21),
// langsung dari parser/handler asli — bukan salinan aturan. Tes golden ini
// mengunci perilaku itu sebelum router (Fase 1) mengambil alih pemilahan.
func jalurSaatIni(s *Server, caller directory.Caller, pesan string) teamcorpus.Handler {
	if !caller.IsStaff {
		return teamcorpus.HLLM // pelanggan tak pernah masuk jalur perintah staf
	}
	text, handled := s.handleStaffCommand(context.Background(), caller, "sesi-golden", pesan)
	if !handled {
		return teamcorpus.HLLM
	}
	switch {
	case strings.HasPrefix(text, "*Status integrasi*"):
		return teamcorpus.HStatusIntegrasi
	default:
		if _, ok := directory.ParseListQuery(pesan); ok {
			return teamcorpus.HDaftarPelanggan
		}
		return teamcorpus.HPerintahCek
	}
}

func TestGoldenKorpusStaf(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	billingHTTP(t, s)
	noc := s.identifyCaller(context.Background(), "628111222333")
	if !noc.IsStaff {
		t.Fatal("penelepon uji harus staf")
	}
	for _, k := range teamcorpus.Staf {
		if got := jalurSaatIni(s, noc, k.Pesan); got != k.Harapan {
			t.Errorf("[%s] %q -> %s, mau %s (%s)", k.Nama, k.Pesan, got, k.Harapan, k.Catatan)
		}
	}
}

func TestGoldenKorpusPelangganTakPernahJalurStaf(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	billingHTTP(t, s)
	// nomor di luar direktori staf = pelanggan/tak dikenal
	cust := s.identifyCaller(context.Background(), "628999000111")
	if cust.IsStaff {
		t.Fatal("nomor uji pelanggan tidak boleh dikenali sebagai staf")
	}
	for _, k := range teamcorpus.PelangganKasus {
		if text, handled := s.handleStaffCommand(context.Background(), cust, "sesi-cust", k.Pesan); handled {
			t.Errorf("[%s] pelanggan %q DITANGANI jalur staf: %q", k.Nama, k.Pesan, text)
		}
	}
}

func TestGoldenKeluhanPanjangBukanPerintah(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	billingHTTP(t, s)
	noc := s.identifyCaller(context.Background(), "628111222333")
	for _, pesan := range teamcorpus.KeluhanPanjang {
		if text, handled := s.handleStaffCommand(context.Background(), noc, "sesi-kp", pesan); handled {
			t.Errorf("keluhan %q terbaca sebagai perintah staf: %q", pesan, text)
		}
		if cmd := directory.ParseCommand(pesan); cmd.Kind != directory.CmdNone {
			t.Errorf("ParseCommand(%q) = %+v, harus CmdNone", pesan, cmd)
		}
		if _, ok := directory.ParseListQuery(pesan); ok {
			t.Errorf("ParseListQuery(%q) harus false", pesan)
		}
		if d, all := directory.ParseStatusQuestion(pesan); len(d) > 0 || all {
			t.Errorf("ParseStatusQuestion(%q) harus kosong", pesan)
		}
	}
}

// Korpus tidak boleh berisi data nyata: tak ada nomor 62xxxxxxxxxx panjang dan
// tak ada JID.
func TestKorpusTanpaDataNyata(t *testing.T) {
	all := append(append([]teamcorpus.Kasus{}, teamcorpus.Staf...), teamcorpus.PelangganKasus...)
	for _, k := range all {
		for _, bad := range []string{"@s.whatsapp.net", "@lid", "@g.us", "628"} {
			if strings.Contains(k.Pesan, bad) {
				t.Errorf("[%s] pesan memuat %q (dilarang di korpus)", k.Nama, bad)
			}
		}
	}
}
