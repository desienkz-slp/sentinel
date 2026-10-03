package auth

import "testing"

func TestSetAndVerify(t *testing.T) {
	s := New("salt123", nil, 0)
	s.SetUser("desienkz", "628268Matamu")
	if !s.Verify("desienkz", "628268Matamu") {
		t.Fatal("password benar harus lolos")
	}
	if s.Verify("desienkz", "salah") {
		t.Fatal("password salah harus ditolak")
	}
	if s.Verify("tidakada", "628268Matamu") {
		t.Fatal("username tak dikenal harus ditolak")
	}
}

func TestResetPassword(t *testing.T) {
	s := New("salt123", nil, 0)
	s.SetUser("desienkz", "lama")
	if err := s.ResetPassword("desienkz", "baru"); err != nil {
		t.Fatalf("ResetPassword: %v", err)
	}
	if !s.Verify("desienkz", "baru") {
		t.Fatal("password baru harus berlaku")
	}
	if s.Verify("desienkz", "lama") {
		t.Fatal("password lama harus tidak berlaku")
	}
	if err := s.ResetPassword("tidakada", "x"); err == nil {
		t.Fatal("reset user tak dikenal harus error")
	}
}

func TestSession(t *testing.T) {
	s := New("salt", nil, 0)
	s.SetUser("desienkz", "pass")
	tok := s.IssueSession("desienkz")
	if !s.ValidateSession(tok) {
		t.Fatal("sesi valid harus lolos")
	}
	if s.ValidateSession("random") {
		t.Fatal("token asal harus ditolak")
	}
}

func TestSaltAndUsersPersist(t *testing.T) {
	s := New("", nil, 0)
	if s.Salt() == "" {
		t.Fatal("salt kosong harus dibuat acak")
	}
	s.SetUser("a", "b")
	users := s.Users()
	if users["a"] == "" {
		t.Fatal("hash tidak boleh kosong")
	}
	if users["a"] == "b" {
		t.Fatal("password TIDAK boleh disimpan plaintext")
	}
}
