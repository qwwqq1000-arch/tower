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

// TestTransientFailureDoesNotBan verifies that a transient upstream error (502),
// which is NOT a configured ban signal, fails over WITHOUT opening the breaker or
// emitting a ban event — only retry is recorded.
func TestTransientFailureDoesNotBan(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502) // bad gateway — transient, not a ban signal
		_, _ = w.Write([]byte(`{"error":"bad gateway"}`))
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

	base := policy.Defaults() // BanSignals=[401], BanPersistStreak=3
	base.BanPersistStreak = 1 // would open immediately IF 502 were treated as a ban
	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: base, Now: func() int64 { return 0 }}

	for i := 0; i < 3; i++ {
		_ = svc.Dispatch(ctx, owner, "opus", "please do a real task here", []byte(`{"model":"opus"}`))
	}

	// breaker must remain closed (502 is not a ban signal).
	if store.IsPermanent(nodeID + ":default") {
		t.Fatal("transient 502 must not cause a permanent ban")
	}
	if bs := store.BanStreak(nodeID + ":default"); bs != 0 {
		t.Fatalf("ban streak = %d, want 0 (502 is not a ban signal)", bs)
	}
	evs, _ := q.ListRecentEvents(ctx, 50)
	for _, e := range evs {
		if e.OwnerID == owner && (e.Type == "ban_detected" || e.Type == "ban_permanent") {
			t.Fatalf("transient 502 must not emit %s", e.Type)
		}
	}
}
