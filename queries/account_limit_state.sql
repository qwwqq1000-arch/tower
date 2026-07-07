-- name: UpsertAccountLimitState :exec
INSERT INTO account_limit_state (key, limited_until, limit_reason, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE SET
  limited_until = EXCLUDED.limited_until,
  limit_reason  = EXCLUDED.limit_reason,
  updated_at    = EXCLUDED.updated_at;

-- name: DeleteAccountLimitState :exec
DELETE FROM account_limit_state WHERE key = $1;

-- name: ListActiveAccountLimitState :many
SELECT key, limited_until, limit_reason FROM account_limit_state WHERE limited_until > $1;
