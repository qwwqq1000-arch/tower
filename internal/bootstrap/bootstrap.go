// Package bootstrap seeds the first admin user on a fresh install.
package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func randID(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// EnsureAdmin creates a superadmin from username/password when the tenants
// table is empty and both are non-empty. Returns whether it created one.
func EnsureAdmin(ctx context.Context, q *sqlc.Queries, username, password string) (bool, error) {
	if username == "" || password == "" {
		return false, nil
	}
	n, err := q.CountTenants(ctx)
	if err != nil {
		return false, err
	}
	if n > 0 {
		return false, nil
	}
	hash, salt, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	_, err = q.CreateTenant(ctx, sqlc.CreateTenantParams{
		ID:        randID("u_"),
		Username:  username,
		PwHash:    hash,
		Salt:      salt,
		Role:      "superadmin",
		IngestKey: randID("ik_"),
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
