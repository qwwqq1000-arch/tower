package auth

import "testing"

func TestHashVerifyPassword(t *testing.T) {
	hash, salt, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || salt == "" {
		t.Fatal("empty hash/salt")
	}
	if !VerifyPassword("correct horse battery staple", hash, salt) {
		t.Fatal("verify should succeed for correct password")
	}
	if VerifyPassword("wrong", hash, salt) {
		t.Fatal("verify should fail for wrong password")
	}
}

func TestHashPassword_UniqueSalt(t *testing.T) {
	h1, s1, _ := HashPassword("same")
	h2, s2, _ := HashPassword("same")
	if s1 == s2 {
		t.Fatal("salts must differ")
	}
	if h1 == h2 {
		t.Fatal("hashes must differ due to salt")
	}
}
