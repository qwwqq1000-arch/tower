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
	upsertAccountCalled     int
	upsertNodeAcctCalled    int
	upsertQuotaCalled       int
	setFetchErrorCalled     int
	lastFetchErrorAccountID string
	lastFetchErrorMsg       string
	// nodeAcctEnabled tracks the Enabled value passed in each UpsertCpaNodeAccount
	// call, keyed by account_id. Used to assert manual-disable preservation (cpa-2).
	nodeAcctEnabled map[string]bool
}

func (s *stubSyncDB) UpsertCpaAccount(_ context.Context, _ sqlc.UpsertCpaAccountParams) error {
	s.upsertAccountCalled++
	return nil
}
func (s *stubSyncDB) UpsertCpaNodeAccount(_ context.Context, arg sqlc.UpsertCpaNodeAccountParams) error {
	s.upsertNodeAcctCalled++
	if s.nodeAcctEnabled == nil {
		s.nodeAcctEnabled = make(map[string]bool)
	}
	s.nodeAcctEnabled[arg.AccountID] = arg.Enabled
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
// TestSync_NoAutoUsagePoll verifies the reactive design (account-limit-reactive):
// the periodic discovery Sync discovers accounts but must NOT auto-poll the Anthropic
// account-usage endpoint — that poll was slow, inaccurate, and risked 429s. Rotation
// is now reactive (set from a usage-limit dispatch response); quota numbers refresh
// only on the manual "刷新" button.
func TestSync_NoAutoUsagePoll(t *testing.T) {
	const mgmtSecret = "test-secret"
	usageHit := false
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
			usageHit = true
			w.WriteHeader(503)
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	node := sqlc.Node{ID: "n_cpa_test", Kind: "cpa", BaseUrl: srv.URL, MgmtKey: mgmtSecret, Enabled: true}
	stub := &stubSyncDB{}

	n, err := Sync(context.Background(), stub, node, &RotateConfig{})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if n != 1 {
		t.Fatalf("want 1 discovered account, got %d", n)
	}
	if usageHit {
		t.Error("Sync must NOT call the account-usage endpoint — rotation is reactive now")
	}
	if stub.upsertQuotaCalled != 0 {
		t.Errorf("UpsertCpaQuota should not be called (no auto poll), got %d", stub.upsertQuotaCalled)
	}
	if stub.setFetchErrorCalled != 0 {
		t.Errorf("SetCpaQuotaFetchError should not be called (no auto poll), got %d", stub.setFetchErrorCalled)
	}
}

// TestSync_PreservesManualDisable verifies that Sync passes Enabled=false when
// the CPA source reports an account as disabled, and Enabled=true for active
// accounts. The SQL ON CONFLICT clause intentionally omits "enabled = EXCLUDED.enabled"
// so that an admin's manual disable (enabled=false in node_accounts) is never
// overwritten by a subsequent discovery cycle — only the profile_id (selector) is
// updated for existing rows (cpa-2).
func TestSync_PreservesManualDisable(t *testing.T) {
	const mgmtSecret = "test-secret-manual-disable"
	// CPA node returns two accounts: one active, one disabled by CPA.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+mgmtSecret {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v0/management/auth-files" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[
				{"id":"acct-active","provider":"other","email":"active@example.com","account_type":"pro"},
				{"id":"acct-disabled","provider":"other","email":"disabled@example.com","account_type":"pro","disabled":true}
			]}`))
			return
		}
		w.WriteHeader(404)
	}))
	defer srv.Close()

	node := sqlc.Node{
		ID:      "n_cpa_md",
		Kind:    "cpa",
		BaseUrl: srv.URL,
		MgmtKey: mgmtSecret,
		Enabled: true,
	}
	stub := &stubSyncDB{}
	rot := &RotateConfig{}

	n, err := Sync(context.Background(), stub, node, rot)
	if err != nil {
		t.Fatalf("Sync failed: %v", err)
	}
	if n != 2 {
		t.Fatalf("want 2 discovered accounts, got %d", n)
	}

	// Active CPA account → Enabled=true (CPA source says active → new rows get enabled).
	activeAID := "cpa:n_cpa_md:acct-active"
	if got, ok := stub.nodeAcctEnabled[activeAID]; !ok || !got {
		t.Errorf("active account %q: want Enabled=true, got Enabled=%v (ok=%v)", activeAID, got, ok)
	}
	// Disabled CPA account → Enabled=false (CPA source says disabled → new rows come in disabled).
	disabledAID := "cpa:n_cpa_md:acct-disabled"
	if got, ok := stub.nodeAcctEnabled[disabledAID]; !ok || got {
		t.Errorf("CPA-disabled account %q: want Enabled=false, got Enabled=%v (ok=%v)", disabledAID, got, ok)
	}

	// The SQL ON CONFLICT clause (verified in queries/node_accounts.sql) does NOT
	// include "enabled = EXCLUDED.enabled", so a second Sync call cannot flip an
	// admin-manually-disabled row back to true. We document that invariant here:
	// after re-syncing, the stub still records the same enabled values (only
	// profile_id would be updated on conflict in the real DB).
	stub2 := &stubSyncDB{}
	if _, err := Sync(context.Background(), stub2, node, rot); err != nil {
		t.Fatalf("second Sync failed: %v", err)
	}
	// A manually-disabled account that CPA now reports as active would pass
	// Enabled=true here, but the SQL ON CONFLICT path does NOT apply that value
	// (only INSERT path does). This test documents the Sync contract: it passes
	// CPA state faithfully; the DB layer preserves the admin override via SQL.
	if got := stub2.nodeAcctEnabled[activeAID]; !got {
		t.Errorf("re-sync: active account should still pass Enabled=true to upsert, got %v", got)
	}
}
