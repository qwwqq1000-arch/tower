-- name: UpsertCpaQuota :exec
INSERT INTO cpa_account_quota (account_id, five_hour_util, five_hour_resets_at, seven_day_util, seven_day_resets_at, seven_day_sonnet_util, seven_day_sonnet_resets_at, updated_at)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
ON CONFLICT (account_id) DO UPDATE SET
  five_hour_util = EXCLUDED.five_hour_util,
  five_hour_resets_at = EXCLUDED.five_hour_resets_at,
  seven_day_util = EXCLUDED.seven_day_util,
  seven_day_resets_at = EXCLUDED.seven_day_resets_at,
  seven_day_sonnet_util = EXCLUDED.seven_day_sonnet_util,
  seven_day_sonnet_resets_at = EXCLUDED.seven_day_sonnet_resets_at,
  updated_at = EXCLUDED.updated_at;

-- name: ListCpaQuota :many
SELECT * FROM cpa_account_quota;
