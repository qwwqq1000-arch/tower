-- name: InsertAudit :exec
INSERT INTO audit_log (ts, actor, action, target, before, after)
VALUES ($1,$2,$3,$4,$5,$6);

-- name: ListRecentAudit :many
SELECT * FROM audit_log ORDER BY ts DESC, id DESC LIMIT $1;
