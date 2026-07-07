package cpaclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// stubOwnerDB extends stubSyncDB to also capture the OwnerID from UpsertCpaAccount.
type stubOwnerDB struct {
	stubSyncDB
	capturedOwnerIDs []string
}

func (s *stubOwnerDB) UpsertCpaAccount(_ context.Context, arg sqlc.UpsertCpaAccountParams) error {
	s.upsertAccountCalled++
	s.capturedOwnerIDs = append(s.capturedOwnerIDs, arg.OwnerID)
	return nil
}

// newOwnerTestServer returns an httptest server serving one account on /v0/management/auth-files.
func newOwnerTestServer(t *testing.T, secret string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			w.WriteHeader(401)
			return
		}
		if r.URL.Path == "/v0/management/auth-files" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"files":[{"id":"acct-1","provider":"other","email":"test@example.com","account_type":"pro"}]}`))
			return
		}
		w.WriteHeader(404)
	}))
	return srv
}

// TestSyncAccountOwner verifies the account owner resolution logic:
//   - When node.AccountOwnerID is non-empty, CPA accounts are assigned that owner.
//   - When node.AccountOwnerID is empty, accounts fall back to node.OwnerID.
func TestSyncAccountOwner(t *testing.T) {
	const mgmtSecret = "owner-test-secret"

	t.Run("AccountOwnerID_set", func(t *testing.T) {
		srv := newOwnerTestServer(t, mgmtSecret)
		defer srv.Close()

		node := sqlc.Node{
			ID:             "n_cpa_owner",
			Kind:           "cpa",
			BaseUrl:        srv.URL,
			MgmtKey:        mgmtSecret,
			Enabled:        true,
			OwnerID:        "u_node",
			AccountOwnerID: "u_acct",
		}
		stub := &stubOwnerDB{}

		n, err := Sync(context.Background(), stub, node, &RotateConfig{})
		if err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if n != 1 {
			t.Fatalf("want 1 discovered account, got %d", n)
		}
		if len(stub.capturedOwnerIDs) != 1 {
			t.Fatalf("want 1 UpsertCpaAccount call, got %d", len(stub.capturedOwnerIDs))
		}
		if got := stub.capturedOwnerIDs[0]; got != "u_acct" {
			t.Errorf("account owner: want %q (AccountOwnerID), got %q", "u_acct", got)
		}
	})

	t.Run("AccountOwnerID_empty_falls_back_to_OwnerID", func(t *testing.T) {
		srv := newOwnerTestServer(t, mgmtSecret)
		defer srv.Close()

		node := sqlc.Node{
			ID:             "n_cpa_fallback",
			Kind:           "cpa",
			BaseUrl:        srv.URL,
			MgmtKey:        mgmtSecret,
			Enabled:        true,
			OwnerID:        "u_node",
			AccountOwnerID: "", // empty → should fall back to OwnerID
		}
		stub := &stubOwnerDB{}

		n, err := Sync(context.Background(), stub, node, &RotateConfig{})
		if err != nil {
			t.Fatalf("Sync: %v", err)
		}
		if n != 1 {
			t.Fatalf("want 1 discovered account, got %d", n)
		}
		if len(stub.capturedOwnerIDs) != 1 {
			t.Fatalf("want 1 UpsertCpaAccount call, got %d", len(stub.capturedOwnerIDs))
		}
		if got := stub.capturedOwnerIDs[0]; got != "u_node" {
			t.Errorf("account owner: want %q (OwnerID fallback), got %q", "u_node", got)
		}
	})
}
