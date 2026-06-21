-- name: CreateProvisionJob :one
INSERT INTO provision_jobs (id, host, name, owner_id, created_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$5)
RETURNING *;

-- name: GetProvisionJob :one
SELECT * FROM provision_jobs WHERE id = $1;

-- name: AppendProvisionLog :exec
UPDATE provision_jobs SET log = log || sqlc.arg(chunk), updated_at = sqlc.arg(updated_at) WHERE id = sqlc.arg(id);

-- name: SetProvisionStatus :exec
UPDATE provision_jobs SET status = $2, step = $3, updated_at = $4 WHERE id = $1;
