package cpaclient

import (
	"context"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// statusFor maps a CPA account's reported state to a Tower account status.
func statusFor(a Account) string {
	if a.Disabled {
		return "disabled"
	}
	if a.Unavailable {
		return "banned"
	}
	if strings.EqualFold(a.Status, "error") {
		return "banned"
	}
	return "active"
}

// accountID is the deterministic Tower account id for a CPA account on a node.
func accountID(nodeID string, a Account) string {
	return "cpa:" + nodeID + ":" + a.ID
}

// Sync lists the accounts on one CPA node and upserts them into Tower's account
// pool (accounts + node_accounts) so each appears in the 号库 and is dispatchable
// (profile_id carries the X-CLIProxy-Account selector). Returns the number of
// accounts discovered.
func Sync(ctx context.Context, q *sqlc.Queries, node sqlc.Node) (int, error) {
	if !strings.EqualFold(node.Kind, "cpa") || strings.TrimSpace(node.MgmtKey) == "" {
		return 0, nil
	}
	accounts, err := New(node.BaseUrl, node.MgmtKey).ListAccounts(ctx)
	if err != nil {
		return 0, err
	}
	for _, a := range accounts {
		aid := accountID(node.ID, a)
		email := a.Email
		if email == "" {
			email = a.Name
		}
		if err := q.UpsertCpaAccount(ctx, sqlc.UpsertCpaAccountParams{
			ID:               aid,
			OwnerID:          node.OwnerID,
			Email:            email,
			SubscriptionType: a.AccountType,
			Status:           statusFor(a),
		}); err != nil {
			return 0, err
		}
		if err := q.UpsertCpaNodeAccount(ctx, sqlc.UpsertCpaNodeAccountParams{
			NodeID:    node.ID,
			AccountID: aid,
			ProfileID: a.DispatchSelector(),
			Enabled:   !a.Disabled,
		}); err != nil {
			return 0, err
		}
	}
	return len(accounts), nil
}

// SyncAll discovers accounts for every CPA node in the registry.
func SyncAll(ctx context.Context, q *sqlc.Queries) error {
	nodes, err := q.ListNodes(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, n := range nodes {
		if !strings.EqualFold(n.Kind, "cpa") {
			continue
		}
		if _, err := Sync(ctx, q, n); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
