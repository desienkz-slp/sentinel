package router

import (
	"testing"

	"ainoc/internal/directory"
	"ainoc/internal/teamcorpus"
)

func staf(role directory.Role) directory.Caller {
	return directory.Caller{Number: "628111222333", Role: role, IsStaff: true, Name: "Staf Uji"}
}

var (
	pelanggan  = directory.Caller{Number: "628999000111", Role: directory.RoleCustomer, IsCustomer: true, Name: "Pelanggan Uji"}
	takDikenal = directory.Caller{Number: "628999000222", Role: directory.RoleCustomer}
)

func TestTeamForHanyaDariIdentitas(t *testing.T) {
	cases := []struct {
		name string
		c    directory.Caller
		want Team
	}{
		{"pelanggan", pelanggan, TeamCS},
		{"tak dikenal", takDikenal, TeamCS},
		{"admin", staf(directory.RoleAdmin), TeamCSLead},
		{"noc senior", staf(directory.RoleNOCSenior), TeamNOC},
		{"super admin", staf(directory.RoleSuperAdmin), TeamNOC},
	}
	for _, c := range cases {
		if got := TeamFor(c.c); got != c.want {
			t.Errorf("%s: TeamFor = %s, mau %s", c.name, got, c.want)
		}
	}
}

func TestRouteTabel(t *testing.T) {
	noc := staf(directory.RoleNOCSenior)
	cases := []struct {
		pesan string
		want  Handler
	}{
		{"sudah bisa terhubung ke billing?", HStatusIntegrasi},
		{"radius aktif gak", HStatusIntegrasi},
		{"semua integrasi normal?", HStatusIntegrasi},
		{"daftar pelanggan", HDaftarPelanggan},
		{"terkait dafta pelanggan dan history bayar?", HDaftarPelanggan},
		{"pelanggan isolir", HDaftarPelanggan},
		{"cari uji-01", HDaftarPelanggan},
		{"cek billing pelanggan-uji", HCekBilling},
		{"riwayat pelanggan-uji", HCekBilling},
		{"cek user pppor pelanggan-uji", HCekPPPoE},
		{"cek traffict", HCekTraffic},
		{"cek traffic pelanggan-uji", HCekTraffic},
		{"halo", HLLM},
		{"cek koneksi 8.8.8.8", HLLM},
		{"", HLLM},
	}
	for _, c := range cases {
		d := Route(noc, c.pesan)
		if d.Handler != c.want {
			t.Errorf("Route(%q) = %s, mau %s", c.pesan, d.Handler, c.want)
		}
		if d.Team != TeamNOC || d.Reason == "" {
			t.Errorf("Route(%q): team=%s reason=%q", c.pesan, d.Team, d.Reason)
		}
	}
}

// Urutan deterministik: pesan yang cocok >1 aturan selalu jatuh ke yang
// pertama. "cek koneksi billing" cocok status DAN billing -> status menang.
func TestRouteUrutanTetap(t *testing.T) {
	noc := staf(directory.RoleNOCSenior)
	if d := Route(noc, "cek koneksi billing"); d.Handler != HStatusIntegrasi {
		t.Errorf("status harus menang atas cek billing, dapat %s", d.Handler)
	}
	if d := Route(noc, "cek billing pelanggan-uji"); d.Handler != HCekBilling {
		t.Errorf("cek billing dengan target harus HCekBilling, dapat %s", d.Handler)
	}
}

// PROPERTI 1: pelanggan / tak dikenal TIDAK PERNAH mendapat jalur kode staf
// dan tidak pernah tim NOC — untuk SEMUA pesan korpus, termasuk yang meniru
// perintah staf.
func TestPropertiPelangganTakPernahJalurStafAtauNOC(t *testing.T) {
	var pesan []string
	for _, k := range teamcorpus.Staf {
		pesan = append(pesan, k.Pesan)
	}
	for _, k := range teamcorpus.PelangganKasus {
		pesan = append(pesan, k.Pesan)
	}
	pesan = append(pesan, teamcorpus.KeluhanPanjang...)
	for _, c := range []directory.Caller{pelanggan, takDikenal} {
		for _, p := range pesan {
			d := Route(c, p)
			if d.Team != TeamCS {
				t.Errorf("pelanggan %q -> tim %s, harus cs", p, d.Team)
			}
			if d.HandledByCode() {
				t.Errorf("pelanggan %q -> jalur kode %s, harus llm", p, d.Handler)
			}
		}
	}
}

// PROPERTI 2: staf TIDAK PERNAH masuk tim CS (nada pelanggan).
func TestPropertiStafTakPernahTimCS(t *testing.T) {
	for _, role := range []directory.Role{directory.RoleAdmin, directory.RoleNOCSenior, directory.RoleSuperAdmin} {
		for _, k := range teamcorpus.Staf {
			if d := Route(staf(role), k.Pesan); d.Team == TeamCS {
				t.Errorf("staf %s pesan %q -> tim cs", role, k.Pesan)
			}
		}
	}
}

// PROPERTI 3: keluhan panjang tidak pernah dibaca sebagai perintah, siapa pun
// pengirimnya.
func TestPropertiKeluhanPanjangBukanPerintah(t *testing.T) {
	for _, role := range []directory.Role{directory.RoleAdmin, directory.RoleNOCSenior, directory.RoleSuperAdmin} {
		for _, p := range teamcorpus.KeluhanPanjang {
			if d := Route(staf(role), p); d.HandledByCode() {
				t.Errorf("keluhan %q -> jalur kode %s", p, d.Handler)
			}
		}
	}
}

// Korpus staf: keputusan router == jalur yang diharapkan korpus (perilaku
// v0.2.21), untuk semua jabatan staf.
func TestRouteSesuaiKorpus(t *testing.T) {
	peta := map[teamcorpus.Handler]func(Handler) bool{
		teamcorpus.HStatusIntegrasi: func(h Handler) bool { return h == HStatusIntegrasi },
		teamcorpus.HDaftarPelanggan: func(h Handler) bool { return h == HDaftarPelanggan },
		teamcorpus.HPerintahCek:     func(h Handler) bool { return h == HCekBilling || h == HCekPPPoE || h == HCekTraffic },
		teamcorpus.HLLM:             func(h Handler) bool { return h == HLLM },
	}
	for _, role := range []directory.Role{directory.RoleAdmin, directory.RoleNOCSenior, directory.RoleSuperAdmin} {
		for _, k := range teamcorpus.Staf {
			d := Route(staf(role), k.Pesan)
			if !peta[k.Harapan](d.Handler) {
				t.Errorf("[%s/%s] %q -> %s, korpus mengharapkan %s", role, k.Nama, k.Pesan, d.Handler, k.Harapan)
			}
		}
	}
}

// Route harus murni: dua panggilan sama menghasilkan keputusan sama.
func TestRouteMurni(t *testing.T) {
	noc := staf(directory.RoleNOCSenior)
	for _, k := range teamcorpus.Staf {
		a, b := Route(noc, k.Pesan), Route(noc, k.Pesan)
		if a.Team != b.Team || a.Handler != b.Handler || a.Reason != b.Reason {
			t.Errorf("Route tidak murni untuk %q", k.Pesan)
		}
	}
}

// Fuzz sederhana: tidak boleh panik untuk masukan aneh.
func TestRouteTidakPanik(t *testing.T) {
	aneh := []string{"\x00", "   ", "????", "a", "🙂🙂🙂", string(make([]byte, 5000)), "cek", "cari", "daftar", "billing"}
	for _, p := range aneh {
		for _, c := range []directory.Caller{pelanggan, takDikenal, staf(directory.RoleNOCSenior), {}} {
			_ = Route(c, p)
		}
	}
}
