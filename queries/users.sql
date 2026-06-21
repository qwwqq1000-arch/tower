-- name: DeleteTenant :exec
DELETE FROM tenants WHERE id = $1;

-- name: SetTenantRole :exec
UPDATE tenants SET role = $2 WHERE id = $1;

-- name: SetTenantPassword :exec
UPDATE tenants SET pw_hash = $2, salt = $3, must_change_pw = FALSE WHERE id = $1;
