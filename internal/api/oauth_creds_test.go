package api

import (
	"crypto/rand"
	"encoding/base64"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
)

// newTestCipher builds a *crypto.Cipher with a random 32-byte key for tests.
func newTestCipher(t *testing.T) *crypto.Cipher {
	t.Helper()
	k := make([]byte, 32)
	if _, err := rand.Read(k); err != nil {
		t.Fatal(err)
	}
	c, err := crypto.NewCipher(base64.StdEncoding.EncodeToString(k))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// TestEncryptImportCred verifies that encryptImportCred stores a non-empty
// ciphertext when a cipher is provided, and that it decrypts back to the
// original value (vault-crypto-2).
func TestEncryptImportCred(t *testing.T) {
	c := newTestCipher(t)
	original := "user@example.com"

	enc := encryptImportCred(c, original)

	// Must not be empty.
	if enc == "" {
		t.Fatal("encryptImportCred returned empty string with a real cipher")
	}
	// Must not equal plaintext (would mean encryption was bypassed).
	if enc == original {
		t.Fatalf("encryptImportCred returned plaintext %q unchanged", original)
	}
	// Must decrypt back to the original.
	if got := c.DecryptOrPlaintext(enc); got != original {
		t.Fatalf("DecryptOrPlaintext(enc) = %q, want %q", got, original)
	}
}

// TestEncryptImportCred_NilCipher verifies that encryptImportCred with a nil
// cipher returns the plaintext unchanged (pass-through for keyless deployments).
func TestEncryptImportCred_NilCipher(t *testing.T) {
	enc := encryptImportCred(nil, "some-value")
	if enc != "some-value" {
		t.Fatalf("nil cipher: got %q, want pass-through", enc)
	}
}

// TestEncryptImportCred_Empty verifies that encryptImportCred with an empty
// string returns empty (no spurious ciphertext for empty credentials).
func TestEncryptImportCred_Empty(t *testing.T) {
	c := newTestCipher(t)
	enc := encryptImportCred(c, "")
	if enc != "" {
		t.Fatalf("empty cred: got %q, want empty", enc)
	}
}

// TestDecryptImportCred_RoundTrip verifies the decrypt-on-use path (vault-crypto-2):
// decryptImportCred correctly reverses encryptImportCred, providing the read
// half of the encrypt-at-rest / decrypt-on-use contract.
func TestDecryptImportCred_RoundTrip(t *testing.T) {
	c := newTestCipher(t)
	cases := []struct {
		name  string
		plain string
	}{
		{"email", "user@example.com"},
		{"profileId", "prof_abc123"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			enc := encryptImportCred(c, tc.plain)
			got := decryptImportCred(c, enc)
			if got != tc.plain {
				t.Fatalf("round-trip mismatch: encryptImportCred(%q) → %q → decryptImportCred → %q, want %q",
					tc.plain, enc, got, tc.plain)
			}
		})
	}
}

// TestDecryptImportCred_NilCipher verifies pass-through for keyless deployments.
func TestDecryptImportCred_NilCipher(t *testing.T) {
	got := decryptImportCred(nil, "some-value")
	if got != "some-value" {
		t.Fatalf("nil cipher: got %q, want pass-through", got)
	}
}

// TestDecryptImportCred_LegacyPlaintext verifies that legacy plaintext rows
// (written before encryption was enabled) are returned unchanged.
func TestDecryptImportCred_LegacyPlaintext(t *testing.T) {
	c := newTestCipher(t)
	// a value that was never encrypted (plaintext row from before vault-crypto-2)
	legacy := "user@legacy.com"
	got := decryptImportCred(c, legacy)
	if got != legacy {
		t.Fatalf("legacy plaintext: got %q, want %q", got, legacy)
	}
}
