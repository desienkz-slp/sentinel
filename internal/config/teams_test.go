package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNormalizeTeamModeGagalTertutup(t *testing.T) {
	cases := map[string]TeamMode{
		"": TeamOff, "off": TeamOff, "OFF": TeamOff, "  off ": TeamOff,
		"on": TeamOn, "ON": TeamOn, "aktif": TeamOn, "true": TeamOn, "1": TeamOn,
		"shadow": TeamShadow, "Shadow": TeamShadow, "bayangan": TeamShadow,
		// nilai sembarang / salah ketik TIDAK boleh menyalakan fitur
		"onn": TeamOff, "yes": TeamOff, "enable": TeamOff, "shado": TeamOff, "0": TeamOff, "null": TeamOff,
	}
	for in, want := range cases {
		if got := NormalizeTeamMode(in); got != want {
			t.Errorf("NormalizeTeamMode(%q) = %q, mau %q", in, got, want)
		}
	}
}

func TestTeamDefaultSemuaOff(t *testing.T) {
	f := Default().Teams()
	for name, m := range map[string]TeamMode{"routing": f.Routing, "cs_scope": f.CSScope, "handoff": f.Handoff, "severity": f.Severity, "presenter": f.Presenter} {
		if m != TeamOff {
			t.Errorf("default %s = %q, harus off (perilaku lama tak boleh berubah)", name, m)
		}
	}
}

func TestTeamFlagBertahanSaveLoad(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte(`{"addr":":8090"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	c.TeamRouting = "shadow"
	c.TeamCSScope = "on"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	f := Load(p).Teams()
	if f.Routing != TeamShadow || f.CSScope != TeamOn || f.Handoff != TeamOff {
		t.Fatalf("flag tidak bertahan: %+v", f)
	}
}

func TestTeamFlagTampilDiRedacted(t *testing.T) {
	c := Default()
	c.TeamRouting = "shadow"
	if got := c.Redacted()["team_routing"]; got != "shadow" {
		t.Fatalf("redacted team_routing = %v", got)
	}
	c.TeamRouting = "ngawur"
	if got := c.Redacted()["team_routing"]; got != "off" {
		t.Fatalf("nilai ngawur harus tampil off, dapat %v", got)
	}
}
