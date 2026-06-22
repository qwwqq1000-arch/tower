-- name: UpsertAccountState :exec
INSERT INTO account_state (node_id, profile_id, status, cooldown_until, ban_streak, fail_count, permanent, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (node_id, profile_id) DO UPDATE SET
  status = EXCLUDED.status,
  cooldown_until = EXCLUDED.cooldown_until,
  ban_streak = EXCLUDED.ban_streak,
  fail_count = EXCLUDED.fail_count,
  permanent = EXCLUDED.permanent,
  updated_at = EXCLUDED.updated_at;

-- name: ListAccountState :many
SELECT * FROM account_state;
