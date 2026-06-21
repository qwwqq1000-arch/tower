-- name: AssignAccount :one
INSERT INTO node_accounts (node_id, account_id, profile_id, egress, weight, role, slot_id)
VALUES ($1,$2,$3,$4,$5,$6,$7)
RETURNING *;

-- name: ListNodeAccountsByNode :many
SELECT * FROM node_accounts WHERE node_id = $1;

-- name: ListNodeAccountsByAccount :many
SELECT * FROM node_accounts WHERE account_id = $1;

-- name: SetNodeAccountEnabled :exec
UPDATE node_accounts SET enabled = $3 WHERE node_id = $1 AND account_id = $2;

-- name: SetNodeAccountEnabledByAccount :exec
UPDATE node_accounts SET enabled = $2 WHERE account_id = $1;

-- name: UnassignAccount :exec
DELETE FROM node_accounts WHERE node_id = $1 AND account_id = $2;

-- name: UpdateNodeAccount :exec
UPDATE node_accounts SET egress=$3, weight=$4, role=$5, enabled=$6, slot_id=$7
WHERE node_id=$1 AND account_id=$2;
