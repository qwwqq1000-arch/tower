package sqlc

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestDispatchTables(t *testing.T) {
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
	q := New(pool)
	sfx := suffixC(t)

	// policy upsert idempotent
	if err := q.UpsertPolicy(ctx, UpsertPolicyParams{ScopeType: "global", ScopeID: "_" + sfx, Params: []byte(`{"MaxConcurrent":5}`), UpdatedAt: 1}); err != nil {
		t.Fatalf("upsert policy: %v", err)
	}
	if err := q.UpsertPolicy(ctx, UpsertPolicyParams{ScopeType: "global", ScopeID: "_" + sfx, Params: []byte(`{"MaxConcurrent":7}`), UpdatedAt: 2}); err != nil {
		t.Fatalf("re-upsert policy: %v", err)
	}

	// fallback channel
	_, err = q.CreateFallbackChannel(ctx, CreateFallbackChannelParams{
		ID: "fc_" + sfx, OwnerID: "o1", Name: "relay", BaseUrl: "http://relay:8080", Priority: 100, Weight: 100,
	})
	if err != nil {
		t.Fatalf("create channel: %v", err)
	}
	chs, err := q.ListEnabledFallbackChannels(ctx)
	if err != nil || len(chs) < 1 {
		t.Fatalf("list channels: %v n=%d", err, len(chs))
	}

	// dispatch log
	if err := q.InsertDispatchLog(ctx, InsertDispatchLogParams{
		Ts: 100, OwnerID: "o1", Model: "opus", Target: "node1", Status: "ok", HttpStatus: 200, FallbackReason: "",
	}); err != nil {
		t.Fatalf("insert log: %v", err)
	}
	logs, err := q.ListRecentDispatchLogs(ctx, 10)
	if err != nil || len(logs) < 1 {
		t.Fatalf("list logs: %v n=%d", err, len(logs))
	}
}
