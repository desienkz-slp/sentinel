package teamscope

import (
	"reflect"
	"testing"

	"ainoc/internal/router"
)

var uji = Customer{Username: "pelanggan-uji", Phone: "628111000111"}

func TestCSTidakBolehToolCakupanLuas(t *testing.T) {
	for tool := range scopeless {
		v := Check(router.TeamCS, tool, map[string]any{"filter": "isolir"}, uji)
		if v.Allowed {
			t.Errorf("CS tidak boleh memakai %s", tool)
		}
		if v.Reason == "" {
			t.Errorf("%s: penolakan harus punya alasan", tool)
		}
	}
}

func TestCSToolTakDikenalDitolak(t *testing.T) {
	for _, tool := range []string{"mikrotik.disconnect_pppoe", "billing.delete", "sembarang", "", "radius.reset"} {
		if v := Check(router.TeamCS, tool, nil, uji); v.Allowed {
			t.Errorf("tool %q harus ditolak untuk CS", tool)
		}
	}
}

// Model jahat: meminta identitas pelanggan LAIN lewat berbagai nama argumen.
// Hasilnya harus selalu identitas pelanggan terverifikasi.
func TestCSIdentitasLainDipaksaKePelangganSendiri(t *testing.T) {
	for _, tool := range []string{"billing.get_customer", "billing.get_history", "radius.get_session", "mikrotik.get_pppoe_status", "genieacs.get_device_state"} {
		for _, key := range identityKeys {
			v := Check(router.TeamCS, tool, map[string]any{key: "pelanggan-orang-lain"}, uji)
			if !v.Allowed {
				t.Errorf("%s/%s: seharusnya diizinkan dengan identitas dipaksa", tool, key)
				continue
			}
			if got := v.Args["identity"]; got != "pelanggan-uji" {
				t.Errorf("%s/%s: identity = %v, harus dipaksa ke pelanggan-uji", tool, key, got)
			}
			if !v.Rewritten {
				t.Errorf("%s/%s: harus ditandai Rewritten (percobaan lintas pelanggan)", tool, key)
			}
			// kunci lain yang berpotensi membawa identitas harus dibuang
			for _, k2 := range identityKeys {
				if k2 != "identity" {
					if _, ada := v.Args[k2]; ada {
						t.Errorf("%s/%s: kunci %q masih ada di argumen", tool, key, k2)
					}
				}
			}
		}
	}
}

// Banyak kunci sekaligus + nilai non-string: tidak boleh ada yang lolos.
func TestCSBanyakKunciDanTipeAneh(t *testing.T) {
	args := map[string]any{
		"identity": "orang-lain", "username": "orang-lain", "phone": "628999000333",
		"device_id": "ABC-123", "search": 12345, "name": []string{"x"}, "customer": map[string]any{"id": 9},
		"interface": "ether1", // bukan identitas; tidak disentuh
	}
	v := Check(router.TeamCS, "radius.get_session", args, uji)
	if !v.Allowed || v.Args["identity"] != "pelanggan-uji" || !v.Rewritten {
		t.Fatalf("hasil: %+v", v)
	}
	for _, k := range []string{"username", "phone", "device_id", "search", "name", "customer"} {
		if _, ada := v.Args[k]; ada {
			t.Errorf("kunci %q harus dibuang", k)
		}
	}
	if v.Args["interface"] != "ether1" {
		t.Error("argumen non-identitas tidak boleh diubah")
	}
}

// Model BENAR: memakai identitas pelanggan itu sendiri -> diizinkan, TIDAK
// ditandai sebagai percobaan lintas pelanggan.
func TestCSIdentitasBenarTidakDitandaiRewritten(t *testing.T) {
	for _, val := range []string{"pelanggan-uji", "Pelanggan-Uji", "  pelanggan-uji ", "628111000111", "+62 811-1000-111"} {
		v := Check(router.TeamCS, "billing.get_customer", map[string]any{"identity": val}, uji)
		if !v.Allowed || v.Args["identity"] != "pelanggan-uji" {
			t.Errorf("%q: %+v", val, v)
		}
		if v.Rewritten {
			t.Errorf("%q: identitas sah tidak boleh dianggap percobaan lintas pelanggan", val)
		}
	}
}

// Tanpa identitas terverifikasi -> gagal tertutup untuk tool registry.
func TestCSTanpaIdentitasTerverifikasiDitolak(t *testing.T) {
	for _, c := range []Customer{{}, {Username: "  "}, {Phone: "628111000111"}} {
		if v := Check(router.TeamCS, "billing.get_customer", map[string]any{"identity": "siapa-saja"}, c); v.Allowed {
			t.Errorf("tanpa username terverifikasi (%+v) harus ditolak", c)
		}
	}
}

// Probe jaringan bukan data pelanggan: diteruskan apa adanya, target dari
// pelanggan tidak ditimpa dengan username.
func TestCSProbeDiteruskan(t *testing.T) {
	for _, p := range []string{"ping", "dns", "tcp", "http"} {
		v := Check(router.TeamCS, p, map[string]any{"target": "8.8.8.8"}, uji)
		if !v.Allowed || v.Args["target"] != "8.8.8.8" || v.Rewritten {
			t.Errorf("%s: %+v", p, v)
		}
		if _, ada := v.Args["identity"]; ada {
			t.Errorf("%s: probe tidak boleh diberi identity", p)
		}
	}
	// Probe yang membocorkan info host/infrastruktur tidak boleh untuk CS.
	for _, p := range []string{"traceroute", "interface", "service", "system", "radius"} {
		if v := Check(router.TeamCS, p, map[string]any{"target": "127.0.0.1"}, uji); v.Allowed {
			t.Errorf("probe %q tidak boleh untuk CS", p)
		}
	}
}

// NOC dan CS-Lead: tool & argumen tidak diubah (otorisasi peran tetap di
// directory.Authorize).
func TestNOCDanCSLeadTidakDibatasi(t *testing.T) {
	for _, team := range []router.Team{router.TeamNOC, router.TeamCSLead} {
		args := map[string]any{"identity": "siapa-saja", "filter": "isolir"}
		for _, tool := range []string{"billing.list_customers", "radius.get_system_stats", "billing.get_customer"} {
			v := Check(team, tool, args, Customer{})
			if !v.Allowed || v.Rewritten || !reflect.DeepEqual(v.Args, args) {
				t.Errorf("%s/%s: %+v", team, tool, v)
			}
		}
	}
}

// Check tidak boleh mengubah map masukan (aman dipanggil berulang).
func TestCheckTidakMengubahMasukan(t *testing.T) {
	in := map[string]any{"identity": "orang-lain", "username": "x"}
	_ = Check(router.TeamCS, "billing.get_customer", in, uji)
	if in["identity"] != "orang-lain" || in["username"] != "x" || len(in) != 2 {
		t.Fatalf("map masukan berubah: %v", in)
	}
}

func TestFilterToolsUntukCS(t *testing.T) {
	semua := []string{
		"billing.get_customer", "billing.get_history", "billing.list_customers",
		"radius.get_session", "radius.get_system_stats", "radius.get_user",
		"mikrotik.get_pppoe_status", "mikrotik.get_interface_stats", "mikrotik.get_customer_traffic",
		"genieacs.get_device_state", "genieacs.get_devices", "ping", "dns", "tcp", "traceroute", "interface", "system", "mikrotik.disconnect_pppoe",
	}
	got := FilterTools(router.TeamCS, semua)
	want := []string{"billing.get_customer", "billing.get_history", "radius.get_session", "mikrotik.get_pppoe_status", "genieacs.get_device_state", "ping", "dns", "tcp"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("FilterTools(CS) = %v, mau %v", got, want)
	}
	if got := FilterTools(router.TeamNOC, semua); !reflect.DeepEqual(got, semua) {
		t.Fatalf("NOC tidak boleh disaring: %v", got)
	}
	// tool tulis tak pernah muncul untuk CS
	for _, n := range FilterTools(router.TeamCS, semua) {
		if n == "mikrotik.disconnect_pppoe" {
			t.Fatal("tool tulis bocor ke CS")
		}
	}
}

// Invarian: apa pun yang diminta, tool yang lolos untuk CS tidak pernah memuat
// identitas selain milik pelanggan terverifikasi.
func TestInvariPenuhCSTidakAdaKebocoran(t *testing.T) {
	tools := []string{"billing.get_customer", "billing.get_history", "radius.get_session", "mikrotik.get_pppoe_status", "genieacs.get_device_state"}
	nilai := []any{"orang-lain", "", "*", "%", "' OR 1=1 --", "pelanggan-uji; pelanggan-lain", 0, nil, true, "628999888777"}
	for _, tool := range tools {
		for _, k := range identityKeys {
			for _, n := range nilai {
				v := Check(router.TeamCS, tool, map[string]any{k: n}, uji)
				if !v.Allowed {
					continue
				}
				for key, val := range v.Args {
					if key == "identity" {
						if val != "pelanggan-uji" {
							t.Fatalf("%s %s=%v: identity bocor = %v", tool, k, n, val)
						}
						continue
					}
					for _, ik := range identityKeys {
						if key == ik {
							t.Fatalf("%s %s=%v: kunci identitas %q lolos", tool, k, n, key)
						}
					}
				}
			}
		}
	}
}
