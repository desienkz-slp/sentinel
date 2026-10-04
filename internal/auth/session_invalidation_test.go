package auth

import (
	"testing"
	"time"
)

func TestSessionsAreUnique(t *testing.T) {
	s := New("salt", nil, 0)
	s.SetUser("operator", "old")
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		token := s.IssueSession("operator")
		if seen[token] {
			t.Fatal("rapid logins reused the same token")
		}
		seen[token] = true
	}
}

func TestPasswordResetDoesNotResurrectSessions(t *testing.T) {
	s := New("salt", nil, 0)
	s.SetUser("operator", "old")
	old := s.IssueSession("operator")
	if err := s.ResetPassword("operator", "new"); err != nil {
		t.Fatal(err)
	}
	if s.ValidateSession(old) {
		t.Fatal("reset did not invalidate old token")
	}
	fresh := s.IssueSession("operator")
	if fresh == old || s.ValidateSession(old) {
		t.Fatal("new login resurrected pre-reset token")
	}
	if !s.ValidateSession(fresh) {
		t.Fatal("fresh session invalid")
	}
}

func TestRevokeSessionIsScopedAndPermanent(t *testing.T) {
	s := New("salt", nil, 0)
	s.SetUser("operator", "password")
	revoked := s.IssueSession("operator")
	other := s.IssueSession("operator")
	s.RevokeSession(revoked)
	s.RevokeSession(revoked)
	s.RevokeSession("")
	s.RevokeSession("unknown")
	fresh := s.IssueSession("operator")
	if s.ValidateSession(revoked) {
		t.Fatal("revoked session resurrected")
	}
	if !s.ValidateSession(other) || !s.ValidateSession(fresh) {
		t.Fatal("logout invalidated another session")
	}
}

func TestSessionExpiry(t *testing.T) {
	s := New("salt", nil, 0)
	token := s.IssueSession("operator")
	s.mu.Lock()
	s.sessions[token] = time.Now().Add(-time.Second)
	s.mu.Unlock()
	if s.ValidateSession(token) {
		t.Fatal("expired session valid")
	}
}
