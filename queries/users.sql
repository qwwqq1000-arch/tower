-- name: DeleteTenant :exec
DELETE FROM tenants WHERE id = $1;

-- name: SetTenantRole :exec
UPDATE tenants SET role = $2 WHERE id = $1;

-- name: SetTenantPassword :exec
UPDATE tenants SET pw_hash = $2, salt = $3, must_change_pw = FALSE WHERE id = $1;

-- name: SetTenantChannelRate :exec
UPDATE tenants SET channel_rate = $2 WHERE id = $1;

-- name: SetTenantFallbackLimit :exec
UPDATE tenants SET fallback_limit = $2 WHERE id = $1;

-- name: GetSessionEpoch :one
SELECT session_epoch FROM tenants WHERE id = $1;

-- name: BumpSessionEpoch :exec
UPDATE tenants SET session_epoch = session_epoch + 1 WHERE id = $1;
