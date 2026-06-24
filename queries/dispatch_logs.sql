-- name: InsertDispatchLog :exec
INSERT INTO dispatch_logs (ts, owner_id, model, target, profile_id, status, http_status, latency_ms, tokens_in, tokens_out, fallback_reason, ttfb_ms, stream, cost_usd, request_id, cache_read, cache_creation)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17);

-- name: UpsertDispatchLogDetail :exec
-- Writes the per-request body+headers once; later log rows of the same request
-- (same request_id) are no-ops so we keep exactly one detail per request.
INSERT INTO dispatch_log_details (request_id, owner_id, ts, req_body, req_headers)
VALUES ($1,$2,$3,$4,$5)
ON CONFLICT (request_id) DO NOTHING;

-- name: GetDispatchLogDetail :one
SELECT request_id, owner_id, ts, req_body, req_headers, resp_status, resp_body FROM dispatch_log_details WHERE request_id = $1;

-- name: UpdateDispatchLogDetailResponse :exec
-- Appends a response segment and sets the latest status for a request (logs-detail-2).
-- Appending (not overwriting) lets a failed-over request keep every attempt's error —
-- e.g. a node 429 followed by a fallback 200 — instead of only the final outcome
-- (logs-detail-3). Capped to 64KB. No-op if the detail row was already pruned.
UPDATE dispatch_log_details SET resp_status = $2, resp_body = left(coalesce(resp_body, '') || $3, 65536) WHERE request_id = $1;

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
