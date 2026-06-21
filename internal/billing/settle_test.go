package billing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func sfxB(t *testing.T) string {
	t.Helper()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func TestSettle(t *testing.T) {
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
	s := sfxB(t)
	tenant := "t_" + s

	// seed cost_rollup
	_ = q.AddCostRollup(ctx, sqlc.AddCostRollupParams{ScopeType: "owner", ScopeID: tenant, Day: "2026-06-21", Model: "opus", Requests: 1, CostUsd: 1.25})
	_ = q.AddCostRollup(ctx, sqlc.AddCostRollupParams{ScopeType: "owner", ScopeID: tenant, Day: "2026-06-22", Model: "sonnet", Requests: 1, CostUsd: 0.75})

	st, err := Settle(ctx, pool, tenant, 0, 100, 1, "s_"+s)
	if err != nil {
		t.Fatalf("settle: %v", err)
	}
	if st.GrossUsd < 1.999 || st.GrossUsd > 2.001 {
		t.Fatalf("gross=%v, want ~2.0", st.GrossUsd)
	}
	if st.Status != "paid" {
		t.Fatalf("status=%s", st.Status)
	}
	// ledger has a settlement entry
	rows, _ := q.ListLedgerByTenant(ctx, tenant)
	if len(rows) != 1 || rows[0].Type != "settlement" {
		t.Fatalf("ledger=%+v", rows)
	}
}
