-- name: InsertDispatchLog :exec
INSERT INTO dispatch_logs (ts, owner_id, model, target, profile_id, status, http_status, latency_ms, tokens_in, tokens_out, fallback_reason, ttfb_ms, stream, cost_usd)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14);

-- name: ListRecentDispatchLogs :many
SELECT * FROM dispatch_logs ORDER BY ts DESC LIMIT $1;

-- name: ListLogsByOwner :many
SELECT * FROM dispatch_logs WHERE owner_id = $1 ORDER BY ts DESC LIMIT $2;

-- name: TodayDispatchForOwner :one
SELECT count(*)::bigint AS requests, coalesce(sum(cost_usd),0)::float8 AS cost
FROM dispatch_logs WHERE owner_id = $1 AND ts >= $2;

-- name: CountDispatchLogs :one
SELECT count(*) FROM dispatch_logs;

-- name: CountDispatchLogsSince :one
SELECT count(*) FROM dispatch_logs WHERE ts >= $1;

-- name: CountDispatchLogsByStatus :one
SELECT count(*) FROM dispatch_logs WHERE status = $1;

-- name: CountDispatchLogsByOwner :one
SELECT count(*) FROM dispatch_logs WHERE owner_id = $1;

-- name: CountDispatchLogsByOwnerSince :one
SELECT count(*) FROM dispatch_logs WHERE owner_id = $1 AND ts >= $2;
