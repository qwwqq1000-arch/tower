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

// TestDecryptOrPlaintext_RoundTrip: ciphertext written by the same cipher reads
// back as plaintext through the transparent shim (vault-crypto-3).
func TestDecryptOrPlaintext_RoundTrip(t *testing.T) {
	c, _ := NewCipher(newKeyB64(t))
	enc := c.EncryptStr("mgmt-secret-xyz")
	if enc == "mgmt-secret-xyz" {
		t.Fatal("EncryptStr returned plaintext")
	}
	if got := c.DecryptOrPlaintext(enc); got != "mgmt-secret-xyz" {
		t.Fatalf("DecryptOrPlaintext(enc) = %q, want plaintext", got)
	}
}

// TestDecryptOrPlaintext_LegacyPlaintext: a legacy plaintext row (not our
// ciphertext) is returned unchanged so un-migrated rows keep working.
func TestDecryptOrPlaintext_LegacyPlaintext(t *testing.T) {
	c, _ := NewCipher(newKeyB64(t))
	if got := c.DecryptOrPlaintext("sk-legacy-plaintext"); got != "sk-legacy-plaintext" {
		t.Fatalf("legacy plaintext = %q, want unchanged", got)
	}
	// Ciphertext from a different key must NOT be returned as garbage plaintext
	// silently mangled — it is not decryptable, so it is treated as plaintext.
	other, _ := NewCipher(newKeyB64(t))
	foreign := other.EncryptStr("foreign")
	if got := c.DecryptOrPlaintext(foreign); got != foreign {
		t.Fatalf("foreign ciphertext = %q, want unchanged (treated as plaintext)", got)
	}
}

// TestNilCipher_PassThrough: a nil Cipher (plaintext-mode / no master key) is a
// transparent pass-through on both write and read so handlers can call it
// unconditionally.
func TestNilCipher_PassThrough(t *testing.T) {
	var c *Cipher
	if got := c.EncryptStr("plain"); got != "plain" {
		t.Fatalf("nil EncryptStr = %q, want pass-through", got)
	}
	if got := c.DecryptOrPlaintext("plain"); got != "plain" {
		t.Fatalf("nil DecryptOrPlaintext = %q, want pass-through", got)
	}
}
