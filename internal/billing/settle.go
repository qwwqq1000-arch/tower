package billing

import (
	"context"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// Settle sums an owner's accrued cost into a paid settlement plus a ledger entry.
func Settle(ctx context.Context, q *sqlc.Queries, tenantID string, periodStart, periodEnd, now int64, settleID string) (sqlc.Settlement, error) {
	gross, err := q.SumCostForOwner(ctx, tenantID)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	st, err := q.CreateSettlement(ctx, sqlc.CreateSettlementParams{
		ID: settleID, TenantID: tenantID, PeriodStart: periodStart, PeriodEnd: periodEnd,
		GrossUsd: gross, SettledUsd: gross, Status: "paid", CreatedAt: now,
	})
	if err != nil {
		return sqlc.Settlement{}, err
	}
	_, err = q.AppendLedger(ctx, sqlc.AppendLedgerParams{
		TenantID: tenantID, Ts: now, Type: "settlement", AmountUsd: gross, BalanceAfter: 0, Ref: settleID, Note: "settlement",
	})
	if err != nil {
		return sqlc.Settlement{}, err
	}
	return st, nil
}
