-- name: CreateDispatchKey :one
INSERT INTO dispatch_keys (id, key_hash, salt, prefix, owner_id, label)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetDispatchKeysByPrefix :many
SELECT * FROM dispatch_keys WHERE prefix = $1 AND enabled = TRUE;

-- name: ListDispatchKeysByOwner :many
SELECT * FROM dispatch_keys WHERE owner_id = $1 ORDER BY created_at DESC;

-- name: DisableDispatchKey :exec
UPDATE dispatch_keys SET enabled = FALSE WHERE id = $1;
