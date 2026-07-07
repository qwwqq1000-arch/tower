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
	// Serialize concurrent settle calls for the same tenant via a Postgres
	// advisory lock scoped to this transaction. hashtext() maps the tenantID
	// string to an int4 key; pg_advisory_xact_lock blocks until it can acquire
	// and releases automatically when the transaction ends.
	if _, err := tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", tenantID); err != nil {
		return sqlc.Settlement{}, err
	}
	// gross = all-time accrued consumption; alreadySettled = sum of prior paid
	// settlements. We settle only the outstanding delta so re-settling does not
	// re-charge the full lifetime total (the previous behaviour) and the tenant's
	// unsettled balance actually drops after a settle.
	// We settle the FEE (托管费 + 渠道托管费), not raw consumption (billing-fee-1):
	//   gross = node consumption × hostingRate + channel relay consumption × channelRate
	// SettledUsd / the ledger record the fee owed, and alreadySettled is the sum of
	// prior settled FEE — so the unsettled balance is the outstanding fee.
	consumption, err := q.SumCostForOwner(ctx, tenantID)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	rate, err := q.GetHostingRate(ctx, tenantID)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	channelConsumption, err := q.SumFallbackSpendByOwner(ctx, tenantID)
	if err != nil {
		return sqlc.Settlement{}, err
	}
	channelRate := 0.0
	if t, terr := q.GetTenantByID(ctx, tenantID); terr == nil {
		channelRate = t.ChannelRate
	}
	gross := TotalHostingFee(consumption, rate, channelConsumption, channelRate)
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
