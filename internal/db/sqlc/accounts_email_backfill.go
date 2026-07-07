package sqlc

import "context"

// UpdateAccountEmailIfEmpty backfills an account's email from a node's live
// /health once it appears. Accounts onboarded via the OAuth wizard are created
// before the node finishes loading the account info (email lags login by a few
// seconds), so they land in the pool with an empty email. The poller calls this
// each cycle; the WHERE clause makes it a no-op once the email is already set.
const updateAccountEmailIfEmpty = `UPDATE accounts SET email=$2 WHERE id=$1 AND (email = '' OR email IS NULL)`

type UpdateAccountEmailIfEmptyParams struct {
	ID    string
	Email string
}

func (q *Queries) UpdateAccountEmailIfEmpty(ctx context.Context, arg UpdateAccountEmailIfEmptyParams) error {
	_, err := q.db.Exec(ctx, updateAccountEmailIfEmpty, arg.ID, arg.Email)
	return err
}
