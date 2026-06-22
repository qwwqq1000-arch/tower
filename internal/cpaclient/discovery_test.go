package cpaclient

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func testCipher(t *testing.T) *crypto.Cipher {
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

// TestSync_DecryptsMgmtKeyAtRest is the node round-trip for vault-crypto-3: a
// node's mgmt_key is stored as ciphertext (encrypt-on-write), and Sync must
// decrypt it transparently before sending it as the Bearer secret to the CPA
// management API (decrypt-on-read → use). The server requires the *plaintext*
// secret; if Sync forwarded the raw ciphertext it would 401. An empty file list
// keeps this a pure unit test (no DB / nil queries).
func TestSync_DecryptsMgmtKeyAtRest(t *testing.T) {
	const plaintextSecret = "cpa-mgmt-secret-123"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+plaintextSecret {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"files":[]}`))
	}))
	defer srv.Close()

	cipher := testCipher(t)
	encMgmt := cipher.EncryptStr(plaintextSecret) // encrypt-on-write
	if encMgmt == plaintextSecret {
		t.Fatal("mgmt_key was not encrypted")
	}

	node := sqlc.Node{
		ID:      "n_cpa",
		Kind:    "cpa",
		BaseUrl: srv.URL,
		MgmtKey: encMgmt, // stored as ciphertext at rest
		Enabled: true,
	}
	rot := &RotateConfig{Cipher: cipher}

	// nil q is safe: an empty file list means Sync never upserts.
	n, err := Sync(context.Background(), nil, node, rot)
	if err != nil {
		t.Fatalf("Sync with encrypted mgmt_key failed (ciphertext not decrypted before Bearer?): %v", err)
	}
	if n != 0 {
		t.Fatalf("want 0 discovered accounts, got %d", n)
	}
}

// TestSync_LegacyPlaintextMgmtKey: a node whose mgmt_key is still legacy
// plaintext (written before encryption-at-rest) must keep working via the
// transparent read shim.
func TestSync_LegacyPlaintextMgmtKey(t *testing.T) {
	const plaintextSecret = "legacy-plaintext-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+plaintextSecret {
			w.WriteHeader(401)
			return
		}
		_, _ = w.Write([]byte(`{"files":[]}`))
	}))
	defer srv.Close()

	node := sqlc.Node{
		ID:      "n_cpa_legacy",
		Kind:    "cpa",
		BaseUrl: srv.URL,
		MgmtKey: plaintextSecret, // un-migrated plaintext row
		Enabled: true,
	}
	rot := &RotateConfig{Cipher: testCipher(t)}

	if _, err := Sync(context.Background(), nil, node, rot); err != nil {
		t.Fatalf("Sync with legacy plaintext mgmt_key failed: %v", err)
	}
}
