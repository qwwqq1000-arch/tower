// Package vault stores and retrieves account OAuth credentials encrypted at rest.
package vault

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// Creds are plaintext OAuth credentials (only ever in memory, never logged).
type Creds struct {
	Access    string
	Refresh   string
	ExpiresAt int64
}

// Vault encrypts credentials with the cipher before persisting via sqlc.
type Vault struct {
	q *sqlc.Queries
	c *crypto.Cipher
}

// New builds a Vault over a pool and cipher.
func New(pool *pgxpool.Pool, c *crypto.Cipher) *Vault {
	return &Vault{q: sqlc.New(pool), c: c}
}

// Store encrypts cr and upserts the account row.
func (v *Vault) Store(ctx context.Context, id, ownerID, email, subType string, cr Creds, onboardedAt int64) error {
	accessEnc, err := v.c.Encrypt([]byte(cr.Access))
	if err != nil {
		return err
	}
	refreshEnc, err := v.c.Encrypt([]byte(cr.Refresh))
	if err != nil {
		return err
	}
	_, err = v.q.CreateAccount(ctx, sqlc.CreateAccountParams{
		ID: id, OwnerID: ownerID, Email: email, SubscriptionType: subType,
		OauthAccessEnc: accessEnc, OauthRefreshEnc: refreshEnc,
		ExpiresAt: cr.ExpiresAt, OnboardedAt: onboardedAt,
	})
	return err
}

// Load reads and decrypts an account's credentials.
func (v *Vault) Load(ctx context.Context, id string) (Creds, error) {
	row, err := v.q.GetAccount(ctx, id)
	if err != nil {
		return Creds{}, err
	}
	access, err := v.c.Decrypt(row.OauthAccessEnc)
	if err != nil {
		return Creds{}, err
	}
	refresh, err := v.c.Decrypt(row.OauthRefreshEnc)
	if err != nil {
		return Creds{}, err
	}
	return Creds{Access: string(access), Refresh: string(refresh), ExpiresAt: row.ExpiresAt}, nil
}
