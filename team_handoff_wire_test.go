package main

import (
	"ainoc/internal/incident"
	"ainoc/internal/severity"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ainoc/internal/agent"
	"ainoc/internal/audit"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/escalation"
	"ainoc/internal/handoff"
	"ainoc/internal/observability"
	"ainoc/internal/wa"
)

// gateway WA palsu: merekam semua pesan keluar.
type fakeGW struct {
	mu   sync.Mutex
	sent []map[string]string
	fail bool
	srv  *httptest.Server
}

func newFakeGW(t *testing.T) *fakeGW {
	g := &fakeGW{}
	g.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var b map[string]string
		_ = json.NewDecoder(r.Body).Decode(&b)
		g.mu.Lock()
		defer g.mu.Unlock()
		if g.fail {
			http.Error(w, "gateway down", 503)
			return
		}
		g.sent = append(g.sent, b)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(g.srv.Close)
	return g
}

func (g *fakeGW) messages() []map[string]string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]map[string]string(nil), g.sent...)
}

const (
	nomorNOC   = "628111222333"
	nomorAdmin = "628111444555"
	nomorPel   = "628111000111"
	caseJar    = "CASE-20261003-ABC234"
	caseBil    = "CASE-20261003-XYZ567"
)

func handoffServer(t *testing.T, mode string) (*Server, *fakeGW) {
	t.Helper()
	s := staffCmdServer(t)
	s.cfg.StaffMembers = append(s.cfg.StaffMembers,
		config.StaffMember{Number: nomorAdmin, Name: "Admin Uji", Role: "admin", Active: true})
	s.cfg.TeamHandoff = mode
	s.cfg.WATimeout = 5
	s.teams = observability.NewTeamCollector()
	s.syncDirectory()
	gw := newFakeGW(t)
	s.wa = wa.New(gw.srv.URL, 5)
	s.ho = handoff.New(filepath.Join(t.TempDir(), "handoffs.json"))
	s.esc = nil
	return s, gw
}

func bukaKasus(s *Server, id, domain string) {
	s.ho.Open(handoff.Handoff{CaseID: id, Customer: nomorPel, Domain: domain, Complaint: "internet mati"})
}

func balas(s *Server, nomor, pesan string) string {
	c := s.identifyCaller(context.Background(), nomor)
	text, _ := s.handleStaffCommand(context.Background(), c, "k", pesan)
	return text
}

// ---- mode ----

func TestHandoffOffTidakMenyentuhApaPun(t *testing.T) {
	s, gw := handoffServer(t, "off")
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar+" Sudah normal")
	if !strings.Contains(got, "belum diaktifkan") {
		t.Fatalf("balasan: %q", got)
	}
	if len(gw.messages()) != 0 {
		t.Fatal("mode off tidak boleh mengirim apa pun")
	}
	if h, _ := s.ho.Get(caseJar); h.Status != handoff.StatusOpen {
		t.Fatal("kasus tidak boleh berubah saat off")
	}
	// openHandoff juga no-op saat off
	s.openHandoff(agent.Report{CaseID: "CASE-X", CaseState: escalationState}, nomorPel, "network")
	if _, ok := s.ho.Get("CASE-X"); ok {
		t.Fatal("openHandoff harus no-op saat off")
	}
}

func TestHandoffShadowMencatatTanpaMengirim(t *testing.T) {
	s, gw := handoffServer(t, "shadow")
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar+" Gangguan sudah diperbaiki")
	if !strings.Contains(got, "TIDAK dikirim") {
		t.Fatalf("balasan: %q", got)
	}
	if len(gw.messages()) != 0 {
		t.Fatal("shadow tidak boleh mengirim ke pelanggan")
	}
	if h, _ := s.ho.Get(caseJar); h.Status != handoff.StatusClosed {
		t.Fatalf("shadow tetap mencatat: %s", h.Status)
	}
}

func TestHandoffOnMengirimHanyaUntukPelanggan(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar+" Gangguan sudah diperbaiki, silakan dicoba lagi.")
	if !strings.Contains(got, "sudah dikirim ke pelanggan") {
		t.Fatalf("balasan staf: %q", got)
	}
	msgs := gw.messages()
	if len(msgs) != 1 {
		t.Fatalf("pesan keluar = %d, mau 1", len(msgs))
	}
	if msgs[0]["to"] != nomorPel {
		t.Errorf("tujuan = %q", msgs[0]["to"])
	}
	if msgs[0]["message"] != "Gangguan sudah diperbaiki, silakan dicoba lagi." {
		t.Errorf("isi = %q", msgs[0]["message"])
	}
	h, _ := s.ho.Get(caseJar)
	if len(h.Updates) != 1 || !h.Updates[0].Notified {
		t.Fatalf("update harus tercatat terkirim: %+v", h.Updates)
	}
	snap := s.teams.Snapshot().Handoffs
	if snap["updated"] != 1 || snap["notified"] != 1 {
		t.Errorf("metrik = %+v", snap)
	}
}

// Inti keamanan: pesan staf yang salah ketik tidak boleh membocorkan data.
func TestHandoffPesanPelangganDisaring(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "tutup "+caseJar+" Sudah normal. Cek router 172.16.0.10 atau hubungi 628999000222")
	msgs := gw.messages()
	if len(msgs) != 1 {
		t.Fatalf("pesan = %d", len(msgs))
	}
	for _, bocor := range []string{"172.16.0.10", "628999000222"} {
		if strings.Contains(msgs[0]["message"], bocor) {
			t.Errorf("bocor %q: %q", bocor, msgs[0]["message"])
		}
	}
	if !strings.Contains(msgs[0]["message"], "Sudah normal") {
		t.Errorf("isi sah hilang: %q", msgs[0]["message"])
	}
}

// Disaring walau presenter sendiri off: ini pesan hasil ketikan staf.
func TestHandoffDisaringWalauPresenterOff(t *testing.T) {
	s, gw := handoffServer(t, "on")
	s.cfg.TeamPresenter = "off"
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "tutup "+caseJar+" Router 172.16.0.10 sudah diganti")
	if m := gw.messages(); len(m) != 1 || strings.Contains(m[0]["message"], "172.16.0.10") {
		t.Fatalf("pesan = %+v", m)
	}
	if s.cfg.TeamPresenter != "off" {
		t.Fatal("pengaturan presenter tidak boleh berubah")
	}
}

// ---- aturan ----

func TestHandoffTutupTanpaPesanDitolak(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar)
	if !strings.Contains(got, "wajib menyertakan pesan") {
		t.Fatalf("balasan: %q", got)
	}
	if len(gw.messages()) != 0 {
		t.Fatal("tidak boleh ada pesan")
	}
	if h, _ := s.ho.Get(caseJar); h.Status != handoff.StatusOpen {
		t.Fatal("kasus tidak boleh tertutup")
	}
}

func TestHandoffKirimGandaTidakTerjadi(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "update "+caseJar+" Sedang kami periksa")
	got := balas(s, nomorNOC, "update "+caseJar+" Sedang kami periksa")
	if !strings.Contains(got, "tidak dikirim ulang") {
		t.Fatalf("balasan: %q", got)
	}
	if n := len(gw.messages()); n != 1 {
		t.Fatalf("pesan = %d, mau 1", n)
	}
}

func TestHandoffKasusTertutupTidakBisaDiubah(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "tutup "+caseJar+" Selesai ya")
	got := balas(s, nomorNOC, "update "+caseJar+" Ada lagi")
	if !strings.Contains(got, "sudah ditutup") {
		t.Fatalf("balasan: %q", got)
	}
	if n := len(gw.messages()); n != 1 {
		t.Fatalf("pesan = %d", n)
	}
}

func TestHandoffKasusTakDikenal(t *testing.T) {
	s, gw := handoffServer(t, "on")
	got := balas(s, nomorNOC, "tutup CASE-20261003-ZZZ222 ok")
	if !strings.Contains(got, "tidak ditemukan") || len(gw.messages()) != 0 {
		t.Fatalf("balasan: %q pesan=%d", got, len(gw.messages()))
	}
}

// Otorisasi per domain: NOC hanya jaringan, Admin selain jaringan.
func TestHandoffOtorisasiPerDomain(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	bukaKasus(s, caseBil, "billing")

	if got := balas(s, nomorAdmin, "tutup "+caseJar+" coba"); !strings.Contains(got, "tidak berwenang") {
		t.Errorf("Admin tak boleh menutup kasus jaringan: %q", got)
	}
	if got := balas(s, nomorNOC, "tutup "+caseBil+" coba"); !strings.Contains(got, "tidak berwenang") {
		t.Errorf("NOC tak boleh menutup kasus billing: %q", got)
	}
	if len(gw.messages()) != 0 {
		t.Fatal("penolakan tak boleh mengirim pesan")
	}
	if got := balas(s, nomorAdmin, "tutup "+caseBil+" Tagihan sudah dikoreksi"); !strings.Contains(got, "sudah dikirim") {
		t.Errorf("Admin boleh menutup kasus billing: %q", got)
	}
	if got := balas(s, nomorNOC, "tutup "+caseJar+" Sudah normal"); !strings.Contains(got, "sudah dikirim") {
		t.Errorf("NOC boleh menutup kasus jaringan: %q", got)
	}
	// penolakan tercatat di audit, tanpa isi pesan
	n := 0
	for _, e := range s.aud.Recent(100) {
		if e.EventType == "handoff_update_denied" {
			n++
			if strings.Contains(e.Note, "coba") {
				t.Errorf("audit tak boleh memuat isi pesan: %s", e.Note)
			}
		}
	}
	if n != 2 {
		t.Errorf("entri penolakan = %d, mau 2", n)
	}
}

// Pelanggan tidak bisa menjalankan perintah ini: bukan staf -> tak masuk jalur kode.
func TestHandoffPelangganTidakBisa(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorPel, "tutup "+caseJar+" saya yang tutup")
	if got != "" {
		t.Fatalf("pelanggan tidak boleh ditangani jalur staf: %q", got)
	}
	if h, _ := s.ho.Get(caseJar); h.Status != handoff.StatusOpen || len(gw.messages()) != 0 {
		t.Fatal("kasus tak boleh berubah")
	}
	if canUpdateHandoff(directory.Caller{Number: nomorPel, Role: directory.RoleCustomer}, "network") {
		t.Fatal("pelanggan tidak berwenang")
	}
}

func TestHandoffGatewayGagalDicatat(t *testing.T) {
	s, gw := handoffServer(t, "on")
	gw.fail = true
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "tutup "+caseJar+" Sudah normal")
	if !strings.Contains(got, "GAGAL terkirim") {
		t.Fatalf("staf harus tahu gagal: %q", got)
	}
	h, _ := s.ho.Get(caseJar)
	if len(h.Updates) != 1 || h.Updates[0].Notified || h.Updates[0].NotifyErr == "" {
		t.Fatalf("kegagalan harus tercatat: %+v", h.Updates)
	}
	if s.teams.Snapshot().Handoffs["notify_failed"] != 1 {
		t.Errorf("metrik = %+v", s.teams.Snapshot().Handoffs)
	}
}

// Pembaruan tanpa kalimat untuk pelanggan: tercatat, tak ada pesan.
func TestHandoffUpdateTanpaPesanTidakMengirim(t *testing.T) {
	s, gw := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	got := balas(s, nomorNOC, "update "+caseJar+" lapangan")
	if !strings.Contains(got, "Tidak ada pesan untuk pelanggan") || len(gw.messages()) != 0 {
		t.Fatalf("balasan: %q pesan=%d", got, len(gw.messages()))
	}
	if h, _ := s.ho.Get(caseJar); h.Status != handoff.StatusNeedField {
		t.Fatalf("status = %s", h.Status)
	}
}

// ---- pembukaan saat eskalasi ----

func TestOpenHandoffSaatEskalasi(t *testing.T) {
	s, _ := handoffServer(t, "shadow")
	rep := agent.Report{CaseID: "CASE-20261003-NEW234", CaseState: escalationState, Query: "internet mati total",
		Steps: []agent.Step{{Kind: "tool", Tool: "radius.get_session", OK: true, Output: "sesi tidak ada"}}}
	s.openHandoff(rep, nomorPel, "network")
	s.openHandoff(rep, nomorPel, "network") // idempoten
	h, ok := s.ho.Get(rep.CaseID)
	if !ok {
		t.Fatal("handoff harus dibuka")
	}
	if h.Evidence["radius"] != "sesi tidak ada" {
		t.Errorf("bukti radius = %q", h.Evidence["radius"])
	}
	for _, sys := range []string{"billing", "mikrotik", "genieacs"} {
		if h.Evidence[sys] != handoff.Unknown {
			t.Errorf("bukti %s = %q, mau UNKNOWN", sys, h.Evidence[sys])
		}
	}
	if s.teams.Snapshot().Handoffs["opened"] != 1 {
		t.Errorf("opened = %d, mau 1 (idempoten)", s.teams.Snapshot().Handoffs["opened"])
	}
	// bukan ESCALATION -> tidak dibuka
	s.openHandoff(agent.Report{CaseID: "CASE-LAIN", CaseState: "CONVERSATION"}, nomorPel, "network")
	if _, ok := s.ho.Get("CASE-LAIN"); ok {
		t.Fatal("hanya state ESCALATION yang membuka handoff")
	}
}

// Audit memuat ID kasus + status, TIDAK memuat teks pesan ke pelanggan.
func TestHandoffAuditTanpaIsiPesan(t *testing.T) {
	s, _ := handoffServer(t, "on")
	bukaKasus(s, caseJar, "network")
	balas(s, nomorNOC, "tutup "+caseJar+" RAHASIA-ISI-PESAN-123")
	adaUpdate := false
	for _, e := range s.aud.Recent(100) {
		if e.EventType == "handoff_update" || e.EventType == "handoff_notify" {
			adaUpdate = true
			if strings.Contains(e.Note, "RAHASIA-ISI-PESAN") {
				t.Errorf("audit memuat isi pesan: %s", e.Note)
			}
		}
	}
	if !adaUpdate {
		t.Fatal("audit harus mencatat update")
	}
	_ = audit.Entry{}
}

// Pesan eskalasi ke staf memuat cara membalas HANYA saat handoff aktif;
// saat off, pesan identik dengan perilaku lama.
func TestPesanEskalasiPetunjukBalas(t *testing.T) {
	for _, tc := range []struct {
		mode     string
		petunjuk bool
	}{{"off", false}, {"shadow", true}, {"on", true}} {
		s, gw := handoffServer(t, tc.mode)
		s.esc = escalationDedupForTest()
		rep := agent.Report{CaseID: "CASE-20261003-PET234", CaseState: escalationState, Query: "internet mati"}
		s.deliverEscalation(context.Background(), rep, nomorPel, "network")
		msgs := gw.messages()
		if len(msgs) != 1 {
			t.Fatalf("[%s] pesan = %d, mau 1 (ke NOC Senior)", tc.mode, len(msgs))
		}
		got := strings.Contains(msgs[0]["message"], "tutup CASE-20261003-PET234")
		if got != tc.petunjuk {
			t.Errorf("[%s] petunjuk=%v, mau %v:\n%s", tc.mode, got, tc.petunjuk, msgs[0]["message"])
		}
		if tc.mode == "off" {
			if _, ok := s.ho.Get(rep.CaseID); ok {
				t.Error("off: handoff tidak boleh dicatat")
			}
		} else if _, ok := s.ho.Get(rep.CaseID); !ok {
			t.Errorf("[%s] handoff harus dicatat saat eskalasi", tc.mode)
		}
	}
}

func escalationDedupForTest() *escalation.Dedup { return escalation.NewDedup() }

func eskalasiBaru(s *Server, id string, n int) agent.Report {
	if s.inc == nil {
		s.inc = incident.New("", 100)
	}
	for i := 0; i < n; i++ {
		s.inc.Add(incident.Incident{ID: fmt.Sprintf("I%s%d", id, i), Identity: fmt.Sprintf("62811100%04d", i),
			Intent: "no_internet", Status: incident.StatusInvestigating, StartedAt: time.Now()})
	}
	return agent.Report{CaseID: id, CaseState: escalationState, Query: "mati", Intent: "no_internet"}
}

// Fixture uji, bukan rekomendasi: mencerminkan kebijakan operator yang disetujui.
var kebijakanUji = severity.Policy{
	P3Customers: 3,
	Floor:       "P4",
	ScopeFloor:  map[string]string{"area": "P2", "olt": "P2", "upstream": "P1"},
}

func rekamGrupMassTerverifikasi(s *Server, incidentID string, topology incident.Topology) {
	s.inc = incident.New("", 100)
	now := time.Now().UTC()
	for i, identity := range []string{"628111000001", "628111000002", "628111000003"} {
		id := fmt.Sprintf("INC-MASS-%d", i)
		if i == 2 {
			id = incidentID
		}
		s.inc.Record(incident.Incident{
			ID: id, Identity: identity, Intent: "no_internet", Status: incident.StatusInvestigating,
			Topology: topology, StartedAt: now.Add(time.Duration(i) * time.Minute),
		}, incident.CorrelationPolicy{Window: 15 * time.Minute, MassThreshold: 3})
	}
}

func TestSeverityOffTidakMengisi(t *testing.T) {
	s, _ := handoffServer(t, "shadow")
	s.cfg.TeamSeverity = "off"
	s.cfg.SeverityPolicy = kebijakanUji
	s.openHandoff(eskalasiBaru(s, "CASE-20261003-SV0234", 5), nomorPel, "network")
	if h, _ := s.ho.Get("CASE-20261003-SV0234"); h.Severity != "" {
		t.Fatalf("off: severity harus kosong, dapat %q", h.Severity)
	}
}

func TestSeverityTanpaKebijakanUnrated(t *testing.T) {
	s, _ := handoffServer(t, "shadow")
	s.cfg.TeamSeverity = "shadow"
	s.openHandoff(eskalasiBaru(s, "CASE-20261003-SV1234", 9), nomorPel, "network")
	if h, _ := s.ho.Get("CASE-20261003-SV1234"); h.Severity != "UNRATED" {
		t.Fatalf("tanpa kebijakan harus UNRATED, dapat %q", h.Severity)
	}
}

func TestSeverityDariGrupMassTerverifikasi(t *testing.T) {
	for _, tc := range []struct {
		name     string
		topology incident.Topology
		want     string
	}{
		{name: "pon tiga pelanggan", topology: incident.Topology{Area: "Cimahi", OLT: "OLT-1", PON: "PON-7"}, want: "P3"},
		{name: "olt", topology: incident.Topology{Area: "Cimahi", OLT: "OLT-1"}, want: "P2"},
		{name: "area", topology: incident.Topology{Area: "Cimahi"}, want: "P2"},
		{name: "upstream", topology: incident.Topology{Upstream: "Transit-A"}, want: "P1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := handoffServer(t, "shadow")
			s.cfg.TeamSeverity = "on"
			s.cfg.SeverityPolicy = kebijakanUji
			incidentID := "INC-" + strings.ReplaceAll(tc.name, " ", "-")
			caseID := "CASE-20261003-" + strings.ToUpper(strings.ReplaceAll(tc.name, " ", ""))
			rekamGrupMassTerverifikasi(s, incidentID, tc.topology)

			// Teks LLM dan hitungan intent-global tidak boleh menentukan severity.
			s.openHandoff(agent.Report{ID: incidentID, CaseID: caseID, CaseState: escalationState,
				Intent: "no_internet", Query: "LLM mengklaim P1", Answer: "P1"}, nomorPel, "network")
			if h, _ := s.ho.Get(caseID); h.Severity != tc.want {
				t.Fatalf("severity = %q, mau %s", h.Severity, tc.want)
			}
		})
	}
}

func TestSeverityTanpaGrupMassTerverifikasiUnrated(t *testing.T) {
	s, _ := handoffServer(t, "shadow")
	s.cfg.TeamSeverity = "on"
	s.cfg.SeverityPolicy = kebijakanUji
	s.inc = incident.New("", 100)
	for i := 0; i < 10; i++ {
		s.inc.Add(incident.Incident{ID: fmt.Sprintf("INC-GLOBAL-%d", i), Identity: fmt.Sprintf("6281119%03d", i),
			Intent: "no_internet", Status: incident.StatusInvestigating, StartedAt: time.Now()})
	}

	s.openHandoff(agent.Report{ID: "INC-NO-GROUP", CaseID: "CASE-20261003-NOGROUP", CaseState: escalationState,
		Intent: "no_internet", Query: "LLM mengklaim P1", Answer: "P1"}, nomorPel, "network")
	if h, _ := s.ho.Get("CASE-20261003-NOGROUP"); h.Severity != "UNRATED" {
		t.Fatalf("tanpa grup/scope terverifikasi severity harus UNRATED, dapat %q", h.Severity)
	}
}

func TestSeverityPolicyDitolakBilaSalah(t *testing.T) {
	s, h := teamTestServer(t)
	code, out := call(t, h, "POST", "/api/config", `{"severity_policy":{"p1_customers":2,"p2_customers":9,"floor":"P4"}}`)
	if code != 400 || !strings.Contains(out["error"].(string), "severity_policy") {
		t.Fatalf("harus 400: %d %v", code, out)
	}
	if s.cfg.SeverityPolicy.Configured() {
		t.Fatal("kebijakan salah tidak boleh tersimpan")
	}
	code, out = call(t, h, "POST", "/api/config", `{"severity_policy":{"p1_customers":9,"p2_customers":3,"floor":"P4"}}`)
	if code != 200 || !s.cfg.SeverityPolicy.Configured() || s.cfg.SeverityPolicy.P1Customers != 9 {
		t.Fatalf("kebijakan sah harus tersimpan: %d %+v", code, s.cfg.SeverityPolicy)
	}
	if cfg, _ := out["config"].(map[string]any); cfg == nil || cfg["severity_policy"] == nil {
		t.Fatal("kebijakan harus terlihat di config tersunting")
	}
}

// Kasus nyata: Super Admin menulis "cek internet sekarang" tanpa menyebut pelanggan.
func TestRoutingOnStafKeluhanDitanyaTarget(t *testing.T) {
	s, _ := handoffServer(t, "off")
	s.cfg.TeamRouting = "on"
	for _, nomor := range []string{nomorNOC, nomorAdmin} {
		got := balas(s, nomor, "cek internet sekarang")
		if !strings.Contains(got, "Pelanggan mana") {
			t.Errorf("%s: %q", nomor, got)
		}
	}
	// shadow/off: perilaku lama (diteruskan ke LLM -> tidak ditangani handler staf)
	for _, mode := range []string{"off", "shadow"} {
		s.cfg.TeamRouting = mode
		if got := balas(s, nomorNOC, "cek internet sekarang"); got != "" {
			t.Errorf("[%s] harus tetap jalur lama, dapat %q", mode, got)
		}
	}
}
