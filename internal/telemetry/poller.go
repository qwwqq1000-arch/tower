package telemetry

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
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
	thresh := p.threshold(ctx)
	mc := p.maxConcurrent(ctx)
	nodes, err := p.Q.ListNodes(ctx)
	if err != nil {
		return err
	}
	now := p.Now()
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

		var quota nodeclient.QuotaAll
		var quotaErr error
		if !nodeDown {
			quota, quotaErr = cl.QuotaAll(ctx)
			if quotaErr != nil {
				log.Printf("poller: node %s health OK but QuotaAll failed (transient?): %v — skipping offline update this cycle", n.ID, quotaErr)
			}
			if health.Version != "" {
				_ = p.Q.UpdateNodeVersion(ctx, sqlc.UpdateNodeVersionParams{ID: n.ID, Version: health.Version})
			}
			// Accumulate utilization across every profile window for averaging.
			for _, pr := range quota.Profiles {
				for _, win := range pr.Windows {
					switch win.Type {
					case "five_hour":
						sum5h += win.Utilization
						cnt5h++
					case "seven_day":
						sum7d += win.Utilization
						cnt7d++
					}
				}
			}
		}

		for _, a := range accs {
			if !a.Enabled {
				continue
			}
			key := n.ID + ":" + a.ProfileID

			// Derive per-account offline from per-profile IsActive so a single
			// logged-out profile does not take all sibling profiles offline.
			//
			// When the node is reachable (nodeDown=false) but QuotaAll returned a
			// transient error, we cannot distinguish "profile absent" from "fetch
			// failed". Flipping all profiles offline in that case causes unnecessary
			// dispatch disruption. Instead we skip the SetOffline update for this
			// cycle and preserve whatever stale state the store already holds.
			pq, foundInQuota := findProfile(quota, a.ProfileID)
			if quotaErr != nil && !nodeDown {
				// Transient quota fetch failure on an otherwise healthy node:
				// keep stale offline state; skip limit update too.
				p.Store.SetCapacity(key, mc)
				continue
			}
			profileOffline := OfflineForProfile(health, healthErr, pq, foundInQuota && !nodeDown)

			p.Store.SetOffline(key, p.Capacity, profileOffline)
			p.Store.SetCapacity(key, mc)
			if profileOffline {
				continue
			}
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
