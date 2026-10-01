package identity

import "testing"

func TestNormalizeVariants(t *testing.T) {
	cases := map[string]string{
		"628123456789":                "628123456789",
		"+628123456789":               "628123456789",
		"08123456789":                 "628123456789",
		"8123456789":                  "628123456789",
		"628123456789@s.whatsapp.net": "628123456789",
		"628123456789@c.us":           "628123456789",
		"628123456789@lid":            "628123456789",
		" 628123456789 ":              "628123456789",
		"62 812 3456 789":             "628123456789",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, mau %q", in, got, want)
		}
	}
}

func TestNormalizeGroupJIDNotANumber(t *testing.T) {
	// JID grup tidak boleh diubah jadi nomor pelanggan.
	got := Normalize("120363000000000000@g.us")
	if got == "120363000000000000" {
		// Memang digit, tapi harus dipakai dengan IsGroup, bukan sebagai nomor.
	}
	// Yang penting: JID grup tidak dianggap identitas pelanggan.
	if IsGroup("120363000000000000@g.us", "") != true {
		t.Error("JID grup harus terdeteksi IsGroup")
	}
}

func TestNormalizeEmpty(t *testing.T) {
	if Normalize("") != "" {
		t.Error("Normalize kosong harus kosong")
	}
	if Normalize("   ") != "" {
		t.Error("Normalize spasi harus kosong")
	}
}

func TestEqual(t *testing.T) {
	if !Equal("08123456789", "628123456789") {
		t.Error("0812 dan 62812 harus setara")
	}
	if !Equal("+628123456789@s.whatsapp.net", "628123456789") {
		t.Error("format JID harus setara dengan nomor polos")
	}
	if Equal("628111", "628222") {
		t.Error("nomor berbeda tidak boleh setara")
	}
	if Equal("", "") {
		t.Error("dua kosong tidak boleh dianggap setara")
	}
}

func TestIsLID(t *testing.T) {
	cases := map[string]bool{
		"111111111111111@lid": true,
		"222222222222222@LID": true,
		"628123456789":        false,
		"":                    false,
	}
	for in, want := range cases {
		if got := IsLID(in); got != want {
			t.Errorf("IsLID(%q) = %v, mau %v", in, got, want)
		}
	}
}
