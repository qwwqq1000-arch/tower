-- name: InsertDispatchEvent :exec
INSERT INTO dispatch_events (ts, type, target, owner_id, detail) VALUES ($1,$2,$3,$4,$5);

-- name: ListRecentEvents :many
SELECT * FROM dispatch_events ORDER BY ts DESC, id DESC LIMIT $1;

-- name: ListEventsByOwner :many
SELECT * FROM dispatch_events WHERE owner_id = $1 ORDER BY ts DESC, id DESC LIMIT $2;

-- name: InsertBanEpisode :exec
INSERT INTO ban_episodes (node_id, profile_id, banned_at, detail) VALUES ($1,$2,$3,$4);

-- name: RecoverBanEpisode :exec
UPDATE ban_episodes SET recovered_at = $3, survival_ms = $3 - banned_at
WHERE node_id = $1 AND profile_id = $2 AND recovered_at = 0;
