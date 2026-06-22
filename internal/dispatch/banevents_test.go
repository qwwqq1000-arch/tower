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

func TestSSEHasError(t *testing.T) {
	good := "event: message_start\ndata: {\"type\":\"message_start\"}\n\nevent: message_stop\ndata: {}\n"
	if sseHasError(good) {
		t.Error("clean stream must not be flagged as error")
	}
	bad1 := "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\"}}\n"
	bad2 := "data: {\"type\": \"error\", \"error\": {\"type\": \"api_error\"}}\n"
	if !sseHasError(bad1) || !sseHasError(bad2) {
		t.Error("error events must be detected (both spacing variants)")
	}
}

// TestCooldownSignalCoolsAccount verifies a CooldownSignal (429) puts the account
// into a temporary cooldown (not a ban) and emits a cooldown event.
func TestCooldownSignalCoolsAccount(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
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

	now := int64(1000)
	base := policy.Defaults() // BanSignals=[401]
	base.CooldownSignals = []int{429}
	base.CooldownSignalSec = 60
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: base, Now: func() int64 { return now }}

	_ = svc.Dispatch(ctx, owner, "opus", "please do a real task here", []byte(`{"model":"opus"}`))

	key := nodeID + ":default"
	if store.BanStreak(key) != 0 || store.IsPermanent(key) {
		t.Fatal("429 must not ban the account")
	}
	// account must be in cooldown now (within the 60s window)
	for _, snap := range store.Snapshot(now + 1000) {
		if snap.Key == key && snap.Status != "cooldown" {
			t.Fatalf("status=%s, want cooldown", snap.Status)
		}
	}
	// after the cooldown window it returns to active
	for _, snap := range store.Snapshot(now + 61_000) {
		if snap.Key == key && snap.Status != "active" {
			t.Fatalf("after cooldown status=%s, want active", snap.Status)
		}
	}
	evs, _ := q.ListRecentEvents(ctx, 50)
	seenCooldown := false
	for _, e := range evs {
		if e.OwnerID == owner && e.Type == "cooldown" {
			seenCooldown = true
		}
		if e.OwnerID == owner && (e.Type == "ban_detected" || e.Type == "ban_permanent") {
			t.Fatal("429 must not emit a ban event")
		}
	}
	if !seenCooldown {
		t.Error("expected a cooldown event")
	}
}

// TestBanEventAttributedToAccountOwner verifies that a ban_detected event is
// attributed to the banned account's owner (events-audit-3), not to the
// dispatch caller's owner. Scenario: admin dispatch key (ownerID="") dispatches
// to an account owned by tenant "tenant_X"; the ban event OwnerID must be
// "tenant_X", not "".
func TestBanEventAttributedToAccountOwner(t *testing.T) {
	q, closeDB := setupDB(t)
	defer closeDB()
	ctx := context.Background()

	// Node always returns 401 (ban signal).
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"error":"authentication_error"}`))
	}))
	defer node.Close()

	sfx := suffixDispatch(t)
	// The tenant that owns the account.
	acctOwner := "tenant_" + sfx
	// The dispatch caller is an admin key (ownerID = "") that can see all accounts.
	dispatchOwner := ""

	nodeID := "n_attr_" + sfx
	if _, err := q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n-attr-" + sfx, BaseUrl: node.URL, ApiKey: "k", OwnerID: acctOwner}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if _, err := q.CreateAccount(ctx, sqlc.CreateAccountParams{ID: "a_attr_" + sfx, OwnerID: acctOwner}); err != nil {
		t.Fatalf("create account: %v", err)
	}
	if _, err := q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeID, AccountID: "a_attr_" + sfx, ProfileID: "default", Weight: 100, Role: "baseline"}); err != nil {
		t.Fatalf("assign: %v", err)
	}

	base := policy.Defaults()
	base.BanPersistStreak = 1 // single 401 opens the breaker
	store := state.NewStore(func() int64 { return 0 }, func(min, max int64) int64 { return min })
	svc := &Service{Q: q, Store: store, Base: base, Now: func() int64 { return 0 }}

	// Dispatch as admin (ownerID="") so it sees the tenant-owned account.
	_ = svc.Dispatch(ctx, dispatchOwner, "opus", "please do a real task here", []byte(`{"model":"opus"}`))

	evs, err := q.ListRecentEvents(ctx, 100)
	if err != nil {
		t.Fatalf("list events: %v", err)
	}
	// The ban event must carry the account's owner, not the dispatch caller's "".
	for _, e := range evs {
		if e.Type == "ban_detected" || e.Type == "ban_permanent" {
			if e.OwnerID != acctOwner {
				t.Errorf("ban event OwnerID=%q, want %q (the banned account's owner)", e.OwnerID, acctOwner)
			}
		}
	}
	// Confirm at least one ban event was recorded.
	var seenBan bool
	for _, e := range evs {
		if (e.Type == "ban_detected" || e.Type == "ban_permanent") && e.OwnerID == acctOwner {
			seenBan = true
		}
	}
	if !seenBan {
		t.Error("expected a ban_detected or ban_permanent event attributed to the account's owner")
	}
}
