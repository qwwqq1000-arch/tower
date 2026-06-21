-- name: BanTotal :one
SELECT count(*) FROM ban_episodes;

-- name: BanCountsByWeekday :many
SELECT EXTRACT(DOW FROM to_timestamp(banned_at/1000.0))::int AS bucket, count(*)::int AS n
FROM ban_episodes GROUP BY bucket ORDER BY bucket;

-- name: BanCountsByHour :many
SELECT EXTRACT(HOUR FROM to_timestamp(banned_at/1000.0))::int AS bucket, count(*)::int AS n
FROM ban_episodes GROUP BY bucket ORDER BY bucket;
