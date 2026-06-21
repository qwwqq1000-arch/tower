-- name: SetNodeEnabled :exec
UPDATE nodes SET enabled = $2 WHERE id = $1;
