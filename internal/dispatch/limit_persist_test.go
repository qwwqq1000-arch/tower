package dispatch

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// TestRestoreLimitState verifies the startup restore pattern: rows returned by
// ListActiveAccountLimitState are fed into store.SetLimitedReason, so a still-
// limited account shows as limited after restart.
func TestRestoreLimitState_InMemory(t *testing.T) {
	now := int64(1_000_000_000)
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	cap := 3
	key := "node:acct"
	untilMs := now + 3_600_000 // 1h from now

	// Simulate what the restore loop in main.go does:
	rows := []sqlc.ListActiveAccountLimitStateRow{
		{Key: key, LimitedUntil: untilMs, LimitReason: "5h"},
	}
	for _, r := range rows {
		store.SetLimitedReason(r.Key, cap, r.LimitedUntil, r.LimitReason)
	}

	// The account should now be limited.
	if !store.IsLimited(key, now) {
		t.Fatal("restored account should be limited")
	}

	// After the limit expires it is no longer limited.
	if store.IsLimited(key, untilMs+1) {
		t.Fatal("limit should have expired")
	}
}

// TestPersistLimitState_DB is an integration test that exercises the full
// round-trip via the real DB: write with UpsertAccountLimitState, read back
// with ListActiveAccountLimitState, delete with DeleteAccountLimitState.
// Requires TEST_DATABASE_URL; skipped otherwise.
func TestPersistLimitState_DB(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	if err := db.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	q := sqlc.New(pool)

	now := int64(1_000_000)
	key := "test-limit-node:test-acct"
	untilMs := now + 3_600_000
	reason := "5h"

	// Upsert a row.
	if err := q.UpsertAccountLimitState(ctx, sqlc.UpsertAccountLimitStateParams{
		Key: key, LimitedUntil: untilMs, LimitReason: reason, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	defer func() { _ = q.DeleteAccountLimitState(ctx, key) }()

	// ListActive at now should return the row.
	rows, err := q.ListActiveAccountLimitState(ctx, now)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Key == key && r.LimitedUntil == untilMs && r.LimitReason == reason {
			found = true
		}
	}
	if !found {
		t.Fatalf("row not found in ListActive; rows=%v", rows)
	}

	// ListActive at untilMs+1 should NOT return the row (expired).
	rows2, err := q.ListActiveAccountLimitState(ctx, untilMs+1)
	if err != nil {
		t.Fatalf("list expired: %v", err)
	}
	for _, r := range rows2 {
		if r.Key == key {
			t.Fatal("expired row should not appear in ListActive")
		}
	}

	// Delete and verify gone.
	if err := q.DeleteAccountLimitState(ctx, key); err != nil {
		t.Fatalf("delete: %v", err)
	}
	rows3, err := q.ListActiveAccountLimitState(ctx, now)
	if err != nil {
		t.Fatalf("list after delete: %v", err)
	}
	for _, r := range rows3 {
		if r.Key == key {
			t.Fatal("deleted row should not appear")
		}
	}

	// Restore into store verifies the loop.
	store := state.NewStore(func() int64 { return now }, func(min, max int64) int64 { return min })
	base := policy.Defaults()
	for _, r := range rows { // rows from the first list, before delete
		store.SetLimitedReason(r.Key, base.MaxConcurrent, r.LimitedUntil, r.LimitReason)
	}
	if !store.IsLimited(key, now) {
		t.Fatal("restored account should be limited")
	}
}
