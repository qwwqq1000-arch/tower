-- name: UpdateNodeVersion :exec
UPDATE nodes SET version = $2 WHERE id = $1;
