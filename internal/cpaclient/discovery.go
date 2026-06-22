package cpaclient

import (
	"context"
	"strings"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
	"github.com/qwwqq1000-arch/tower/internal/telemetry"
)

// RotateConfig threads the live state store and rotation parameters into CPA
// discovery so a CPA account's quota utilization gates dispatch the same way the
// meridian telemetry poller gates meridian accounts. A nil Store disables the
// projection (e.g. in discovery-only tests).
//
// Threshold and Capacity are the effective values for the current cycle. They
// are refreshed from the live global policy (via Refresh) before each SyncAll so
// CPA accounts honor QuotaRotateThreshold / MaxConcurrent overrides exactly like
// the meridian poller does; BaseThreshold and BaseCapacity hold the startup
// defaults used as fallback when the policy row is absent or does not override.
type RotateConfig struct {
	Store        *state.Store
	Threshold    float64 // effective QuotaRotateThreshold for this cycle (0..1 fraction)
	Capacity     int     // effective per-account slot capacity for this cycle (MaxConcurrent)
	DefaultTTLMs int64   // fallback limit window when resets_at is unknown

	// BaseThreshold/BaseCapacity are the compiled-in defaults applied when the
	// global policy row is missing or omits an override. Mirrors Poller.Threshold
	// / Poller.Capacity. When zero, Threshold/Capacity are used as the base.
	BaseThreshold float64
	BaseCapacity  int
}

// Refresh resolves the effective Threshold and Capacity from the live global
// policy row, mirroring telemetry.Poller.threshold / Poller.maxConcurrent so CPA
// accounts gate on the same live values as meridian accounts. It must be called
// once per cycle before SyncAll. On any error (or when rot is nil) it leaves the
// current effective values unchanged.
func (rot *RotateConfig) Refresh(ctx context.Context, q *sqlc.Queries) {
	if rot == nil {
		return
	}
	baseThresh := rot.BaseThreshold
	if baseThresh == 0 {
		baseThresh = rot.Threshold
	}
	baseCap := rot.BaseCapacity
	if baseCap == 0 {
		baseCap = rot.Capacity
	}
	rot.Threshold = baseThresh
	rot.Capacity = baseCap
	rows, err := q.ListPolicies(ctx)
	if err != nil {
		return
	}
	for _, row := range rows {
		if row.ScopeType == "global" {
			rot.Threshold = policy.PickThreshold(row.Params, baseThresh)
			rot.Capacity = policy.PickMaxConcurrent(row.Params, baseCap)
			return
		}
	}
}

// cpaWindows flattens a CPA Usage payload into telemetry windows for projection.
func cpaWindows(u *Usage) []telemetry.CpaWindow {
	var ws []telemetry.CpaWindow
	if u.FiveHour != nil {
		ws = append(ws, telemetry.CpaWindow{Type: "five_hour", Utilization: u.FiveHour.Utilization, ResetsAt: u.FiveHour.ResetsAt})
	}
	if u.SevenDay != nil {
		ws = append(ws, telemetry.CpaWindow{Type: "seven_day", Utilization: u.SevenDay.Utilization, ResetsAt: u.SevenDay.ResetsAt})
	}
	if u.SevenDayOpus != nil {
		ws = append(ws, telemetry.CpaWindow{Type: "seven_day_opus", Utilization: u.SevenDayOpus.Utilization, ResetsAt: u.SevenDayOpus.ResetsAt})
	}
	if u.SevenDaySonnet != nil {
		ws = append(ws, telemetry.CpaWindow{Type: "seven_day_sonnet", Utilization: u.SevenDaySonnet.Utilization, ResetsAt: u.SevenDaySonnet.ResetsAt})
	}
	return ws
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
func Sync(ctx context.Context, q *sqlc.Queries, node sqlc.Node, rot *RotateConfig) (int, error) {
	if !strings.EqualFold(node.Kind, "cpa") || strings.TrimSpace(node.MgmtKey) == "" || !node.Enabled {
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
		// Best-effort quota refresh (only for claude/anthropic accounts — the usage
		// endpoint is Anthropic OAuth-only).
		if strings.EqualFold(a.Provider, "claude") || strings.EqualFold(a.Provider, "anthropic") {
			if u, uerr := c.Usage(ctx, a.DispatchSelector()); uerr == nil && u != nil {
				_ = q.UpsertCpaQuota(ctx, quotaParams(aid, u, time.Now().UnixMilli()))
				// Project utilization into the live store so a saturated CPA
				// account rotates out of dispatch, just like meridian accounts.
				if rot != nil && rot.Store != nil {
					now := time.Now().UnixMilli()
					limits := telemetry.LimitsFromCpaQuota(cpaWindows(u), rot.Threshold, now, rot.DefaultTTLMs)
					key := node.ID + ":" + a.DispatchSelector()
					rot.Store.SetLimited(key, rot.Capacity, limits)
				}
			}
		}
	}
	return len(accounts), nil
}

// SyncAll discovers accounts for every CPA node in the registry, projecting each
// account's quota utilization into rot.Store (when non-nil) so saturated CPA
// accounts rotate out of dispatch. The effective rotation threshold and capacity
// are resolved from the live global policy at the start of each cycle (mirroring
// the meridian poller), so QuotaRotateThreshold / MaxConcurrent overrides gate
// CPA and meridian accounts identically.
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
