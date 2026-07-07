package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

func sfx(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestPollOnce_OfflineWhenNodeDown(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	_ = db.Migrate(ctx, url)
	pool, err := db.Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := sqlc.New(pool)

	s := sfx(t)
	nodeID := "n_" + s
	// unreachable base URL
	_, _ = q.CreateNode(ctx, sqlc.CreateNodeParams{ID: nodeID, Name: "n", BaseUrl: "http://127.0.0.1:1", ApiKey: "k", OwnerID: "o_" + s})
	_, _ = q.AssignAccount(ctx, sqlc.AssignAccountParams{NodeID: nodeID, AccountID: "a_" + s, ProfileID: "default", Weight: 100, Role: "baseline"})

	store := state.NewStore(func() int64 { return 1000 }, func(a, b int64) int64 { return a })
	p := &Poller{Q: q, Store: store, DefaultTTLMs: 3600000, Capacity: 2, Now: func() int64 { return 1000 }}
	_ = p.PollOnce(ctx)

	cfg := state.BreakerCfg{PersistStreak: 3, BaseMs: 1, MaxMs: 1, Mult: 2}
	if store.TryDispatch(nodeID+":default", "opus", cfg) {
		t.Fatal("unreachable node → offline → no dispatch")
	}
}
