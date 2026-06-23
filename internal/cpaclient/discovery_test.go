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

// stubSyncDB is a test stub that satisfies syncQuerier and records calls.
type stubSyncDB struct {
	upsertAccountCalled    int
	upsertNodeAcctCalled   int
	upsertQuotaCalled      int
	setFetchErrorCalled    int
	lastFetchErrorAccountID string
	lastFetchErrorMsg       string
}

func (s *stubSyncDB) UpsertCpaAccount(_ context.Context, _ sqlc.UpsertCpaAccountParams) error {
	s.upsertAccountCalled++
	return nil
}
func (s *stubSyncDB) UpsertCpaNodeAccount(_ context.Context, _ sqlc.UpsertCpaNodeAccountParams) error {
	s.upsertNodeAcctCalled++
	return nil
}
func (s *stubSyncDB) UpsertCpaQuota(_ context.Context, _ sqlc.UpsertCpaQuotaParams) error {
	s.upsertQuotaCalled++
	return nil
}
func (s *stubSyncDB) SetCpaQuotaFetchError(_ context.Context, arg sqlc.SetCpaQuotaFetchErrorParams) error {
	s.setFetchErrorCalled++
	s.lastFetchErrorAccountID = arg.AccountID
	s.lastFetchErrorMsg = arg.QuotaFetchError
	return nil
}

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

// TestSync_QuotaFetchError verifies that when the CPA usage endpoint returns an
// error for a claude/anthropic account, Sync records the error in the DB via
// SetCpaQuotaFetchError (cpa-3) instead of silently ignoring it. This ensures
// the UI can show "quota unavailable" rather than null/stale data.
func TestSync_QuotaFetchError(t *testing.T) {
	const mgmtSecret = "test-secret"
	// Build a test server: accounts endpoint returns one claude account;
	// usage endpoint fails with a non-2xx to simulate a quota fetch error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+mgmtSecret {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v0/management/auth-files" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"id":"acct-1","provider":"claude","email":"test@example.com","account_type":"pro"}]}`))
			return
		}
		if r.URL.Path == "/v0/management/account-usage" {
			// Simulate a temporary upstream error so the quota fetch fails.
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`service unavailable`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	node := sqlc.Node{
		ID:      "n_cpa_test",
		Kind:    "cpa",
		BaseUrl: srv.URL,
		MgmtKey: mgmtSecret,
		Enabled: true,
	}
	stub := &stubSyncDB{}
	rot := &RotateConfig{}

	n, err := Sync(context.Background(), stub, node, rot)
	if err != nil {
		t.Fatalf("Sync should not fail on quota fetch error: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 discovered account, got %d", n)
	}
	// UpsertCpaQuota must NOT have been called (usage fetch failed).
	if stub.upsertQuotaCalled != 0 {
		t.Errorf("UpsertCpaQuota should not be called when usage fetch fails, got %d calls", stub.upsertQuotaCalled)
	}
	// SetCpaQuotaFetchError MUST have been called with a non-empty error message.
	if stub.setFetchErrorCalled != 1 {
		t.Errorf("SetCpaQuotaFetchError should be called once, got %d calls", stub.setFetchErrorCalled)
	}
	if stub.lastFetchErrorMsg == "" {
		t.Error("quota fetch error message must not be empty")
	}
	expectedAID := "cpa:n_cpa_test:acct-1"
	if stub.lastFetchErrorAccountID != expectedAID {
		t.Errorf("fetch error account id: got %q want %q", stub.lastFetchErrorAccountID, expectedAID)
	}
}
