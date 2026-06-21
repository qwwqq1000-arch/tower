-- name: ListAllDispatchKeys :many
SELECT * FROM dispatch_keys ORDER BY created_at DESC;
