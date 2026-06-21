-- name: BanTotal :one
SELECT count(*) FROM ban_episodes;

-- name: BanCountsByWeekday :many
SELECT EXTRACT(DOW FROM to_timestamp(banned_at/1000.0))::int AS bucket, count(*)::int AS n
FROM ban_episodes GROUP BY bucket ORDER BY bucket;

-- name: BanCountsByHour :many
SELECT EXTRACT(HOUR FROM to_timestamp(banned_at/1000.0))::int AS bucket, count(*)::int AS n
FROM ban_episodes GROUP BY bucket ORDER BY bucket;

-- name: BanTotalByOwner :one
SELECT count(*) FROM ban_episodes be
JOIN node_accounts na ON na.node_id = be.node_id AND na.profile_id = be.profile_id
JOIN accounts a ON a.id = na.account_id
WHERE a.owner_id = $1;

-- name: BanCountsByWeekdayForOwner :many
SELECT EXTRACT(DOW FROM to_timestamp(be.banned_at/1000.0))::int AS bucket, count(*)::int AS n
FROM ban_episodes be
JOIN node_accounts na ON na.node_id = be.node_id AND na.profile_id = be.profile_id
JOIN accounts a ON a.id = na.account_id
WHERE a.owner_id = $1
GROUP BY bucket ORDER BY bucket;

-- name: BanCountsByHourForOwner :many
SELECT EXTRACT(HOUR FROM to_timestamp(be.banned_at/1000.0))::int AS bucket, count(*)::int AS n
FROM ban_episodes be
JOIN node_accounts na ON na.node_id = be.node_id AND na.profile_id = be.profile_id
JOIN accounts a ON a.id = na.account_id
WHERE a.owner_id = $1
GROUP BY bucket ORDER BY bucket;
