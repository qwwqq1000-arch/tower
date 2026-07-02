-- name: TodayDispatchByModel :many
SELECT model,
       count(*)::int AS requests,
       coalesce(sum(tokens_in),0)::bigint AS tokens_in,
       coalesce(sum(tokens_out),0)::bigint AS tokens_out,
       sum(CASE WHEN status='ok' THEN 1 ELSE 0 END)::int AS ok,
       coalesce(sum(cost_usd),0)::float8 AS cost
FROM dispatch_logs WHERE ts >= $1
GROUP BY model ORDER BY requests DESC;

-- name: ListTenantsBasic :many
SELECT id, username, role, channel_rate, fallback_limit FROM tenants ORDER BY created_at;

-- name: GetHostingRate :one
SELECT rate FROM hosting_rates WHERE tenant_id = $1 ORDER BY effective_from DESC LIMIT 1;

-- name: SumAllCost :one
SELECT coalesce(sum(cost_usd),0)::float8 AS total FROM dispatch_logs WHERE status='ok';

-- name: CostByTargetSince :many
SELECT target, coalesce(sum(cost_usd),0)::float8 AS cost, count(*)::bigint AS requests
FROM dispatch_logs WHERE ts >= $1 AND status='ok' GROUP BY target;

-- name: SumAllCostFromLogs :one
SELECT coalesce(sum(cost_usd),0)::float8 AS total FROM dispatch_logs WHERE status='ok' AND owner_id = $1;

-- name: CostByTargetTotal :many
SELECT target, coalesce(sum(cost_usd),0)::float8 AS cost, count(*)::bigint AS requests
FROM dispatch_logs WHERE status='ok' GROUP BY target;
