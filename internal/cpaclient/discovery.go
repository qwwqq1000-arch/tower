package cpaclient

import (
	"context"
	"strings"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// RefreshQuotaForNode fetches + persists the live usage for every claude/anthropic CPA
// account on a node (manual on-demand — the periodic poll was removed; quota refreshes
// only on the 刷新 button now). Display only: it does NOT project utilization into the
// limit store, so it never re-introduces poll-based rotation (that stays reactive).
// Returns the number of accounts whose quota was refreshed.
func RefreshQuotaForNode(ctx context.Context, q syncQuerier, node sqlc.Node, cipher *crypto.Cipher) (int, error) {
	if !strings.EqualFold(node.Kind, "cpa") || strings.TrimSpace(node.MgmtKey) == "" || !node.Enabled {
		return 0, nil
	}
	c := New(node.BaseUrl, cipher.DecryptOrPlaintext(node.MgmtKey))
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range accounts {
		// The account-usage endpoint is Anthropic OAuth-only.
		if !strings.EqualFold(a.Provider, "claude") && !strings.EqualFold(a.Provider, "anthropic") {
			continue
		}
		aid := accountID(node.ID, a)
		now := time.Now().UnixMilli()
		if u, uerr := c.Usage(ctx, a.AuthIndex, a.DispatchSelector()); uerr != nil {
			_ = q.SetCpaQuotaFetchError(ctx, sqlc.SetCpaQuotaFetchErrorParams{AccountID: aid, QuotaFetchError: uerr.Error(), UpdatedAt: now})
		} else if u != nil {
			_ = q.UpsertCpaQuota(ctx, quotaParams(aid, u, now))
			n++
		}
	}
	return n, nil
}

// RefreshAllQuota refreshes quota for every CPA node in the registry (the 刷新全部额度
// button). Best-effort: a failing node does not abort the rest.
func RefreshAllQuota(ctx context.Context, q *sqlc.Queries, cipher *crypto.Cipher) (int, error) {
	nodes, err := q.ListNodes(ctx)
	if err != nil {
		return 0, err
	}
	total := 0
	for _, n := range nodes {
		if !strings.EqualFold(n.Kind, "cpa") {
			continue
		}
		c, _ := RefreshQuotaForNode(ctx, q, n, cipher)
		total += c
	}
	return total, nil
}

// syncQuerier is the narrow DB interface required by Sync. *sqlc.Queries satisfies it.
// Using an interface lets unit tests inject a stub without a real database.
type syncQuerier interface {
	UpsertCpaAccount(ctx context.Context, arg sqlc.UpsertCpaAccountParams) error
	UpsertCpaNodeAccount(ctx context.Context, arg sqlc.UpsertCpaNodeAccountParams) error
	UpsertCpaQuota(ctx context.Context, arg sqlc.UpsertCpaQuotaParams) error
	SetCpaQuotaFetchError(ctx context.Context, arg sqlc.SetCpaQuotaFetchErrorParams) error
}

// RotateConfig threads the live state store and the per-account slot capacity
// (MaxConcurrent) into CPA discovery. Capacity is the effective value for the
// current cycle, refreshed from the live global policy (via Refresh) before each
// SyncAll so CPA accounts honor the MaxConcurrent override exactly like the
// meridian poller does; BaseCapacity holds the startup default used as fallback
// when the policy row is absent or does not override.
type RotateConfig struct {
	Store        *state.Store
	Capacity     int   // effective per-account slot capacity for this cycle (MaxConcurrent)
	DefaultTTLMs int64 // fallback limit window when resets_at is unknown

	// Cipher is the runtime master-key cipher (vault-crypto-1) used to decrypt a
	// node's stored mgmt_key before building the CPA management client
	// (vault-crypto-3). May be nil when secrets are stored as plaintext.
	Cipher *crypto.Cipher

	// BaseCapacity is the compiled-in default applied when the global policy row
	// is missing or omits an override. Mirrors Poller.Capacity. When zero,
	// Capacity is used as the base.
	BaseCapacity int
}

// Refresh resolves the effective Capacity from the live global policy row,
// mirroring telemetry.Poller.maxConcurrent so CPA accounts gate on the same live
// MaxConcurrent as meridian accounts. It must be called once per cycle before
// SyncAll. On any error (or when rot is nil) it leaves the current effective
// value unchanged.
func (rot *RotateConfig) Refresh(ctx context.Context, q *sqlc.Queries) {
	if rot == nil {
		return
	}
	baseCap := rot.BaseCapacity
	if baseCap == 0 {
		baseCap = rot.Capacity
	}
	rot.Capacity = baseCap
	rows, err := q.ListPolicies(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.ScopeType == "global" {
			rot.Capacity = policy.PickMaxConcurrent(row.Params, baseCap)
			return
		}
	}
}

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
//
// When a quota fetch fails for a claude/anthropic account, Sync records the
// error message in cpa_account_quota.quota_fetch_error so the UI can show
// "quota unavailable" instead of silently displaying null (cpa-3).
func Sync(ctx context.Context, q syncQuerier, node sqlc.Node, rot *RotateConfig) (int, error) {
	if !strings.EqualFold(node.Kind, "cpa") || strings.TrimSpace(node.MgmtKey) == "" || !node.Enabled {
		return 0, nil
	}
	// Decrypt the stored mgmt_key transparently before building the CPA client
	// (vault-crypto-3): ciphertext rows decrypt, legacy plaintext rows pass
	// through unchanged. A nil cipher (plaintext-mode) is a no-op.
	var cipher *crypto.Cipher
	if rot != nil {
		cipher = rot.Cipher
	}
	c := New(node.BaseUrl, cipher.DecryptOrPlaintext(node.MgmtKey))
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return 0, err
	}
	acctOwner := node.AccountOwnerID
	if acctOwner == "" {
		acctOwner = node.OwnerID
	}
	for _, a := range accounts {
		aid := accountID(node.ID, a)
		email := a.Email
		if email == "" {
			email = a.Name
		}
		if err := q.UpsertCpaAccount(ctx, sqlc.UpsertCpaAccountParams{
			ID:               aid,
			OwnerID:          acctOwner,
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
			BoundAt:   time.Now().UnixMilli(),
		}); err != nil {
			return 0, err
		}
		// NOTE: account discovery only — the Anthropic account-usage endpoint is NO
		// LONGER polled here (account-limit-reactive). Auto-polling it was slow,
		// inaccurate, and piled extra requests onto loaded accounts (risking 429s).
		// Rotation is now reactive: a dispatch that returns a usage-limit response sets
		// the account limited until the reset time parsed from it (auto-recovers). The
		// quota numbers are refreshed only on the manual "刷新" button (node_control).
	}
	return len(accounts), nil
}

// SyncAll discovers accounts for every CPA node in the registry into Tower's
// account pool. The effective per-account capacity (MaxConcurrent) is resolved
// from the live global policy at the start of each cycle (mirroring the meridian
// poller), so the MaxConcurrent override gates CPA and meridian accounts
// identically. Discovery is display/pool-only: it does NOT project utilization
// into the limit store (rotation is reactive — driven by dispatch responses and
// the spend caps, not by polled utilization).
func SyncAll(ctx context.Context, q *sqlc.Queries, rot *RotateConfig) error {
	rot.Refresh(ctx, q)
	nodes, err := q.ListNodes(ctx)
	if err != nil {
		return err
	}
	var firstErr error
	for _, n := range nodes {
		if !strings.EqualFold(n.Kind, "cpa") {
			continue
		}
		if _, err := Sync(ctx, q, n, rot); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
