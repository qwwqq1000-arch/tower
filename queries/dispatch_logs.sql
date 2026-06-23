-- name: InsertDispatchLog :exec
INSERT INTO dispatch_logs (ts, owner_id, model, target, profile_id, status, http_status, latency_ms, tokens_in, tokens_out, fallback_reason, ttfb_ms, stream, cost_usd, request_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15);

-- name: UpsertDispatchLogDetail :exec
-- Writes the per-request body+headers once; later log rows of the same request
-- (same request_id) are no-ops so we keep exactly one detail per request.
INSERT INTO dispatch_log_details (request_id, owner_id, ts, req_body, req_headers)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (request_id) DO NOTHING;

-- name: GetDispatchLogDetail :one
SELECT request_id, owner_id, ts, req_body, req_headers FROM dispatch_log_details WHERE request_id = $1;

-- name: DeleteDispatchLogDetailBefore :exec
DELETE FROM dispatch_log_details WHERE ts < $1;

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
