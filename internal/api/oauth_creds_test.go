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
