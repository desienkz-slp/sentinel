package billing

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// fakeBilling meniru search NETORA: pencocokan SUBSTRING pada teks mentah
// nama/username/phone (tanpa normalisasi nomor).
func fakeBilling(t *testing.T, rows []Customer, fail bool) *Adapter {
	t.Helper()
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		if fail {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		q := strings.ToLower(r.URL.Query().Get("search"))
		var out []Customer
		for _, c := range rows {
			if q == "" || strings.Contains(strings.ToLower(c.Phone+" "+c.Name+" "+c.Username), q) {
				out = append(out, c)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(customersResponse{Status: "success", Data: out})
	})
	return New(srv.URL, "k")
}

func TestLookupByPhoneHandlesStoredFormats(t *testing.T) {
	rows := []Customer{
		{Name: "Polos", Username: "u-polos", Phone: "6281200001111"},
		{Name: "Nol", Username: "u-nol", Phone: "081200002222"},
		{Name: "Spasi", Username: "u-spasi", Phone: "+62 812-0000-3333"},
		{Name: "Pendek", Username: "u-pendek", Phone: "62"},
	}
	a := fakeBilling(t, rows, false)
	for _, tc := range []struct{ query, want string }{
		{"6281200001111", "u-polos"},
		{"6281200002222", "u-nol"},   // WA kirim 62..., billing simpan 0...
		{"6281200003333", "u-spasi"}, // billing simpan "+62 812-0000-3333"
		{"+62 812-0000-1111", "u-polos"},
	} {
		c, ok, err := a.LookupByPhone(context.Background(), tc.query)
		if err != nil || !ok || c.Username != tc.want {
			t.Errorf("LookupByPhone(%q) = %q ok=%v err=%v, mau %q", tc.query, c.Username, ok, err, tc.want)
		}
	}
	if _, ok, _ := a.LookupByPhone(context.Background(), "6289999999999"); ok {
		t.Error("nomor tak terdaftar tidak boleh cocok")
	}
	if _, ok, _ := a.LookupByPhone(context.Background(), "62"); ok {
		t.Error("nomor terlalu pendek tidak boleh cocok")
	}
}

func TestLookupByPhoneAPIFailureIsErrorNotNotFound(t *testing.T) {
	a := fakeBilling(t, nil, true)
	_, ok, err := a.LookupByPhone(context.Background(), "6281200001111")
	if ok || err == nil {
		t.Fatalf("API gagal harus error, bukan 'bukan pelanggan': ok=%v err=%v", ok, err)
	}
}

func TestPhoneVariants(t *testing.T) {
	got := phoneVariants("+62 812-0000-3333")
	if len(got) != 2 || got[0] != "81200003333" || got[1] != "3333" {
		t.Fatalf("phoneVariants = %v", got)
	}
}

func TestGetCustomerEscapesQuery(t *testing.T) {
	var raw string
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		raw = r.URL.RawQuery
		json.NewEncoder(w).Encode(customersResponse{Status: "success"})
	})
	a := New(srv.URL, "k")
	if _, err := a.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "a+b#c d"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "search=a%2Bb%23c+d") {
		t.Fatalf("query tidak di-escape benar: %q", raw)
	}
}
