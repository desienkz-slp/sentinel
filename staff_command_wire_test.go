package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"ainoc/internal/audit"
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
	for _, n := range []string{"billing.get_customer", "radius.get_session", "mikrotik.get_pppoe_status", "mikrotik.get_customer_traffic"} {
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
	if !ok || !strings.Contains(text, "pelanggan-uji") || !strings.Contains(text, "status=AKTIF") || !strings.Contains(text, "sesi online") {
		t.Fatalf("balasan perintah PPPoE tidak sesuai: ok=%v %q", ok, text)
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
	if !ok || !strings.Contains(text, "*Billing*") || !strings.Contains(text, "status=AKTIF") {
		t.Fatalf("balasan billing tidak sesuai: ok=%v %q", ok, text)
	}
	if strings.Contains(text, "RADIUS") || strings.Contains(text, "MikroTik") {
		t.Fatalf("perintah billing hanya boleh memanggil billing: %q", text)
	}
}
