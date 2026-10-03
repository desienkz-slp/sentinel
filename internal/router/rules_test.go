package router

import (
	"testing"

	"ainoc/internal/directory"
)

func stafJabatan(r directory.Role) directory.Caller {
	return directory.Caller{Number: "628111222333", Role: r, IsStaff: true, Name: "Staf Uji"}
}

var pelangganUji = directory.Caller{Number: "628111000111", Role: directory.RoleCustomer, IsCustomer: true}

func TestAturanSetiapJabatanAda(t *testing.T) {
	for _, r := range []directory.Role{directory.RoleUnknown, directory.RoleCustomer, directory.RoleAdmin,
		directory.RoleNOCSenior, directory.RoleSuperAdmin} {
		found := false
		for _, x := range Rules {
			if x.Role == r {
				found = true
			}
		}
		if !found {
			t.Errorf("jabatan %q tidak punya aturan", r)
		}
	}
	// Jabatan asing -> paling ketat (diperlakukan sebagai pengirim biasa)
	if got := RuleFor("hacker"); !got.SubjectIsSender || got.ComplaintNeedsTarget {
		t.Errorf("jabatan asing harus aturan paling ketat: %+v", got)
	}
}

// Staf TIDAK PERNAH dianggap pelanggan.
func TestStafKeluhanTanpaTargetDitanya(t *testing.T) {
	for _, r := range []directory.Role{directory.RoleAdmin, directory.RoleNOCSenior, directory.RoleSuperAdmin} {
		for _, msg := range []string{"cek internet sekarang", "internet lemot", "internet mati"} {
			if d := RouteByRole(stafJabatan(r), msg); d.Handler != HMintaTarget {
				t.Errorf("%s %q -> %s, mau minta_target", r, msg, d.Handler)
			}
		}
	}
}

// Pelanggan tetap dilayani untuk dirinya sendiri; tak pernah ditanya target.
func TestPelangganTetapLLMUntukDirinya(t *testing.T) {
	for _, msg := range []string{"cek internet sekarang", "internet lemot", "internet mati"} {
		d := RouteByRole(pelangganUji, msg)
		if d.Handler != HLLM || d.Team != TeamCS {
			t.Errorf("%q -> %s/%s, mau cs/llm", msg, d.Team, d.Handler)
		}
	}
	d := RouteByRole(directory.Caller{Number: "628999000333"}, "internet mati")
	if d.Handler != HLLM || d.Team != TeamCS {
		t.Errorf("tak dikenal: %s/%s", d.Team, d.Handler)
	}
}

// Staf yang menyebut subjek konkret tidak ditanya lagi.
func TestStafDenganTargetTidakDitanya(t *testing.T) {
	for _, msg := range []string{"cek internet 628111000111", "internet pelanggan budi mati", "internet user@isp mati"} {
		if d := RouteByRole(stafJabatan(directory.RoleNOCSenior), msg); d.Handler == HMintaTarget {
			t.Errorf("%q tidak boleh ditanya target", msg)
		}
	}
}

// Perintah yang sudah dikenali tidak berubah oleh aturan jabatan.
func TestAturanJabatanTidakMengubahPerintahKode(t *testing.T) {
	for _, msg := range []string{"cek user budi123", "cek billing budi123", "daftar pelanggan", "status radius"} {
		for _, r := range []directory.Role{directory.RoleAdmin, directory.RoleNOCSenior, directory.RoleSuperAdmin} {
			if a, b := Route(stafJabatan(r), msg), RouteByRole(stafJabatan(r), msg); a.Handler != b.Handler {
				t.Errorf("%s %q: %s != %s", r, msg, a.Handler, b.Handler)
			}
		}
	}
}

// Route lama (off/shadow) tidak berubah sama sekali.
func TestRouteLamaTidakBerubah(t *testing.T) {
	if d := Route(stafJabatan(directory.RoleSuperAdmin), "cek internet sekarang"); d.Handler != HLLM {
		t.Fatalf("Route lama harus tetap llm: %s", d.Handler)
	}
}
