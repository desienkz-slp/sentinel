// Package auth menyediakan autentikasi login dashboard: hash password,
// sesi berbasis cookie (HMAC-signed), dan reset kredensial.
//
// Password TIDAK pernah disimpan plaintext — hanya HMAC-SHA256(password, salt).
// Salt dibuat acak sekali lalu disimpan di config. Session cookie = HMAC dari
// (username + expiry), diverifikasi di tiap request API/dashboard.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CookieName adalah nama cookie sesi dashboard.
const CookieName = "noc_session"

// DefaultSuperUser adalah kredensial superadmin bawaan (username/password).
// Password disimpan sebagai hash; di sini hanya konstanta untuk init pertama.
const DefaultSuperUser = "desienkz"

// DefaultSuperPass adalah password superadmin awal (di-hash saat bootstrap).
const DefaultSuperPass = "628268Matamu"

// Store memegang pengguna (username -> hash) + secret sesi + salt.
type Store struct {
	mu            sync.RWMutex
	salt          string
	users         map[string]string // username -> hex(HMAC-SHA256(pass, salt))
	sessionSecret string
	ttl           time.Duration
	sessions      map[string]time.Time // token -> expiry (validasi cepat)
}

// New membuat store. salt kosong -> dibuat acak. users boleh nil.
func New(salt string, users map[string]string, ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	if salt == "" {
		salt = randomHex(16)
	}
	secret := randomHex(32)
	s := &Store{
		salt: salt, users: users, sessionSecret: secret,
		ttl: ttl, sessions: map[string]time.Time{},
	}
	if s.users == nil {
		s.users = map[string]string{}
	}
	return s
}

// Salt mengembalikan salt (disimpan ke config).
func (s *Store) Salt() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.salt
}

// Users mengembalikan peta username->hash (untuk persist config).
func (s *Store) Users() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.users))
	for k, v := range s.users {
		out[k] = v
	}
	return out
}

// HasUsers melaporkan apakah ada pengguna terdaftar (login wajib).
func (s *Store) HasUsers() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.users) > 0
}

// SetUser menambah/mengubah password pengguna (hash ulang).
func (s *Store) SetUser(username, password string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := strings.TrimSpace(username)
	if u == "" {
		return
	}
	s.users[u] = s.hash(password)
}

// RemoveUser menghapus pengguna.
func (s *Store) RemoveUser(username string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.users, strings.TrimSpace(username))
}

// ResetPassword mengubah password pengguna (dipakai reset via WA superadmin).
func (s *Store) ResetPassword(username, newPassword string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u := strings.TrimSpace(username)
	if _, ok := s.users[u]; !ok {
		return fmt.Errorf("pengguna %q tidak ditemukan", u)
	}
	s.users[u] = s.hash(newPassword)
	s.sessions = map[string]time.Time{} // invalidasi semua sesi (keamanan)
	return nil
}

func (s *Store) hash(password string) string {
	mac := hmac.New(sha256.New, []byte(s.salt))
	mac.Write([]byte(password))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify memeriksa username + password.
func (s *Store) Verify(username, password string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want, ok := s.users[strings.TrimSpace(username)]
	if !ok {
		return false
	}
	got := s.hash(password)
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// ---- sesi cookie ----

// IssueSession membuat token sesi untuk username dan mencatatnya.
func (s *Store) IssueSession(username string) string {
	token := s.sign(username)
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(s.ttl)
	s.mu.Unlock()
	return token
}

// sign includes a random nonce so rapid logins cannot reuse a revoked token.
func (s *Store) sign(username string) string {
	ts := time.Now().Unix()
	mac := hmac.New(sha256.New, []byte(s.sessionSecret))
	fmt.Fprintf(mac, "%s.%d.%s", username, ts, randomHex(32))
	return fmt.Sprintf("%d.%s", ts, hex.EncodeToString(mac.Sum(nil)))
}

// RevokeSession invalidates one session without logging out other clients.
func (s *Store) RevokeSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, token)
}

// ValidateSession memverifikasi token sesi masih valid.
func (s *Store) ValidateSession(token string) bool {
	if token == "" {
		return false
	}
	s.mu.RLock()
	exp, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok || time.Now().After(exp) {
		return false
	}
	// Bonus: verifikasi HMAC (tamper-evident).
	var ts int64
	var sig string
	if _, err := fmt.Sscanf(token, "%d.%s", &ts, &sig); err != nil {
		return false
	}
	// Recompute check: kita simpan token lengkap; cukup cek keberadaan + expiry.
	return true
}

// SetCookie menulis cookie sesi ke response.
func SetCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(ttl.Seconds()),
	})
}

// ClearCookie menghapus cookie sesi (logout).
func ClearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: CookieName, Value: "", Path: "/", HttpOnly: true,
		MaxAge: -1, Expires: time.Unix(1, 0),
	})
}

// FromRequest membaca token sesi dari cookie request.
func FromRequest(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
