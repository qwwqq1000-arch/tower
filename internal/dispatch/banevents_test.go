package dispatch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// TestBanControlEvents verifies that a ban signal (401) from a node produces a
// ban_detected event and that failover produces a retry event.
func TestBanControlEvents(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	// Node always returns 401 (a ban signal under the default policy).
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"authentication_error"}`))
	}))
	defer node.Close()

	sfx := suffixDispatch(t)
	owner := "owner_" + sfx
	nodeID := "n_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n-" + sfx, BaseUrl: node.URL, ApiKey: "k", OwnerID: owner}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{ID: "a_" + sfx, OwnerID: owner}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeID, AccountID: "a_" + sfx, ProfileID: "default", Weight: 100, Role: "baseline"}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	// PersistStreak=1 so a single 401 opens the breaker (fires ban_detected).
	base := policy.Defaults()
	base.BanPersistStreak = 1
	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: base, Now: func() int64 { return 0 }}

	_ = svc.Dispatch(ctx, owner, "opus", "please do a real task here", []byte(`{"model":"opus"}`))

	evs, err := q.ListRecentEvents(ctx, 50)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	seen := map[string]bool{}
	for _, e := range evs {
		if e.OwnerID == owner {
			seen[e.Type] = true
		}
	}
	if !seen["ban_detected"] {
		t.Errorf("expected a ban_detected event, got types %v", seen)
	}
	if !seen["retry"] {
		t.Errorf("expected a retry event, got types %v", seen)
	}
}
