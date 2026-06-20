package sqlc

import (
	"context"
	"os"
	"testing"

	"github.com/qwwqq1000-arch/tower/internal/db"
)

func TestBillingTables(t *testing.T) {
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
	scope := "tenant_" + sfx

	// cost rollup accumulates on conflict
	for i := 0; i < 2; i++ {
		if err := q.AddCostRollup(ctx, AddCostRollupParams{
			ScopeType: "tenant", ScopeID: scope, Day: "2026-06-21", Model: "opus",
			Requests: 1, TokensIn: 100, TokensOut: 50, CostUsd: 0.01,
		}); err != nil {
			t.Fatalf("rollup: %v", err)
		}
	}
	var reqs int64
	var cost float64
	if err := pool.QueryRow(ctx, `SELECT requests, cost_usd FROM cost_rollup WHERE scope_id=$1`, scope).Scan(&reqs, &cost); err != nil {
		t.Fatalf("read rollup: %v", err)
	}
	if reqs != 2 {
		t.Fatalf("rollup requests=%d, want 2 (accumulated)", reqs)
	}

	// ledger append + list
	if _, err := q.AppendLedger(ctx, AppendLedgerParams{TenantID: scope, Ts: 1, Type: "usage", AmountUsd: 0.02, BalanceAfter: 0.02}); err != nil {
		t.Fatalf("ledger: %v", err)
	}
	rows, err := q.ListLedgerByTenant(ctx, scope)
	if err != nil || len(rows) != 1 {
		t.Fatalf("list ledger: %v n=%d", err, len(rows))
	}

	// settlement
	if _, err := q.CreateSettlement(ctx, CreateSettlementParams{
		ID: "s_" + sfx, TenantID: scope, PeriodStart: 0, PeriodEnd: 100, GrossUsd: 0.02, SettledUsd: 0.02, Status: "paid", CreatedAt: 1,
	}); err != nil {
		t.Fatalf("settlement: %v", err)
	}

	// hosting rate upsert
	if err := q.SetHostingRate(ctx, SetHostingRateParams{TenantID: scope, Rate: 1.5, EffectiveFrom: 0}); err != nil {
		t.Fatalf("rate: %v", err)
	}
}
