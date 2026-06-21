-- name: InsertDispatchLog :exec
INSERT INTO dispatch_logs (ts, owner_id, model, target, profile_id, status, http_status, latency_ms, tokens_in, tokens_out, fallback_reason, ttfb_ms, stream, cost_usd)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14);

-- name: ListRecentDispatchLogs :many
SELECT * FROM dispatch_logs ORDER BY ts DESC LIMIT $1;
