package telemetry

import (
	"context"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
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
}

// PollOnce refreshes every enabled node's accounts once.
func (p *Poller) PollOnce(ctx context.Context) error {
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
				continue
			}
			p.Store.SetOffline(key, p.Capacity, false)
			if pq, ok := findProfile(quota, a.ProfileID); ok {
				p.Store.SetLimited(key, p.Capacity, LimitsFromQuota(pq, p.Threshold, now, p.DefaultTTLMs))
			}
		}
	}
	return nil
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
