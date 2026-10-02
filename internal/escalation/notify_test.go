package escalation

import (
	"strings"
	"testing"
)

func TestClassifyDomainNetworkSymptoms(t *testing.T) {
	cases := []string{
		"internet saya putus total dari tadi pagi",
		"koneksi wifi lemot dan sering terputus",
		"pppoe tidak bisa login, muncul error",
		"ont merah dan los berkedip",
	}
	for _, q := range cases {
		if got := ClassifyDomain(q); got != "network" {
			t.Errorf("ClassifyDomain(%q) = %q, ingin network", q, got)
		}
	}
}

func TestClassifyDomainBillingSymptoms(t *testing.T) {
	cases := []string{
		"tagihan saya belum terbayar tapi internet mati",
		"saya mau ubah paket dan cek pembayaran",
		"invoice bulan ini kelebihan biaya",
		"minta refund karena jaringan sering putus",
	}
	for _, q := range cases {
		if got := ClassifyDomain(q); got != "billing" {
			t.Errorf("ClassifyDomain(%q) = %q, ingin billing", q, got)
		}
	}
}

func TestClassifyDomainEmptyDefaultsNetwork(t *testing.T) {
	if got := ClassifyDomain(""); got != "network" {
		t.Fatalf("ClassifyDomain(\"\") = %q, ingin network", got)
	}
}

func TestFormatHandoffNetworkContainsAllSections(t *testing.T) {
	c := Context{
		CaseID:              "CASE-20261002-ABCDEF",
		Domain:              "network",
		Customer:            "628111222333",
		Complaint:           "Internet putus total",
		ConversationSummary: "Pelanggan melaporkan internet putus sejak pagi.",
		Timeline:            "pagi ini",
		BillingStatus:       "ACTIVE",
		RadiusStatus:        "ONLINE",
		RouterStatus:        "PPPoE DOWN",
		GenieACSStatus:      "ONLINE",
		Diagnostics:         []string{"cek sesi PPPoE"},
		Evidence:            []string{"PPPoE tidak aktif"},
		Hypothesis:          "sesi PPPoE stale",
		Confidence:          "MEDIUM",
		ActionsPerformed:    []string{"diagnostik read-only"},
		ActionsNotPerformed: []string{"ubah konfigurasi router"},
		Recommendation:      "periksa jalur akses",
		Urgency:             "SEV-2",
		AffectedScope:       "satu pelanggan",
		Reason:              "butuh otoritas NOC Senior",
		HumanNeed:           "validasi dan tindakan jaringan",
	}
	msg := FormatHandoff(c, RoleNOCSenior)

	for _, want := range []string{
		"NOC Senior", "Case ID", "CASE-20261002-ABCDEF", "Pelanggan", "628111222333",
		"Keluhan", "Internet putus total", "Status Billing", "Status RADIUS",
		"Status Router", "Status GenieACS", "Hipotesis", "Keyakinan", "Rekomendasi",
		"Urgensi", "Cakupan terdampak", "Alasan eskalasi", "Yang dibutuhkan dari Anda",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("handoff network tidak memuat %q:\n%s", want, msg)
		}
	}
}

func TestFormatHandoffBillingOmitsNetworkStatus(t *testing.T) {
	c := Context{
		CaseID:     "CASE-20261002-ABCDEF",
		Domain:     "billing",
		Customer:   "628111222333",
		Complaint:  "Tagihan belum terbayar",
		Hypothesis: "akun nonaktif karena tagihan",
	}
	msg := FormatHandoff(c, RoleAdmin)
	if !strings.Contains(msg, "Admin") {
		t.Errorf("handoff billing harus ditujukan ke Admin:\n%s", msg)
	}
	// Status per-sistem hanya untuk authority jaringan — jangan bocor ke Admin.
	for _, forbidden := range []string{"Status Billing", "Status RADIUS", "Status Router", "Status GenieACS"} {
		if strings.Contains(msg, forbidden) {
			t.Errorf("handoff billing tidak boleh memuat %q:\n%s", forbidden, msg)
		}
	}
}

func TestFormatHandoffBlankUsesDash(t *testing.T) {
	msg := FormatHandoff(Context{CaseID: "CASE-X", Domain: "network"}, RoleNOCSenior)
	if !strings.Contains(msg, "—") {
		t.Errorf("field kosong harus ditampilkan '—':\n%s", msg)
	}
}

func TestDedupOnceOnlyFirstCall(t *testing.T) {
	d := NewDedup()
	if !d.Once("CASE-A") {
		t.Fatal("pemanggilan pertama harus true")
	}
	if d.Once("CASE-A") {
		t.Fatal("pemanggilan kedua harus false (anti spam)")
	}
	if !d.Once("CASE-B") {
		t.Fatal("case berbeda harus true")
	}
}

func TestDedupEmptyIDNeverSent(t *testing.T) {
	d := NewDedup()
	if d.Once("") {
		t.Fatal("ID kosong tidak boleh dikirim")
	}
}
