package cpaclient

import (
	"context"
	"strings"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// quotaParams maps a CPA Usage payload to the DB upsert params.
func quotaParams(accountID string, u *Usage, now int64) sqlc.UpsertCpaQuotaParams {
	p := sqlc.UpsertCpaQuotaParams{AccountID: accountID, UpdatedAt: now}
	if u.FiveHour != nil {
		p.FiveHourUtil = u.FiveHour.Utilization
		p.FiveHourResetsAt = u.FiveHour.ResetsAt
	}
	if u.SevenDay != nil {
		p.SevenDayUtil = u.SevenDay.Utilization
		p.SevenDayResetsAt = u.SevenDay.ResetsAt
	}
	if u.SevenDaySonnet != nil {
		p.SevenDaySonnetUtil = u.SevenDaySonnet.Utilization
		p.SevenDaySonnetResetsAt = u.SevenDaySonnet.ResetsAt
	}
	return p
}

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
	c := New(node.BaseUrl, node.MgmtKey)
	accounts, err := c.ListAccounts(ctx)
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
		// Best-effort quota refresh (only for claude/anthropic accounts).
		if u, uerr := c.Usage(ctx, a.DispatchSelector()); uerr == nil && u != nil {
			_ = q.UpsertCpaQuota(ctx, quotaParams(aid, u, time.Now().UnixMilli()))
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
