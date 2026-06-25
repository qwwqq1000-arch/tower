-- name: UpsertAccountSpendThreshold :exec
INSERT INTO account_spend_threshold (key, threshold, day, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (key) DO UPDATE SET
  threshold  = EXCLUDED.threshold,
  day        = EXCLUDED.day,
  updated_at = EXCLUDED.updated_at;

-- name: GetAccountSpendThreshold :one
SELECT key, threshold, day FROM account_spend_threshold WHERE key = $1;

-- name: ListAccountSpendThresholds :many
SELECT key, threshold, day FROM account_spend_threshold;
