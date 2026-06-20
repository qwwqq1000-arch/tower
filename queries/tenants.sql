-- name: CreateTenant :one
INSERT INTO tenants (id, username, pw_hash, salt, role, ingest_key, must_change_pw)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING *;

-- name: GetTenantByUsername :one
SELECT * FROM tenants WHERE username = $1;

-- name: GetTenantByID :one
SELECT * FROM tenants WHERE id = $1;
