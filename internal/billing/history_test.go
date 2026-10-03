package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeV1 meniru dua endpoint: NOC search (X-NOC-API-Key) dan /api/v1 history (Bearer).
func fakeV1(t *testing.T, bearer string, historyJSON string, historyStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/noc/v1/customers"):
			if r.Header.Get("X-NOC-API-Key") != "noc-key" {
				w.WriteHeader(401)
				return
			}
			w.Write([]byte(`{"status":"success","data":[{"id":42,"name":"Pelanggan Uji","username":"uji01","status":"active"}],"meta":{"total":1}}`))
		case r.URL.Path == "/api/v1/customers/42/history":
			if r.Method != http.MethodGet {
				t.Errorf("history harus GET, dapat %s", r.Method)
			}
			if r.Header.Get("Authorization") != "Bearer "+bearer {
				w.WriteHeader(401)
				return
			}
			w.WriteHeader(historyStatus)
			w.Write([]byte(historyJSON))
		default:
			t.Errorf("path tak terduga: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

const histOK = `{"success":true,"data":{"history":{"2026":[
 {"period":"2026-09","month":"09","charge_amount":150000,"status":"unpaid"},
 {"period":"2026-08","month":"08","charge_amount":"150000.00","status":"unpaid"},
 {"period":"2026-07","month":"07","charge_amount":150000,"status":"paid"}]},
 "wa_template":{"x":["bukan","angka"]}}}`

func TestHistoryTunggakan(t *testing.T) {
	srv := fakeV1(t, "tok-bearer", histOK, 200)
	defer srv.Close()
	a := New(srv.URL, "noc-key").WithUserToken(srv.URL, "tok-bearer")
	out, err := a.Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TUNGGAKAN 2 bulan", "Rp300.000", "2026-09", "Terakhir lunas: 2026-07"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("teks tidak memuat %q:\n%s", want, out.Text)
		}
	}
}

func TestHistoryTanpaToken(t *testing.T) {
	srv := fakeV1(t, "x", histOK, 200)
	defer srv.Close()
	a := New(srv.URL, "noc-key")
	if _, err := a.Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"}); err == nil || !strings.Contains(err.Error(), "belum dikonfigurasi") {
		t.Fatalf("harus error 'belum dikonfigurasi', dapat %v", err)
	}
}

func TestHistoryTokenDitolak(t *testing.T) {
	srv := fakeV1(t, "benar", histOK, 200)
	defer srv.Close()
	a := New(srv.URL, "noc-key").WithUserToken(srv.URL, "salah")
	_, err := a.Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"})
	if err == nil || !strings.Contains(err.Error(), "ditolak") {
		t.Fatalf("harus error 'ditolak', dapat %v", err)
	}
}

func TestHistoryLunas(t *testing.T) {
	srv := fakeV1(t, "t", `{"success":true,"data":{"history":{"2026":[{"period":"2026-09","charge_amount":1,"status":"paid"}]},"wa_template":{}}}`, 200)
	defer srv.Close()
	a := New(srv.URL, "noc-key").WithUserToken(srv.URL, "t")
	out, err := a.Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"})
	if err != nil || !strings.Contains(out.Text, "Tidak ada tunggakan") {
		t.Fatalf("err=%v teks=%s", err, out.Text)
	}
}
