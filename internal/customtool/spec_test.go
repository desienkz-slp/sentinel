package customtool

import (
	"strings"
	"testing"
)

func TestValidateOK(t *testing.T) {
	s := Spec{Name: "billing.get_invoices", Domain: "billing", Method: "GET", Path: "/customers/{identity}/invoices", Extract: "data.0.total"}
	if err := Validate(s); err != nil {
		t.Fatalf("spec valid ditolak: %v", err)
	}
}

func TestValidateRejects(t *testing.T) {
	cases := []struct {
		name string
		spec Spec
		want string
	}{
		{"domain arbitrer", Spec{Name: "evil.ping", Domain: "evil.com", Method: "GET", Path: "/x"}, "domain"},
		{"method POST", Spec{Name: "billing.x", Domain: "billing", Method: "POST", Path: "/x"}, "get"},
		{"URL absolut", Spec{Name: "billing.x", Domain: "billing", Method: "GET", Path: "http://evil/x"}, "relatif"},
		{"traversal", Spec{Name: "billing.x", Domain: "billing", Method: "GET", Path: "/../etc"}, "berbahaya"},
		{"extract kode", Spec{Name: "billing.x", Domain: "billing", Method: "GET", Path: "/x", Extract: "a;rm -rf"}, "extract"},
		{"nama tanpa titik", Spec{Name: "billingx", Domain: "billing", Method: "GET", Path: "/x"}, "domain.aksi"},
	}
	for _, c := range cases {
		if err := Validate(c.spec); err == nil || !strings.Contains(strings.ToLower(err.Error()), c.want) {
			t.Errorf("%s: Validate = %v, mau error memuat %q", c.name, err, c.want)
		}
	}
}

func TestProposeAndApprove(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir + "/ct.json")
	spec := Spec{Name: "radius.get_sessions_detail", Domain: "radius", Method: "GET", Path: "/api/sessions", Extract: "0.username"}

	if err := s.Propose(spec, "endpoint-b"); err != nil {
		t.Fatalf("Propose: %v", err)
	}
	// Nama bentrok -> tolak.
	if err := s.Propose(spec, "endpoint-b"); err == nil {
		t.Fatal("propose duplikat seharusnya ditolak")
	}
	// Masih draft, belum active.
	if len(s.Active()) != 0 {
		t.Fatalf("sebelum approve, active = %d", len(s.Active()))
	}
	if len(s.Drafts()) != 1 {
		t.Fatalf("drafts = %d, mau 1", len(s.Drafts()))
	}

	if err := s.Approve("radius.get_sessions_detail"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if len(s.Active()) != 1 {
		t.Fatalf("setelah approve, active = %d", len(s.Active()))
	}
}

func TestReject(t *testing.T) {
	s := NewStore("")
	spec := Spec{Name: "mikrotik.get_arp", Domain: "mikrotik", Method: "GET", Path: "/ip/arp/print"}
	_ = s.Propose(spec, "endpoint-b")
	if err := s.Reject("mikrotik.get_arp", "berisiko"); err != nil {
		t.Fatalf("Reject: %v", err)
	}
	sp, _ := s.Get("mikrotik.get_arp")
	if sp.Status != StatusRejected {
		t.Fatalf("status = %s, mau rejected", sp.Status)
	}
}

func TestPersistReload(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/ct.json"
	s := NewStore(p)
	_ = s.Propose(Spec{Name: "genieacs.get_faults", Domain: "genieacs", Method: "GET", Path: "/faults"}, "endpoint-b")
	_ = s.Approve("genieacs.get_faults")
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	s2 := NewStore(p)
	if len(s2.Active()) != 1 {
		t.Fatalf("reload active = %d, mau 1", len(s2.Active()))
	}
}
