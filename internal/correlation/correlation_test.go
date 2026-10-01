package correlation

import "testing"

func TestNormalizeBilling(t *testing.T) {
	cases := map[string]State{
		"ACTIVE":       StateActive,
		"AKTIF":        StateActive,
		"berlangganan": StateActive,
		"SUSPEND":      StateInactive,
		"BLOKIR":       StateInactive,
		"EXPIRED":      StateInactive,
		"weird-value":  StateUnknown,
	}
	for in, want := range cases {
		if got := Normalize("billing", in); got != want {
			t.Errorf("Normalize(billing, %q) = %s, mau %s", in, got, want)
		}
	}
}

func TestNormalizeRadius(t *testing.T) {
	cases := map[string]State{
		"Access-Accept": StateAuthed,
		"AUTHORIZED":    StateAuthed,
		"AUTH_OK":       StateAuthed,
		"Access-Reject": StateUnauthed,
		"FAIL":          StateUnauthed,
		"gagal":         StateUnauthed,
	}
	for in, want := range cases {
		if got := Normalize("radius", in); got != want {
			t.Errorf("Normalize(radius, %q) = %s, mau %s", in, got, want)
		}
	}
}

func TestNormalizeMikrotik(t *testing.T) {
	cases := map[string]State{
		"RUNNING":    StatePPPoEUp,
		"ACTIVE":     StatePPPoEUp,
		"DISCONNECT": StatePPPoEDown,
		"OFFLINE":    StatePPPoEDown,
	}
	for in, want := range cases {
		if got := Normalize("mikrotik", in); got != want {
			t.Errorf("Normalize(mikrotik, %q) = %s, mau %s", in, got, want)
		}
	}
}

func TestNormalizeGenieACS(t *testing.T) {
	cases := map[string]State{
		"ONLINE":           StateDeviceOn,
		"1 devices online": StateDeviceOn,
		"OFFLINE":          StateDeviceOff,
		"LOS":              StateDeviceOff,
	}
	for in, want := range cases {
		if got := Normalize("genieacs", in); got != want {
			t.Errorf("Normalize(genieacs, %q) = %s, mau %s", in, got, want)
		}
	}
}

func TestCorrelateStalePPPoE(t *testing.T) {
	s := NewSet()
	s.Add("billing", "ACTIVE")
	s.Add("radius", "AUTH_OK")
	s.Add("genieacs", "ONLINE")
	s.Add("mikrotik", "OFFLINE")

	c := s.Correlate("billing", "radius", "mikrotik", "genieacs")
	if c.PrimaryArea != "mikrotik" {
		t.Errorf("primary_area = %s, mau mikrotik", c.PrimaryArea)
	}
	if !c.Complete {
		t.Error("semua domain terisi, harus complete")
	}
	if c.Confidence < 0.8 {
		t.Errorf("confidence = %.2f, mau >= 0.8", c.Confidence)
	}
}

func TestCorrelateDeviceOffline(t *testing.T) {
	s := NewSet()
	s.Add("billing", "ACTIVE")
	s.Add("genieacs", "OFFLINE")

	c := s.Correlate("billing", "genieacs")
	if c.PrimaryArea != "genieacs" {
		t.Errorf("primary_area = %s, mau genieacs", c.PrimaryArea)
	}
}

func TestCorrelateBillingInactive(t *testing.T) {
	s := NewSet()
	s.Add("billing", "SUSPEND")

	c := s.Correlate("billing")
	if c.PrimaryArea != "billing" {
		t.Errorf("primary_area = %s, mau billing", c.PrimaryArea)
	}
}

func TestCorrelateMissingLowersConfidence(t *testing.T) {
	s := NewSet()
	s.Add("billing", "ACTIVE")
	// radius/mikrotik/genieacs hilang
	c := s.Correlate("billing", "radius", "mikrotik", "genieacs")
	if c.Complete {
		t.Error("domain hilang -> tidak complete")
	}
	if len(c.Missing) != 3 {
		t.Errorf("missing = %v, mau 3 domain", c.Missing)
	}
	if c.Confidence >= 0.8 {
		t.Errorf("confidence = %.2f, mau rendah karena banyak missing", c.Confidence)
	}
}

func TestUnknownLowersConfidence(t *testing.T) {
	full := NewSet()
	full.Add("billing", "ACTIVE")
	full.Add("radius", "AUTH_OK")
	full.Add("genieacs", "ONLINE")
	full.Add("mikrotik", "OFFLINE")
	cFull := full.Correlate("billing", "radius", "mikrotik", "genieacs")

	withUnknown := NewSet()
	withUnknown.Add("billing", "ACTIVE")
	withUnknown.Add("radius", "AUTH_OK")
	withUnknown.Add("genieacs", "ONLINE")
	withUnknown.Add("mikrotik", "nilai-aneh-tak-dikenal")
	cUnk := withUnknown.Correlate("billing", "radius", "mikrotik", "genieacs")

	if cUnk.Confidence >= cFull.Confidence {
		t.Errorf("bukti UNKNOWN harus menurunkan keyakinan: full=%.2f unk=%.2f", cFull.Confidence, cUnk.Confidence)
	}
}

func TestMissingDomainList(t *testing.T) {
	s := NewSet()
	s.Add("billing", "ACTIVE")
	got := s.Missing("billing", "radius", "mikrotik")
	if len(got) != 2 || got[0] != "radius" || got[1] != "mikrotik" {
		t.Errorf("Missing = %v, mau [radius mikrotik]", got)
	}
}
