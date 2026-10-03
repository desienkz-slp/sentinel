// Package router memilih TIM (CS / NOC) dan JALUR penanganan sebuah pesan
// WhatsApp secara deterministik, tanpa LLM dan tanpa I/O.
//
// Prinsip (docs/PLAN_DUA_TIM_CS_NOC.md, Fase 1):
//   - Tim ditentukan IDENTITAS pengirim, bukan isi pesan. Pelanggan atau yang
//     tak dikenal SELALU tim CS dan tidak pernah masuk jalur kode milik staf.
//   - Untuk staf, urutan pemilahan tetap dan tertulis di satu tempat:
//     status integrasi -> daftar pelanggan -> perintah cek -> LLM.
//   - Router memakai parser yang sudah ada di paket directory; perilakunya
//     tidak berubah. Yang baru hanya satu pintu keputusan yang bisa diuji,
//     dicatat, dan dibandingkan (mode shadow).
package router

import (
	"strings"
	"unicode"

	"ainoc/internal/directory"
	"ainoc/internal/standard"
)

// Team adalah tim yang menangani pesan.
type Team string

const (
	// TeamCS melayani pelanggan dan pengirim tak dikenal.
	TeamCS Team = "cs"
	// TeamCSLead melayani staf berjabatan Admin (urusan data/billing).
	TeamCSLead Team = "cs_lead"
	// TeamNOC melayani staf NOC Senior dan Super Admin.
	TeamNOC Team = "noc"
)

// Handler adalah jalur penanganan.
type Handler string

const (
	HStatusIntegrasi Handler = "status_integrasi"
	HDaftarPelanggan Handler = "daftar_pelanggan"
	HCekBilling      Handler = "cek_billing"
	HCekPPPoE        Handler = "cek_pppoe"
	HCekTraffic      Handler = "cek_traffic"
	HHandoff         Handler = "handoff" // tutup/update kasus serah-terima
	// HMintaTarget: staf menulis keluhan tanpa menyebut pelanggan -> tanya "pelanggan mana?".
	HMintaTarget Handler = "minta_target"
	// HLLM = tidak cocok pola deterministik; diteruskan ke agen LLM tim terkait.
	HLLM Handler = "llm"
)

// Decision adalah keputusan satu pesan. Nilai nol tidak dipakai: Route selalu
// mengisi Team dan Handler.
type Decision struct {
	Team    Team
	Handler Handler
	// Argumen hasil parsing, hanya terisi sesuai Handler.
	Domains    []string // HStatusIntegrasi
	AllDomains bool     // HStatusIntegrasi
	List       directory.ListQuery
	Cmd        directory.Command
	Handoff    directory.HandoffCmd
	// Reason = alasan singkat untuk log/audit (tanpa isi pesan).
	Reason string
}

// HandledByCode melaporkan apakah pesan ditangani kode (bukan LLM).
func (d Decision) HandledByCode() bool { return d.Handler != HLLM }

// TeamFor menentukan tim dari identitas pengirim. Hanya identitas yang dipakai.
func TeamFor(c directory.Caller) Team {
	if !c.IsStaff {
		return TeamCS
	}
	if c.Role == directory.RoleAdmin {
		return TeamCSLead
	}
	return TeamNOC
}

// Route memilih tim dan jalur. Fungsi murni: hasil hanya bergantung pada
// argumen, sehingga aman dipanggil di mode shadow tanpa efek samping.
func Route(c directory.Caller, msg string) Decision {
	team := TeamFor(c)

	// Pelanggan / tak dikenal: tidak pernah masuk jalur kode staf.
	if !c.IsStaff {
		return Decision{Team: team, Handler: HLLM, Reason: "pengirim bukan staf"}
	}

	// Staf. Urutan di bawah ini SENGAJA tetap dan identik dengan perilaku
	// sebelum router (dikunci oleh golden test). Perintah serah-terima paling
	// depan: formatnya (kata kerja + CASE-ID) tidak bentrok dengan pola lain.
	if hc, ok := directory.ParseHandoffCommand(msg); ok {
		return Decision{Team: team, Handler: HHandoff, Handoff: hc, Reason: "perintah serah-terima kasus"}
	}
	if doms, all := directory.ParseStatusQuestion(msg); len(doms) > 0 || all {
		return Decision{Team: team, Handler: HStatusIntegrasi, Domains: doms, AllDomains: all,
			Reason: "pertanyaan status koneksi integrasi"}
	}
	if lq, ok := directory.ParseListQuery(msg); ok {
		return Decision{Team: team, Handler: HDaftarPelanggan, List: lq,
			Reason: "daftar/ringkasan pelanggan"}
	}
	cmd := directory.ParseCommand(msg)
	switch cmd.Kind {
	case directory.CmdBilling:
		return Decision{Team: team, Handler: HCekBilling, Cmd: cmd, Reason: "perintah cek billing/riwayat"}
	case directory.CmdTraffic:
		return Decision{Team: team, Handler: HCekTraffic, Cmd: cmd, Reason: "perintah cek traffic"}
	case directory.CmdPPPoE:
		return Decision{Team: team, Handler: HCekPPPoE, Cmd: cmd, Reason: "perintah cek PPPoE"}
	}
	return Decision{Team: team, Handler: HLLM, Reason: "tak cocok pola; diteruskan ke LLM"}
}

// RouteByRole = Route + aturan jabatan (rules.go). Hanya dipakai saat routing=on.
// Beda dari Route: staf yang menulis keluhan tanpa menyebut pelanggan TIDAK
// diperlakukan sebagai pelanggan; ditanya "pelanggan mana?".
func RouteByRole(c directory.Caller, msg string) Decision {
	d := Route(c, msg)
	if d.Handler == HLLM && c.IsStaff && RuleFor(c.Role).ComplaintNeedsTarget &&
		standard.Classify(msg) == standard.IntentComplaint && isCustomerSymptom(msg) && !hasExplicitTarget(msg) {
		return Decision{Team: d.Team, Handler: HMintaTarget, Reason: "staf: keluhan tanpa pelanggan yang disebut"}
	}
	return d
}

// hasExplicitTarget: pesan menyebut subjek konkret (angka/IP/ID/@). Sengaja
// longgar ke arah "ada target" agar pesan ambigu tetap ke LLM seperti semula.
func hasExplicitTarget(msg string) bool {
	for _, r := range msg {
		if unicode.IsDigit(r) || r == '@' {
			return true
		}
	}
	return strings.Contains(strings.ToLower(msg), "pelanggan ")
}

var (
	symptomWords = []string{"internet", "wifi", "koneksi", "lemot", "lambat", "mati", "putus", "down", "gangguan", "los", "nyala"}
	// Menyebut komponen sistem = pertanyaan tentang sistem, bukan keluhan pelanggan.
	systemWords = []string{"radius", "mikrotik", "genieacs", "billing", "server", "olt", "router", "gateway", "api", "database", "sentinel"}
)

// isCustomerSymptom: pesan terdengar seperti keluhan layanan pelanggan dan tidak
// sedang menanyakan komponen sistem.
func isCustomerSymptom(msg string) bool {
	low := " " + strings.ToLower(msg) + " "
	for _, w := range systemWords {
		if strings.Contains(low, " "+w+" ") || strings.Contains(low, " "+w+"?") {
			return false
		}
	}
	for _, w := range symptomWords {
		if strings.Contains(low, w) {
			return true
		}
	}
	return false
}
