-- name: CreateFallbackChannel :one
INSERT INTO fallback_channels (id, owner_id, group_id, name, base_url, api_key, priority, weight, max_concurrent, cooldown_ms, price_threshold, model_allowlist)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: ListFallbackChannelsByOwner :many
SELECT * FROM fallback_channels WHERE owner_id = $1 ORDER BY priority, created_at;

-- name: ListEnabledFallbackChannels :many
SELECT * FROM fallback_channels WHERE enabled = TRUE ORDER BY priority, created_at;

-- name: ListAllFallbackChannels :many
SELECT * FROM fallback_channels ORDER BY priority, created_at;

-- name: UpdateFallbackChannel :exec
UPDATE fallback_channels SET name=$2, base_url=$3, api_key=$4, priority=$5, weight=$6,
  max_concurrent=$7, cooldown_ms=$8, price_threshold=$9, model_allowlist=$10
WHERE id=$1;

-- name: SetFallbackChannelEnabled :exec
UPDATE fallback_channels SET enabled=$2 WHERE id=$1;

-- name: DeleteFallbackChannel :exec
DELETE FROM fallback_channels WHERE id=$1;

-- name: GetFallbackSpendToday :one
SELECT coalesce(sum(requests),0)::bigint AS requests, coalesce(sum(est_cost_usd),0)::float8 AS cost
FROM fallback_spend WHERE channel_id=$1 AND day=$2;

-- name: GetFallbackSpendTotal :one
SELECT coalesce(sum(requests),0)::bigint AS requests, coalesce(sum(est_cost_usd),0)::float8 AS cost
FROM fallback_spend WHERE channel_id=$1;
