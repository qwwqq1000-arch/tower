-- name: CreateAccount :one
INSERT INTO accounts (id, owner_id, email, subscription_type, oauth_access_enc, oauth_refresh_enc, expires_at, onboarded_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = $1;

-- name: ListAccountsByOwner :many
SELECT * FROM accounts WHERE owner_id = $1 ORDER BY created_at DESC;

-- name: UpdateAccountCreds :exec
UPDATE accounts SET oauth_access_enc=$2, oauth_refresh_enc=$3, expires_at=$4 WHERE id=$1;

-- name: SetAccountStatus :exec
UPDATE accounts SET status=$2, banned_at=$3 WHERE id=$1;

-- name: SetAccountExpiry :exec
UPDATE accounts SET expires_at=$2 WHERE id=$1;
