-- name: AddCostRollup :exec
INSERT INTO cost_rollup (scope_type, scope_id, day, model, requests, tokens_in, tokens_out, cost_usd)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (scope_type, scope_id, day, model) DO UPDATE SET
  requests   = cost_rollup.requests + EXCLUDED.requests,
  tokens_in  = cost_rollup.tokens_in + EXCLUDED.tokens_in,
  tokens_out = cost_rollup.tokens_out + EXCLUDED.tokens_out,
  cost_usd   = cost_rollup.cost_usd + EXCLUDED.cost_usd;

-- name: AppendLedger :one
INSERT INTO billing_ledger (tenant_id, ts, type, amount_usd, balance_after, ref, note)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: ListLedgerByTenant :many
SELECT * FROM billing_ledger WHERE tenant_id = $1 ORDER BY ts DESC, id DESC;

-- name: CreateSettlement :one
INSERT INTO settlements (id, tenant_id, period_start, period_end, gross_usd, settled_usd, status, created_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: SetHostingRate :exec
INSERT INTO hosting_rates (tenant_id, rate, effective_from)
VALUES ($1,$2,$3)
ON CONFLICT (tenant_id, effective_from) DO UPDATE SET rate = EXCLUDED.rate;

-- name: UpsertFallbackSpend :exec
INSERT INTO fallback_spend (channel_id, day, requests, est_cost_usd, balance_observed)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (channel_id, day) DO UPDATE SET
  requests = fallback_spend.requests + EXCLUDED.requests,
  est_cost_usd = fallback_spend.est_cost_usd + EXCLUDED.est_cost_usd,
  balance_observed = EXCLUDED.balance_observed;
