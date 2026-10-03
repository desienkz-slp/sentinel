package router

import "ainoc/internal/directory"

// Rule = aturan penanganan pesan untuk SATU jabatan pengirim. Ini sumber
// tunggal; router, uji, dan dokumen (docs/ATURAN_AKSES_JABATAN.md) mengacu ke sini.
//
// Prinsip yang berlaku untuk SEMUA jabatan:
//  1. Jabatan ditentukan KODE dari nomor pengirim (direktori staf / billing),
//     bukan dari isi pesan dan bukan dari klaim model.
//  2. Hak akses data & tool tidak ditetapkan di sini, melainkan di
//     directory.Authorize (izin per jabatan) + teamscope (batas tim CS).
//     Aturan di sini hanya memutuskan SIAPA yang dianggap "pelanggan" pada
//     sebuah pesan.
type Rule struct {
	Role directory.Role
	Team Team
	// SubjectIsSender: true  = yang dicek/didiagnosis adalah pengirim itu sendiri
	//                          (hanya pelanggan; identitas = nomornya, dipaksa kode).
	//                  false = pengirim adalah staf; subjek HARUS disebut eksplisit.
	SubjectIsSender bool
	// ComplaintNeedsTarget: keluhan tanpa subjek ditolak dengan pertanyaan
	// "pelanggan mana?" (tidak pernah memakai nomor staf sebagai pelanggan).
	ComplaintNeedsTarget bool
}

// Rules: urut dari akses terendah ke tertinggi.
var Rules = []Rule{
	{Role: directory.RoleUnknown, Team: TeamCS, SubjectIsSender: true},
	{Role: directory.RoleCustomer, Team: TeamCS, SubjectIsSender: true},
	{Role: directory.RoleAdmin, Team: TeamCSLead, ComplaintNeedsTarget: true},
	{Role: directory.RoleNOCSenior, Team: TeamNOC, ComplaintNeedsTarget: true},
	{Role: directory.RoleSuperAdmin, Team: TeamNOC, ComplaintNeedsTarget: true},
}

// RuleFor mengembalikan aturan jabatan. Jabatan tak dikenal -> aturan paling
// ketat (diperlakukan sebagai pengirim biasa, bukan staf).
func RuleFor(r directory.Role) Rule {
	for _, x := range Rules {
		if x.Role == r {
			return x
		}
	}
	return Rules[0]
}

// TargetPrompt: balasan saat staf mengirim keluhan tanpa menyebut pelanggan.
// Tetap, tidak memuat data apa pun.
const TargetPrompt = "Pelanggan mana yang dicek? Kirim username PPPoE, nomor, atau nama pelanggan.\n" +
	"Contoh: cek user namauser"
