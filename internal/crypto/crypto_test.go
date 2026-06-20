package crypto

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
)

func newKeyB64(t *testing.T) string {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(k)
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	c, err := NewCipher(newKeyB64(t))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("oauth-refresh-token-xyz")
	enc, err := c.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	if enc == string(plain) {
		t.Fatal("ciphertext equals plaintext")
	}
	got, err := c.Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(plain) {
		t.Fatalf("round-trip = %q, want %q", got, plain)
	}
}

func TestNewCipher_BadKeyLength(t *testing.T) {
	if _, err := NewCipher(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("expected error for non-32-byte key")
	}
}

func TestDecrypt_TamperFails(t *testing.T) {
	c, _ := NewCipher(newKeyB64(t))
	enc, _ := c.Encrypt([]byte("secret"))
	raw, _ := base64.StdEncoding.DecodeString(enc)
	raw[len(raw)-1] ^= 0x01
	bad := base64.StdEncoding.EncodeToString(raw)
	if _, err := c.Decrypt(bad); err == nil {
		t.Fatal("expected error decrypting tampered ciphertext")
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	c1, _ := NewCipher(newKeyB64(t))
	c2, _ := NewCipher(newKeyB64(t))
	enc, _ := c1.Encrypt([]byte("secret"))
	if _, err := c2.Decrypt(enc); err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}
