-- name: InsertInterceptedSecret :exec
INSERT INTO intercepted_secrets (request_id, owner_id, account_key, model, secret_type, secret_value, context_line, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: ListInterceptedSecrets :many
SELECT id, request_id, owner_id, account_key, model, secret_type, secret_value, context_line, created_at
FROM intercepted_secrets
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountInterceptedSecrets :one
SELECT count(*) FROM intercepted_secrets;

-- name: GetInterceptedSecret :one
SELECT id, request_id, owner_id, account_key, model, secret_type, secret_value, context_line, created_at
FROM intercepted_secrets
WHERE id = $1;

-- name: DeleteInterceptedSecret :exec
DELETE FROM intercepted_secrets WHERE id = $1;

-- name: ListRecentInterceptedValues :many
SELECT DISTINCT secret_value FROM intercepted_secrets
WHERE owner_id = $1 AND created_at > $2;
