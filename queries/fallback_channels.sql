-- name: CreateFallbackChannel :one
INSERT INTO fallback_channels (id, owner_id, group_id, name, base_url, api_key, priority, max_concurrent, cooldown_ms, price_threshold, model_allowlist, balance_token, balance_user_id, balance_alert_usd)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
RETURNING *;

-- name: ListFallbackChannelsByOwner :many
SELECT * FROM fallback_channels WHERE owner_id = $1 ORDER BY priority, created_at;

-- name: ListEnabledFallbackChannels :many
SELECT * FROM fallback_channels WHERE enabled = TRUE ORDER BY priority, created_at;

-- name: ListAllFallbackChannels :many
SELECT * FROM fallback_channels ORDER BY priority, created_at;

-- name: UpdateFallbackChannel :exec
UPDATE fallback_channels SET name=$2, base_url=$3, api_key=$4, priority=$5,
  max_concurrent=$6, cooldown_ms=$7, price_threshold=$8, model_allowlist=$9,
  balance_token=$10, balance_user_id=$11, balance_alert_usd=$12
WHERE id=$1;

-- name: SetFallbackBalance :exec
UPDATE fallback_channels SET balance_usd=$2, balance_checked_at=$3, balance_error=$4 WHERE id=$1;

-- name: SetFallbackChannelEnabled :exec
UPDATE fallback_channels SET enabled=$2 WHERE id=$1;

-- name: GetFallbackChannel :one
SELECT * FROM fallback_channels WHERE id=$1;

-- name: DeleteFallbackChannel :exec
DELETE FROM fallback_channels WHERE id=$1;

-- name: GetFallbackSpendToday :one
SELECT coalesce(sum(requests),0)::bigint AS requests, coalesce(sum(est_cost_usd),0)::float8 AS cost
FROM fallback_spend WHERE channel_id=$1 AND day=$2;

-- name: GetFallbackSpendTotal :one
SELECT coalesce(sum(requests),0)::bigint AS requests, coalesce(sum(est_cost_usd),0)::float8 AS cost
FROM fallback_spend WHERE channel_id=$1;
