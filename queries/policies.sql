-- name: UpsertPolicy :exec
INSERT INTO policies (scope_type, scope_id, params, updated_at)
VALUES ($1,$2,$3,$4)
ON CONFLICT (scope_type, scope_id) DO UPDATE SET params = EXCLUDED.params, updated_at = EXCLUDED.updated_at;

-- name: ListPolicies :many
SELECT * FROM policies;
