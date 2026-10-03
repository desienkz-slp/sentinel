package severity

import (
	"math/rand"
	"testing"
)

// Angka di bawah hanya fixture uji, bukan rekomendasi operasional.
var pol = Policy{P1Customers: 50, P2Customers: 10, P3Customers: 3, Floor: "P4",
	ScopeFloor: map[string]string{"upstream": "P1", "olt": "P2"}, CriticalFloor: "P2"}

func TestBatasAmbang(t *testing.T) {
	cases := []struct {
		n    int
		want Level
	}{{0, P4}, {2, P4}, {3, P3}, {9, P3}, {10, P2}, {49, P2}, {50, P1}, {500, P1}}
	for _, c := range cases {
		if got := Rate(pol, Input{Affected: c.n}).Level; got != c.want {
			t.Errorf("affected=%d: %s, mau %s", c.n, got, c.want)
		}
	}
}

func TestTanpaKebijakanUnrated(t *testing.T) {
	for _, p := range []Policy{{}, {P1Customers: 5}, {Floor: "P4"}, {P1Customers: 5, Floor: "PX"}} {
		if got := Rate(p, Input{Affected: 1000}).Level; got != Unrated {
			t.Errorf("%+v -> %s, mau UNRATED", p, got)
		}
	}
}

func TestCakupanDanKritisMenaikkan(t *testing.T) {
	if got := Rate(pol, Input{Affected: 1, Scopes: []string{"UPSTREAM"}}).Level; got != P1 {
		t.Errorf("upstream: %s", got)
	}
	if got := Rate(pol, Input{Affected: 1, Scopes: []string{"olt"}}).Level; got != P2 {
		t.Errorf("olt: %s", got)
	}
	if got := Rate(pol, Input{Affected: 1, Critical: true}).Level; got != P2 {
		t.Errorf("kritis: %s", got)
	}
	// terburuk menang
	if got := Rate(pol, Input{Affected: 60, Scopes: []string{"olt"}, Critical: true}).Level; got != P1 {
		t.Errorf("gabungan: %s", got)
	}
	// cakupan tak dikenal diabaikan
	if got := Rate(pol, Input{Affected: 1, Scopes: []string{"rumah"}}).Level; got != P4 {
		t.Errorf("cakupan asing: %s", got)
	}
}

func TestValidate(t *testing.T) {
	bad := []Policy{
		{P1Customers: 5, P2Customers: 10, Floor: "P4"},
		{P2Customers: 5, P3Customers: 9, Floor: "P4"},
		{P1Customers: -1, Floor: "P4"},
		{P1Customers: 5, Floor: "P9"},
		{P1Customers: 5, Floor: "P4", ScopeFloor: map[string]string{"x": "P1"}},
		{P1Customers: 5, Floor: "P4", ScopeFloor: map[string]string{"olt": "P7"}},
		{P1Customers: 5, Floor: "P4", CriticalFloor: "Z"},
	}
	for i, p := range bad {
		if p.Validate() == "" {
			t.Errorf("kasus %d harus ditolak", i)
		}
		if Rate(p, Input{Affected: 99}).Level != Unrated {
			t.Errorf("kasus %d: kebijakan tidak valid harus UNRATED", i)
		}
	}
	if pol.Validate() != "" {
		t.Errorf("pol harus sah: %s", pol.Validate())
	}
}

// Properti: makin banyak pelanggan, level tidak pernah lebih ringan; deterministik.
func TestMonotonDanDeterministik(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 2000; i++ {
		a := r.Intn(200)
		b := a + r.Intn(100)
		in := Input{Affected: a}
		la, lb := Rate(pol, in).Level, Rate(pol, Input{Affected: b}).Level
		if lb.Rank() > la.Rank() {
			t.Fatalf("tidak monoton: %d->%s, %d->%s", a, la, b, lb)
		}
		if Rate(pol, in).Level != la {
			t.Fatal("tidak deterministik")
		}
	}
}

func TestParseLevel(t *testing.T) {
	if l, ok := ParseLevel(" p2 "); !ok || l != P2 {
		t.Error("p2")
	}
	if _, ok := ParseLevel("UNRATED"); ok {
		t.Error("UNRATED bukan level sah untuk input")
	}
}
