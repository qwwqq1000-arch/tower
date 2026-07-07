-- name: SetNodeEnabled :exec
UPDATE nodes SET enabled = $2 WHERE id = $1;

-- name: SetNodePassthrough :exec
UPDATE nodes SET passthrough = $2 WHERE id = $1;
