package state

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func TestPersist_SaveThenWarmStart(t *testing.T) {
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

	// build an account that is banned (open breaker)
	cfg := BreakerCfg{PersistStreak: 1, BaseMs: 100000, MaxMs: 100000, Mult: 2}
	a := NewAccount(2)
	a.Breaker.OnBanSignal(cfg, 1000) // openUntil = 101000
	key := "node-x:default"

	if err := SaveState(ctx, q, "node-x", "default", a, 1000); err != nil {
		t.Fatalf("save: %v", err)
	}

	// fresh store warm-starts from DB
	st := NewStore(fixedClock(2000), minRand)
	if err := LoadStates(ctx, q, st, 2); err != nil {
		t.Fatalf("load: %v", err)
	}
	got := st.Ensure(key, 2)
	if got.Breaker.State(2000) != "open" {
		t.Fatalf("warm-started breaker state=%s, want open (durable ban restored)", got.Breaker.State(2000))
	}
	openUntil, streak, fc := got.Breaker.Snapshot()
	if openUntil != 101000 || streak != 1 || fc != 1 {
		t.Fatalf("snapshot after warm-start = (%d,%d,%d), want (101000,1,1)", openUntil, streak, fc)
	}
}
