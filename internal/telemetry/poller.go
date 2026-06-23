package telemetry

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
	"github.com/qwwqq1000-arch/tower/internal/policy"
	"github.com/qwwqq1000-arch/tower/internal/state"
)

// Poller refreshes live account state from node-reported health/quota.
type Poller struct {
	Q            *sqlc.Queries
	Store        *state.Store
	Threshold    float64
	DefaultTTLMs int64
	Capacity     int
	Now          func() int64

	// Cipher decrypts a node's stored api_key before building the meridian
	// client (vault-crypto-3). May be nil when secrets are stored as plaintext.
	Cipher *crypto.Cipher

	// limitedMu guards lastLimited — the per-key last-known limited state used
	// to detect false→true transitions and emit quota_limited events exactly once.
	limitedMu   sync.Mutex
	lastLimited map[string]bool
}

// PollOnce refreshes every enabled node's accounts once.
func (p *Poller) PollOnce(ctx context.Context) error {
	mc := p.maxConcurrent(ctx)
	nodes, err := p.Q.ListNodes(ctx)
	if err != nil {
		return err
	}
	var sum5h, sum7d float64
	var cnt5h, cnt7d int
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		// CPA (CLIProxyAPI) nodes do not speak the meridian /health+/telemetry
		// protocol; their account health is governed by dispatch ban-detection.
		// Polling them with the meridian client would spuriously mark them offline.
		if strings.EqualFold(n.Kind, "cpa") {
			continue
		}
		accs, err := p.Q.ListNodeAccountsByNode(ctx, n.ID)
		if err != nil || len(accs) == 0 {
			continue
		}
		cl := nodeclient.New(n.BaseUrl, p.Cipher.DecryptOrPlaintext(n.ApiKey))
		health, healthErr := cl.Health(ctx)
		// nodeDown is true when the node is unreachable or reports unhealthy.
		// In that case all profiles on the node go offline regardless of quota.
		nodeDown := healthErr != nil || health.Status == "unhealthy"

		if !nodeDown && health.Version != "" {
			_ = p.Q.UpdateNodeVersion(ctx, sqlc.UpdateNodeVersionParams{ID: n.ID, Version: health.Version})
		}

		for _, a := range accs {
			if !a.Enabled {
				continue
			}
			key := n.ID + ":" + a.ProfileID
			// Offline is node-level only: the quota endpoint is NO LONGER polled here
			// (account-limit-reactive — quota reads are manual-only, "所有刷新都关了").
			// A reachable node ⇒ accounts online; a down/unhealthy node ⇒ all its
			// accounts offline. Per-account quota-limit + auth/ban are detected
			// reactively (dispatch responses) and on the manual 刷新 button — not here.
			p.Store.SetOffline(key, p.Capacity, nodeDown)
			p.Store.SetCapacity(key, mc)
		}
	}
	// Calculate cluster-wide average utilization for display/monitoring only.
	// This does not drive elastic scaling or dispatch decisions; per-account quotas
	// and account-level rate-limit logic (LimitedUntil) own those responsibilities.
	var avg5h, avg7d float64
	if cnt5h > 0 {
		avg5h = sum5h / float64(cnt5h)
	}
	if cnt7d > 0 {
		avg7d = sum7d / float64(cnt7d)
	}
	p.Store.SetQuotaAvg(avg5h, avg7d)
	return nil
}

// threshold reads the global policy row and returns the effective QuotaRotateThreshold.
func (p *Poller) threshold(ctx context.Context) float64 {
	rows, err := p.Q.ListPolicies(ctx)
	if err != nil {
		return p.Threshold
	}
	for _, row := range rows {
		if row.ScopeType == "global" {
			return policy.PickThreshold(row.Params, p.Threshold)
		}
	}
	return p.Threshold
}

// maxConcurrent reads the global policy row and returns the effective MaxConcurrent.
// Falls back to p.Capacity when the policy row is absent or does not override it.
func (p *Poller) maxConcurrent(ctx context.Context) int {
	rows, err := p.Q.ListPolicies(ctx)
	if err != nil {
		return p.Capacity
	}
	for _, row := range rows {
		if row.ScopeType == "global" {
			return policy.PickMaxConcurrent(row.Params, p.Capacity)
		}
	}
	return p.Capacity
}

func findProfile(q nodeclient.QuotaAll, id string) (nodeclient.ProfileQuota, bool) {
	for _, pr := range q.Profiles {
		if pr.ID == id {
			return pr, true
		}
	}
	return nodeclient.ProfileQuota{}, false
}

// Run polls on an interval until ctx is cancelled.
func (p *Poller) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	_ = p.PollOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = p.PollOnce(ctx)
		}
	}
}
