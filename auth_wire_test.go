package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"ainoc/internal/auth"
	"ainoc/internal/config"
	"ainoc/internal/directory"
)

func TestAuthLogoutRevokesSession(t *testing.T) {
	s := &Server{auth: auth.New("test-salt", nil, 0)}
	s.auth.SetUser("operator", "test-password")
	mux := http.NewServeMux()
	s.registerAuthRoutes(mux)
	login := httptest.NewRecorder()
	mux.ServeHTTP(login, httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"username":"operator","password":"test-password"}`)))
	if login.Code != http.StatusOK {
		t.Fatalf("login: %d", login.Code)
	}
	cookie := login.Result().Cookies()[0]
	if !s.auth.ValidateSession(cookie.Value) {
		t.Fatal("login did not issue a valid session")
	}
	request := func(method, path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r)
		return w
	}
	if w := request(http.MethodGet, "/api/auth/logout"); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET logout: %d", w.Code)
	}
	if !s.auth.ValidateSession(cookie.Value) {
		t.Fatal("GET logout revoked session")
	}
	w := request(http.MethodPost, "/api/auth/logout")
	if w.Code != http.StatusOK {
		t.Fatalf("logout: %d", w.Code)
	}
	if cookies := w.Result().Cookies(); len(cookies) != 1 || cookies[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
	if s.auth.ValidateSession(cookie.Value) {
		t.Fatal("logged-out token remains valid on server")
	}
	if w := request(http.MethodGet, "/api/auth/session"); w.Code != http.StatusUnauthorized {
		t.Fatalf("replayed session: %d", w.Code)
	}
	protected := s.authRequired(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	r := httptest.NewRequest(http.MethodGet, "/api/metrics", nil)
	r.AddCookie(cookie)
	denied := httptest.NewRecorder()
	protected.ServeHTTP(denied, r)
	if denied.Code != http.StatusUnauthorized {
		t.Fatalf("replayed cookie passed auth middleware: %d", denied.Code)
	}
	if w := request(http.MethodPost, "/api/auth/logout"); w.Code != http.StatusOK {
		t.Fatalf("repeated logout: %d", w.Code)
	}
}

func TestResetLoginViaWARequiresStaffSuperadmin(t *testing.T) {
	for _, caller := range []directory.Caller{
		{},
		{IsStaff: true, Role: directory.RoleAdmin},
		{IsStaff: true, Role: directory.RoleNOCSenior},
		{Role: directory.RoleSuperAdmin},
	} {
		s := &Server{auth: auth.New("test-salt", nil, 0), cfg: &config.Config{}}
		s.auth.SetUser("operator", "old-password")
		token := s.auth.IssueSession("operator")
		s.resetLoginViaWA(caller, "reset login operator new-password")
		if !s.auth.Verify("operator", "old-password") || !s.auth.ValidateSession(token) {
			t.Fatalf("unauthorized caller changed credentials or sessions: %+v", caller)
		}
	}
}

func TestResetLoginViaWASuperadminPersistsAndRevokes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{auth: auth.New("test-salt", nil, 0), cfg: config.Load(path)}
	s.auth.SetUser("operator", "old-password")
	token := s.auth.IssueSession("operator")
	caller := directory.Caller{IsStaff: true, Role: directory.RoleSuperAdmin}
	if _, handled := s.resetLoginViaWA(caller, "not a reset command"); handled {
		t.Fatal("unrelated command handled")
	}
	reply, handled := s.resetLoginViaWA(caller, "reset login operator new-password")
	if !handled || !strings.Contains(reply, "berhasil") {
		t.Fatalf("reset: %q handled=%v", reply, handled)
	}
	if s.auth.ValidateSession(token) || !s.auth.Verify("operator", "new-password") {
		t.Fatal("reset did not update credentials and revoke sessions")
	}
	saved := config.Load(path)
	if !auth.New(saved.AuthSalt, saved.AuthUsers, 0).Verify("operator", "new-password") {
		t.Fatal("reset not persisted")
	}
}

func TestAuthLogoutWithoutStore(t *testing.T) {
	mux := http.NewServeMux()
	(&Server{}).registerAuthRoutes(mux)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/auth/logout", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("logout without auth: %d", w.Code)
	}
}
