package billing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeNOC meniru /api/noc/v1: search pelanggan + history, keduanya X-NOC-API-Key.
func fakeNOC(t *testing.T, historyJSON string, historyStatus int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-NOC-API-Key") != "noc-key" {
			w.WriteHeader(401)
			return
		}
		switch {
		case r.URL.Path == "/api/noc/v1/customers":
			w.Write([]byte(`{"status":"success","data":[{"id":42,"name":"Pelanggan Uji","username":"uji01","status":"active"}],"meta":{"total":1}}`))
		case r.URL.Path == "/api/noc/v1/customers/42/history":
			if r.Method != http.MethodGet {
				t.Errorf("history harus GET, dapat %s", r.Method)
			}
			w.WriteHeader(historyStatus)
			w.Write([]byte(historyJSON))
		default:
			t.Errorf("path tak terduga: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
}

const histOK = `{"status":"success","data":{"customer":{"id":42,"name":"Pelanggan Uji"},"history":{"2026":[
 {"period":"2026-09","month":"09","charge_amount":150000,"status":"unpaid"},
 {"period":"2026-08","month":"08","charge_amount":"150000.00","status":"unpaid"},
 {"period":"2026-07","month":"07","charge_amount":150000,"status":"paid"}]}}}`

func TestHistoryTunggakan(t *testing.T) {
	srv := fakeNOC(t, histOK, 200)
	defer srv.Close()
	out, err := New(srv.URL, "noc-key").Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TUNGGAKAN 2 bulan", "Rp300.000", "2026-09", "Terakhir lunas: 2026-07"} {
		if !strings.Contains(out.Text, want) {
			t.Errorf("teks tidak memuat %q:\n%s", want, out.Text)
		}
	}
}

func TestHistoryLunas(t *testing.T) {
	srv := fakeNOC(t, `{"status":"success","data":{"history":{"2026":[{"period":"2026-09","charge_amount":1,"status":"paid"}]}}}`, 200)
	defer srv.Close()
	out, err := New(srv.URL, "noc-key").Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"})
	if err != nil || !strings.Contains(out.Text, "Tidak ada tunggakan") {
		t.Fatalf("err=%v teks=%s", err, out.Text)
	}
}

func TestHistoryEndpointBelumAda(t *testing.T) {
	srv := fakeNOC(t, `{"message":"Not Found"}`, 404)
	defer srv.Close()
	_, err := New(srv.URL, "noc-key").Invoke(context.Background(), ToolHistory, map[string]any{"identity": "uji01"})
	if err == nil || !strings.Contains(err.Error(), "belum tersedia") {
		t.Fatalf("harus error 'belum tersedia', dapat %v", err)
	}
}
