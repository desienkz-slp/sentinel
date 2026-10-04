package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ainoc/internal/audit"
	"ainoc/internal/billing"
	"ainoc/internal/config"
	"ainoc/internal/directory"
	"ainoc/internal/policy"
	"ainoc/internal/registry"
	"ainoc/internal/session"
	"ainoc/internal/tool"
)

type fakeAd struct {
	domain string
	tools  []string
	text   string
}

func (f fakeAd) Domain() string                         { return f.domain }
func (f fakeAd) Name() string                           { return f.domain }
func (f fakeAd) Configured() bool                       { return true }
func (f fakeAd) Health(context.Context) (string, error) { return "ok", nil }
func (f fakeAd) ToolNames() []string                    { return f.tools }
func (f fakeAd) Invoke(_ context.Context, n string, a map[string]any) (tool.Output, error) {
	return tool.Output{Text: f.text + " untuk " + a["identity"].(string)}, nil
}

func staffCmdServer(t *testing.T) *Server {
	t.Helper()
	y := "version: 1.0.0\ntools:\n"
	for _, n := range []string{"billing.get_customer", "billing.get_history", "billing.list_customers", "radius.get_session", "mikrotik.get_pppoe_status", "mikrotik.get_customer_traffic"} {
		y += "  - name: " + n + "\n    domain: " + strings.Split(n, ".")[0] + "\n    enabled: true\n    permission: READ\n    risk: LOW\n    scope: single_customer\n"
	}
	p := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(p, []byte(y), 0o644); err != nil {
		t.Fatal(err)
	}
	disp := tool.New(registry.Load(p), policy.Load(""), time.Second)
	disp.Register(fakeAd{"billing", []string{"billing.get_customer"}, "status=AKTIF"})
	disp.Register(fakeAd{"radius", []string{"radius.get_session"}, "sesi online"})
	disp.Register(fakeAd{"mikrotik", []string{"mikrotik.get_pppoe_status", "mikrotik.get_customer_traffic"}, "rx=1Mbps tx=2Mbps"})
	return &Server{
		cfg: &config.Config{StaffMembers: []config.StaffMember{
			{Number: "628111222333", Name: "NOC Uji", Role: "noc_senior", Active: true},
		}},
		disp: disp, aud: audit.New("", 50), pinSesi: newPINStore(time.Minute),
		sesi: session.New(session.DefaultConfig()),
	}
}

func TestStaffCommandRunsForNOCAndSkipsCustomers(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	noc := s.identifyCaller(context.Background(), "628111222333")
	if !noc.IsStaff {
		t.Fatal("NOC harus dikenali sebagai staf")
	}

	text, ok := s.handleStaffCommand(context.Background(), noc, "628111222333", "cek user pppor pelanggan-uji")
	if !ok || !strings.Contains(text, "pelanggan-uji") || !strings.Contains(text, "status=AKTIF") {
		t.Fatalf("balasan perintah PPPoE tidak sesuai: ok=%v %q", ok, text)
	}
	if strings.Contains(text, "*RADIUS:*") {
		t.Fatalf("perintah staf tidak boleh memanggil Radius bulk session: %q", text)
	}
	if strings.Contains(text, "kendala") {
		t.Fatalf("staf tidak boleh ditanya 'kendala': %q", text)
	}

	// "cek traffict" tanpa target -> memakai target terakhir percakapan.
	text, ok = s.handleStaffCommand(context.Background(), noc, "628111222333", "cek traffict")
	if !ok || !strings.Contains(text, "rx=1Mbps") || !strings.Contains(text, "pelanggan-uji") {
		t.Fatalf("balasan traffic tidak sesuai: ok=%v %q", ok, text)
	}
	if s.aud.Len() == 0 {
		t.Fatal("perintah staf harus diaudit")
	}

	// Pelanggan/tak dikenal TIDAK pernah memicu jalur staf.
	cust := directory.Caller{Number: "628777", Role: directory.RoleCustomer, Perms: directory.PermsFor(directory.RoleCustomer)}
	if _, ok := s.handleStaffCommand(context.Background(), cust, "628777", "cek user pppoe pelanggan-uji"); ok {
		t.Fatal("pelanggan tidak boleh menjalankan perintah staf")
	}
	// Keluhan biasa dari staf tetap ke alur normal.
	if _, ok := s.handleStaffCommand(context.Background(), noc, "628111222333", "internet lemot"); ok {
		t.Fatal("pesan non-perintah harus lanjut ke alur biasa")
	}
	// Tanpa target sama sekali -> minta username, bukan menebak.
	text, ok = s.handleStaffCommand(context.Background(), noc, "628000", "cek traffic")
	if !ok || !strings.Contains(text, "Username/nama pelanggan") {
		t.Fatalf("tanpa target harus minta username: %q", text)
	}
}

func TestStaffCommandBilling(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	noc := s.identifyCaller(context.Background(), "628111222333")
	text, ok := s.handleStaffCommand(context.Background(), noc, "628111222333", "cek billing pelanggan-uji")
	if !ok || !strings.Contains(text, "*Billing — pelanggan-uji*") || !strings.Contains(text, "status=AKTIF") {
		t.Fatalf("balasan billing tidak sesuai: ok=%v %q", ok, text)
	}
	if strings.Contains(text, "RADIUS") || strings.Contains(text, "MikroTik") {
		t.Fatalf("perintah billing hanya boleh memanggil billing: %q", text)
	}
}

func TestStaffStatusQuestion(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	noc := s.identifyCaller(context.Background(), "628111222333")

	// Pertanyaan persis dari log produksi: harus dijawab dari health adaptor,
	// bukan dari LLM.
	for _, q := range []string{"sudah bisa terhubung ke billing?", "sudab bisa baca billing?"} {
		text, ok := s.handleStaffCommand(context.Background(), noc, "628111222333", q)
		if !ok {
			t.Fatalf("%q harus ditangani kode, bukan diteruskan ke LLM", q)
		}
		if !strings.Contains(text, "Billing: terhubung") {
			t.Fatalf("%q -> %q, mau memuat 'Billing: terhubung'", q, text)
		}
		if strings.Contains(text, "RADIUS") {
			t.Fatalf("hanya billing yang ditanya: %q", text)
		}
	}

	// Domain yang tak terdaftar dijawab jujur.
	text, ok := s.handleStaffCommand(context.Background(), noc, "628111222333", "genieacs konek?")
	if !ok || !strings.Contains(text, "GenieACS: belum dikonfigurasi") {
		t.Fatalf("genieacs tak terdaftar harus dilaporkan belum dikonfigurasi: ok=%v %q", ok, text)
	}

	// Perintah cek biasa tidak boleh terbajak jalur status.
	text, ok = s.handleStaffCommand(context.Background(), noc, "628111222333", "cek billing pelanggan-uji")
	if !ok || strings.Contains(text, "Status integrasi") {
		t.Fatalf("cek billing <user> harus tetap jalur data pelanggan: %q", text)
	}
}

// billingHTTP memasang adaptor billing sungguhan ke server NOC palsu.
func billingHTTP(t *testing.T, s *Server) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/api/noc/v1/customers/7/history":
			w.Write([]byte(`{"status":"success","data":{"billing_info":{"jenis_bayar":"pascabayar"},"history":{"2020":[{"period":"2020-02","charge_amount":100000,"status":"unpaid"},{"period":"2020-01","charge_amount":100000,"status":"paid"}]}}}`))
		case r.URL.Path == "/api/noc/v1/customers" && q.Get("is_isolated") == "1":
			w.Write([]byte(`{"status":"success","data":[{"id":7,"name":"Uji Isolir","username":"uji-isolir","status":"active","is_isolated":true}],"meta":{"total":2}}`))
		case r.URL.Path == "/api/noc/v1/customers" && q.Get("search") == "uji-isolir":
			w.Write([]byte(`{"status":"success","data":[{"id":7,"name":"Uji Isolir","username":"uji-isolir","status":"active"}],"meta":{"total":1}}`))
		case r.URL.Path == "/api/noc/v1/customers":
			tot := "908"
			if q.Get("status") == "active" {
				tot = "861"
			} else if q.Get("status") == "inactive" {
				tot = "47"
			}
			w.Write([]byte(`{"status":"success","data":[],"meta":{"total":` + tot + `}}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	ad := billing.New(srv.URL, "k")
	s.disp.Register(ad)
}

func TestStaffCustomerList(t *testing.T) {
	s := staffCmdServer(t)
	s.syncDirectory()
	billingHTTP(t, s)
	noc := s.identifyCaller(context.Background(), "628111222333")
	ask := func(q string) string {
		text, ok := s.handleStaffCommand(context.Background(), noc, "628111222333", q)
		if !ok {
			t.Fatalf("%q harus ditangani kode, bukan diteruskan ke LLM", q)
		}
		return text
	}

	// Pesan produksi persis (dengan typo "dafta").
	got := ask("terkait dafta pelanggan dan history bayar?")
	for _, want := range []string{"Total pelanggan: 908", "Aktif: 861", "Nonaktif: 47", "riwayat <username>"} {
		if !strings.Contains(got, want) {
			t.Errorf("ringkasan tak memuat %q: %s", want, got)
		}
	}
	if iso := ask("daftar pelanggan isolir"); !strings.Contains(iso, "2 pelanggan") || strings.Contains(iso, "uji-isolir") {
		t.Errorf("ringkasan isolir harus aggregate tanpa PII: %s", iso)
	}
	if cari := ask("cari uji-isolir"); !strings.Contains(cari, "1 pelanggan") || strings.Contains(cari, "uji-isolir") {
		t.Errorf("hasil cari harus aggregate tanpa PII: %s", cari)
	}
	his := ask("riwayat uji-isolir")
	if !strings.Contains(his, "tidak tersedia") || !strings.Contains(his, "diblokir") {
		t.Errorf("riwayat harus dilaporkan blocked: %s", his)
	}
}
