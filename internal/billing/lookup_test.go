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

// TestSearchByName: cari pelanggan lewat kata kunci lokasi/nama (bukan nomor).
func TestSearchByName(t *testing.T) {
	rows := []Customer{
		{Name: "Bu Citra Jatitengah", Username: "jttcitra", Phone: "6281210797235", Area: struct {
			ID   int    `json:"id"`
			Name string `json:"name"`
		}{Name: "Jatitengah"}},
		{Name: "Farid", Username: "gdhfarid", Phone: "6281332885565"},
	}
	a := fakeBilling(t, rows, false)

	got, err := a.SearchByName(context.Background(), "Jatitengah", 10)
	if err != nil {
		t.Fatalf("SearchByName error: %v", err)
	}
	if len(got) != 1 || got[0].Username != "jttcitra" {
		t.Fatalf("SearchByName(Jatitengah) = %+v, mau 1 (jttcitra)", got)
	}

	// Tak ada -> kosong, bukan error.
	got, err = a.SearchByName(context.Background(), "tidak-ada", 10)
	if err != nil || len(got) != 0 {
		t.Fatalf("SearchByName(tidak-ada) = %d err=%v, mau kosong tanpa error", len(got), err)
	}
}

// TestSearchByNameAPIFailureIsError: API gagal -> error, bukan "tidak ditemukan".
func TestSearchByNameAPIFailureIsError(t *testing.T) {
	a := fakeBilling(t, nil, true)
	if _, err := a.SearchByName(context.Background(), "Jombor", 10); err == nil {
		t.Fatal("API gagal harus error")
	}
}

// Payload meniru bentuk NYATA API NETORA yang pernah merusak decode:
// diskon berupa STRING ("10000.00"), tgl_isolir/registration_date bisa null,
// custom_price selalu null, field tak dikenal harus diabaikan.
func TestDecodeRealisticPayload(t *testing.T) {
	const body = `{"status":"success","data":[
	 {"id":1,"name":"A","username":"ua","phone":"6281200001111","status":"active",
	  "billing_date":5,"tgl_isolir":null,"max_tunggakan":1,"registration_date":null,
	  "is_on_leave":false,"custom_price":null,"diskon":"10000.00","auto_isolir":true,
	  "package":{"id":2,"name":"10M","price":150000},"fitur_baru":{"x":1}},
	 {"id":2,"name":"B","username":"ub","phone":"081200002222","status":"inactive",
	  "billing_date":1,"tgl_isolir":10,"max_tunggakan":2,"registration_date":"2024-01-15",
	  "is_isolated":true,"isolated_since":"2026-09-01 10:00:00","diskon":null,
	  "package":{"id":2,"name":"10M","price":150000}}
	],"meta":{"total":2}}`
	srv := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(body))
	})
	a := New(srv.URL, "k")
	out, err := a.Invoke(context.Background(), "billing.get_customer", map[string]any{"identity": "u"})
	if err != nil {
		t.Fatalf("decode payload nyata gagal: %v", err)
	}
	for _, w := range []string{"ua", "ub", "ISOLIR", "isolir_sejak=2026-09-01", "tgl_isolir=10", "Rp150000"} {
		if !strings.Contains(out.Text, w) {
			t.Errorf("ringkasan tanpa %q: %s", w, out.Text)
		}
	}
	if _, err := a.Ping(context.Background()); err != nil {
		t.Fatalf("ping gagal: %v", err)
	}
}
