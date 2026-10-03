package main

import (
	"context"
	"strings"
	"testing"

	"ainoc/internal/agent"
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

func TestRecordScopeEventKeAuditDanMetrik(t *testing.T) {
	s := routerServer(t, "on")
	s.recordScopeEvent(agent.ScopeEvent{Actor: "628111000111 (customer)", Team: "cs", Tool: "billing.list_customers", Mode: "on", Allowed: false, Reason: "tim CS tidak boleh"})
	s.recordScopeEvent(agent.ScopeEvent{Actor: "628111000111 (customer)", Team: "cs", Tool: "radius.get_session", Mode: "on", Allowed: true, Rewritten: true})
	m := s.teams.Snapshot()
	if m.ScopeDenied != 1 || m.ScopeRewritten != 1 {
		t.Fatalf("metrik scope: denied=%d rewritten=%d, mau 1/1", m.ScopeDenied, m.ScopeRewritten)
	}
	var got []string
	for _, e := range s.aud.Recent(20) {
		if e.EventType == "team_scope" {
			got = append(got, e.Tool+"="+e.PolicyDecision)
		}
	}
	if len(got) != 2 {
		t.Fatalf("audit team_scope = %v, mau 2 entri", got)
	}
	// audit tidak boleh memuat argumen
	for _, e := range s.aud.Recent(20) {
		if e.EventType == "team_scope" && e.Arguments != nil {
			t.Errorf("audit team_scope tidak boleh memuat argumen: %v", e.Arguments)
		}
	}
}

// ---- Fase 3: penyaring presentasi di titik keluar ----

func presenterServer(t *testing.T, mode string) *Server {
	t.Helper()
	s := routerServer(t, "off")
	s.cfg.TeamPresenter = mode
	return s
}

const balasanBocor = "Sudah saya cek ke 172.16.0.10, nomor 628999000111 aktif. Hubungi Staf Uji. Tagihan Rp150.000."

func TestPresenterOffApaAdanya(t *testing.T) {
	s := presenterServer(t, "off")
	cust := s.identifyCaller(context.Background(), "628999000111")
	if got := s.presentReply(cust, balasanBocor); got != balasanBocor {
		t.Fatalf("off harus apa adanya: %q", got)
	}
}

func TestPresenterOnMenyuntingUntukPelanggan(t *testing.T) {
	s := presenterServer(t, "on")
	cust := directory.Caller{Number: "628111000111", Role: directory.RoleCustomer, IsCustomer: true,
		Customer: &directory.CustomerInfo{Phone: "628111000111"}}
	got := s.presentReply(cust, "IP 172.16.0.10, nomor Anda 628111000111, nomor lain 628999000222. Tagihan Rp150.000.")
	for _, bocor := range []string{"172.16.0.10", "628999000222"} {
		if strings.Contains(got, bocor) {
			t.Errorf("masih bocor %q: %q", bocor, got)
		}
	}
	if !strings.Contains(got, "628111000111") {
		t.Errorf("nomor pelanggan sendiri harus tetap ada: %q", got)
	}
	if !strings.Contains(got, "Rp150.000") {
		t.Errorf("angka tagihan sah tidak boleh rusak: %q", got)
	}
	if s.teams.Snapshot().Redactions != 1 {
		t.Errorf("redactions = %d, mau 1", s.teams.Snapshot().Redactions)
	}
}

// Shadow: dicatat, tetapi TEKS ASLI yang dikirim.
func TestPresenterShadowMencatatTanpaMengubah(t *testing.T) {
	s := presenterServer(t, "shadow")
	cust := directory.Caller{Number: "628111000111", Role: directory.RoleCustomer}
	if got := s.presentReply(cust, balasanBocor); got != balasanBocor {
		t.Fatalf("shadow tidak boleh mengubah teks: %q", got)
	}
	if s.teams.Snapshot().Redactions != 1 {
		t.Fatal("shadow harus mencatat penyuntingan")
	}
}

// Staf TIDAK disaring: berhak melihat data internal.
func TestPresenterStafTidakDisaring(t *testing.T) {
	s := presenterServer(t, "on")
	staf := s.identifyCaller(context.Background(), "628111222333")
	if !staf.IsStaff {
		t.Fatal("harus staf")
	}
	if got := s.presentReply(staf, balasanBocor); got != balasanBocor {
		t.Fatalf("balasan staf tidak boleh disaring: %q", got)
	}
}

// Audit hanya memuat KATEGORI, tidak pernah isi yang disunting.
func TestPresenterAuditTanpaIsiAsli(t *testing.T) {
	s := presenterServer(t, "on")
	cust := directory.Caller{Number: "628111000111", Role: directory.RoleCustomer}
	_ = s.presentReply(cust, "alamat 172.16.0.10 dan nomor 628999000222 serta billing.get_customer")
	n := 0
	for _, e := range s.aud.Recent(50) {
		if e.EventType != "reply_redaction" {
			continue
		}
		n++
		for _, bocor := range []string{"172.16.0.10", "628999000222", "billing.get_customer"} {
			if strings.Contains(e.Note, bocor) {
				t.Errorf("audit memuat isi asli %q: %s", bocor, e.Note)
			}
		}
		if !strings.Contains(e.Note, "host_internal") || !strings.Contains(e.Note, "nomor_telepon") {
			t.Errorf("audit harus memuat kategori: %s", e.Note)
		}
	}
	if n != 1 {
		t.Fatalf("entri audit = %d, mau 1", n)
	}
}

func TestPresenterBalasanKosongDanBersih(t *testing.T) {
	s := presenterServer(t, "on")
	cust := directory.Caller{Number: "628111000111", Role: directory.RoleCustomer}
	if got := s.presentReply(cust, ""); got != "" {
		t.Errorf("kosong harus tetap kosong: %q", got)
	}
	bersih := "Koneksi Anda normal, ping 12 ms. Terima kasih."
	if got := s.presentReply(cust, bersih); got != bersih {
		t.Errorf("balasan bersih tidak boleh berubah: %q", got)
	}
	if s.teams.Snapshot().Redactions != 0 {
		t.Error("tidak ada penyuntingan untuk balasan bersih")
	}
}
