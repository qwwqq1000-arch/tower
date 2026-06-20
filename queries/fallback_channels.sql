-- name: CreateFallbackChannel :one
INSERT INTO fallback_channels (id, owner_id, group_id, name, base_url, api_key, priority, weight, max_concurrent, cooldown_ms, price_threshold, model_allowlist)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)
RETURNING *;

-- name: ListFallbackChannelsByOwner :many
SELECT * FROM fallback_channels WHERE owner_id = $1 ORDER BY priority, created_at;

-- name: ListEnabledFallbackChannels :many
SELECT * FROM fallback_channels WHERE enabled = TRUE ORDER BY priority, created_at;
