package netinstall

import "testing"

func TestCatalogAllowlist(t *testing.T) {
	// Semua katalog harus punya nama + cara pakai (bertindak sebagai skill).
	for _, info := range Catalog {
		if info.Name == "" || info.Pkg == "" || info.Usage == "" || info.Description == "" {
			t.Errorf("katalog %+v tidak lengkap (name/pkg/usage/description wajib)", info)
		}
	}
}

func TestLookupAndAllowed(t *testing.T) {
	if _, ok := Lookup("nmap"); !ok {
		t.Fatal("nmap harus ada di katalog")
	}
	if _, ok := Lookup("nmapX"); ok {
		t.Fatal("nmapX tidak boleh ada")
	}
	// Case-insensitive.
	if _, ok := Lookup("NMAP"); !ok {
		t.Fatal("lookup harus case-insensitive")
	}
	// Paket net-tools -> nama net-tools (binary netstat).
	if _, ok := Lookup("net-tools"); !ok {
		t.Fatal("net-tools harus ada")
	}
}

func TestIsAllowed(t *testing.T) {
	if !IsAllowed("tcpdump") {
		t.Fatal("tcpdump harus diizinkan")
	}
	if IsAllowed("rm") {
		t.Fatal("rm TIDAK boleh ada di allowlist")
	}
	if IsAllowed("apt-get") {
		t.Fatal("apt-get sendiri TIDAK boleh jadi tool yang diinstal")
	}
}

func TestNamesSorted(t *testing.T) {
	n := Names()
	for i := 1; i < len(n); i++ {
		if n[i-1] >= n[i] {
			t.Fatalf("Names tidak terurut: %v", n)
		}
	}
}
