package billing

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// Settle sums an owner's accrued cost into a paid settlement plus a ledger entry,
// wrapped in a single pgx transaction for atomicity.
func Settle(ctx context.Context, pool *pgxpool.Pool, tenantID string, periodStart, periodEnd, now int64, settleID string) (sqlc.Settlement, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	defer tx.Rollback(ctx)
	q := sqlc.New(tx)
	// gross = all-time accrued consumption; alreadySettled = sum of prior paid
	// settlements. We settle only the outstanding delta so re-settling does not
	// re-charge the full lifetime total (the previous behaviour) and the tenant's
	// unsettled balance actually drops after a settle.
	gross, err := q.SumCostForOwner(ctx, tenantID)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	alreadySettled, err := q.SumSettledForOwner(ctx, tenantID)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	outstanding := OutstandingToSettle(gross, alreadySettled)
	st, err := q.CreateSettlement(ctx, sqlc.CreateSettlementParams{ID: settleID, TenantID: tenantID, PeriodStart: periodStart, PeriodEnd: periodEnd, GrossUsd: gross, SettledUsd: outstanding, Status: "paid", CreatedAt: now})
	if err != nil {
		return sqlc.Settlement{}, err
	}
	if _, err := q.AppendLedger(ctx, sqlc.AppendLedgerParams{TenantID: tenantID, Ts: now, Type: "settlement", AmountUsd: outstanding, BalanceAfter: 0, Ref: settleID, Note: "settlement"}); err != nil {
		return sqlc.Settlement{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return sqlc.Settlement{}, err
	}
	return st, nil
}
