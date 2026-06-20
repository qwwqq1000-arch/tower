-- name: CreateNode :one
INSERT INTO nodes (id, name, base_url, api_key, mgmt_key, owner_id, group_id, region, short_id, version, fingerprint_seed)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)
RETURNING *;

-- name: GetNode :one
SELECT * FROM nodes WHERE id = $1;

-- name: ListNodes :many
SELECT * FROM nodes ORDER BY created_at DESC;

-- name: ListNodesByOwner :many
SELECT * FROM nodes WHERE owner_id = $1 ORDER BY created_at DESC;

-- name: UpdateNodeEnabled :exec
UPDATE nodes SET enabled = $2 WHERE id = $1;

-- name: DeleteNode :exec
DELETE FROM nodes WHERE id = $1;
