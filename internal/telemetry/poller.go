package telemetry

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/events"
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

	// limitedMu guards lastLimited — the per-key last-known limited state used
	// to detect false→true transitions and emit quota_limited events exactly once.
	limitedMu   sync.Mutex
	lastLimited map[string]bool
}

// PollOnce refreshes every enabled node's accounts once.
func (p *Poller) PollOnce(ctx context.Context) error {
	thresh := p.threshold(ctx)
	mc := p.maxConcurrent(ctx)
	nodes, err := p.Q.ListNodes(ctx)
	if err != nil {
		return err
	}
	now := p.Now()
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		accs, err := p.Q.ListNodeAccountsByNode(ctx, n.ID)
		if err != nil || len(accs) == 0 {
			continue
		}
		cl := nodeclient.New(n.BaseUrl, n.ApiKey)
		health, healthErr := cl.Health(ctx)
		offline := OfflineFromHealth(health, healthErr)

		var quota nodeclient.QuotaAll
		if !offline {
			quota, _ = cl.QuotaAll(ctx)
			if healthErr == nil && health.Version != "" {
				_ = p.Q.UpdateNodeVersion(ctx, sqlc.UpdateNodeVersionParams{ID: n.ID, Version: health.Version})
			}
		}

		for _, a := range accs {
			if !a.Enabled {
				continue
			}
			key := n.ID + ":" + a.ProfileID
			if offline {
				p.Store.SetOffline(key, p.Capacity, true)
				p.Store.SetCapacity(key, mc)
				continue
			}
			p.Store.SetOffline(key, p.Capacity, false)
			p.Store.SetCapacity(key, mc)
			if pq, ok := findProfile(quota, a.ProfileID); ok {
				wasLimited := p.Store.IsLimited(key, now)
				limits := LimitsFromQuota(pq, thresh, now, p.DefaultTTLMs)
				p.Store.SetLimited(key, p.Capacity, limits)
				isLimited := p.Store.IsLimited(key, now)

				p.limitedMu.Lock()
				if p.lastLimited == nil {
					p.lastLimited = make(map[string]bool)
				}
				prev := p.lastLimited[key]
				if isLimited {
					p.lastLimited[key] = true
				} else {
					delete(p.lastLimited, key)
				}
				p.limitedMu.Unlock()

				// Record event only on false→true transition.
				if !wasLimited && isLimited && !prev {
					_ = events.Record(ctx, p.Q, now, events.Event{Type: "quota_limited", Target: key, OwnerID: ""})
				}
			}
		}
	}
	return nil
}

// pickThreshold extracts QuotaRotateThreshold from a JSON policy patch.
// If the patch is absent, unparseable, or contains an invalid value (<=0 or >1),
// it returns def unchanged.
func pickThreshold(patchJSON []byte, def float64) float64 {
	var p policy.Patch
	if err := json.Unmarshal(patchJSON, &p); err != nil {
		return def
	}
	if p.QuotaRotateThreshold != nil {
		if v := *p.QuotaRotateThreshold; v > 0 && v <= 1 {
			return v
		}
	}
	return def
}

// threshold reads the global policy row and returns the effective QuotaRotateThreshold.
func (p *Poller) threshold(ctx context.Context) float64 {
	rows, err := p.Q.ListPolicies(ctx)
	if err != nil {
		return p.Threshold
	}
	for _, row := range rows {
		if row.ScopeType == "global" {
			return pickThreshold(row.Params, p.Threshold)
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
			var patch policy.Patch
			if json.Unmarshal(row.Params, &patch) == nil {
				if patch.MaxConcurrent != nil && *patch.MaxConcurrent > 0 {
					return *patch.MaxConcurrent
				}
			}
			break
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
