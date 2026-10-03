package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ainoc/internal/config"
)

func newStaffServer(t *testing.T) *Server {
	t.Helper()
	cfg := &config.Config{Addr: "127.0.0.1:8090", NOCNumber: "628111222333", AdminNumber: "628111222444"}
	cfg.SetPath(t.TempDir() + "/config.json")
	s := &Server{cfg: cfg, pinSesi: newPINStore(60)}
	if !migrateLegacyStaff(cfg) {
		t.Fatal("migrasi nomor lama harus memindahkan data")
	}
	s.syncDirectory()
	return s
}

func TestMigrateLegacyStaffMakesEntriesEditableAndDeletable(t *testing.T) {
	s := newStaffServer(t)
	if len(s.cfg.StaffMembers) != 2 {
		t.Fatalf("staf hasil migrasi = %d, mau 2", len(s.cfg.StaffMembers))
	}
	if migrateLegacyStaff(s.cfg) {
		t.Fatal("migrasi harus idempoten")
	}
	if got := s.escalationTarget("network"); got != "628111222333" {
		t.Fatalf("target network = %q", got)
	}

	// Edit (upsert) nama NOC.
	r := httptest.NewRequest(http.MethodPost, "/api/staff", strings.NewReader(`{"number":"628111222333","name":"Staf Uji","role":"noc_senior","active":true}`))
	w := httptest.NewRecorder()
	s.handleStaffUpsert(w, r)
	if w.Code != 200 {
		t.Fatalf("upsert status %d: %s", w.Code, w.Body.String())
	}
	if m, ok := s.dir.Lookup("628111222333"); !ok || m.Name != "Staf Uji" {
		t.Fatalf("edit tidak berlaku: %+v ok=%v", m, ok)
	}

	// Hapus benar-benar hilang (tidak muncul lagi dari config lama).
	r = httptest.NewRequest(http.MethodDelete, "/api/staff?nomor=628111222444", nil)
	w = httptest.NewRecorder()
	s.handleStaffDelete(w, r)
	if w.Code != 200 {
		t.Fatalf("delete status %d: %s", w.Code, w.Body.String())
	}
	if _, ok := s.dir.Lookup("628111222444"); ok {
		t.Fatal("staf terhapus masih dikenali")
	}
	s.syncDirectory()
	if _, ok := s.dir.Lookup("628111222444"); ok {
		t.Fatal("staf terhapus muncul lagi setelah sync")
	}
	if got := s.escalationTarget("billing"); got != "" {
		t.Fatalf("tanpa admin aktif, target billing harus kosong, dapat %q", got)
	}

	// Hapus yang tidak ada -> 404 jujur.
	r = httptest.NewRequest(http.MethodDelete, "/api/staff?nomor=628999", nil)
	w = httptest.NewRecorder()
	s.handleStaffDelete(w, r)
	if w.Code != 404 {
		t.Fatalf("hapus tak ada: status %d, mau 404", w.Code)
	}
}
