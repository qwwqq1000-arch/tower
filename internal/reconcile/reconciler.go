package reconcile

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

// NodeAPI is the subset of the node client the reconciler needs.
type NodeAPI interface {
	GetFeatures(ctx context.Context) (map[string]map[string]any, error)
	PatchFeatures(ctx context.Context, adapter string, patch map[string]any) error
}

// ReconcileNode brings one node's SDK features to the desired state.
func ReconcileNode(ctx context.Context, api NodeAPI, desired map[string]map[string]any) (int, error) {
	actual, err := api.GetFeatures(ctx)
	if err != nil {
		return 0, err
	}
	patches := Diff(desired, actual)
	patched := 0
	for adapter, patch := range patches {
		if err := api.PatchFeatures(ctx, adapter, patch); err != nil {
			return patched, err
		}
		patched++
	}
	return patched, nil
}

// Reconciler periodically enforces desired features across all enabled nodes.
type Reconciler struct {
	Q *sqlc.Queries
}

// RunOnce reconciles every enabled node once (best-effort per node).
func (r *Reconciler) RunOnce(ctx context.Context) error {
	raw, err := r.Q.GetDesiredFeatures(ctx)
	if err != nil {
		return nil // no desired row yet → nothing to enforce
	}
	var desired map[string]map[string]any
	if json.Unmarshal(raw, &desired) != nil || len(desired) == 0 {
		return nil
	}
	nodes, err := r.Q.ListNodes(ctx)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if !n.Enabled {
			continue
		}
		// CPA nodes don't speak the meridian /settings feature protocol — skip
		// them (mirrors the telemetry poller).
		if strings.EqualFold(n.Kind, "cpa") {
			continue
		}
		_, _ = ReconcileNode(ctx, nodeclient.New(n.BaseUrl, n.ApiKey), desired)
	}
	return nil
}

// Run reconciles on an interval until ctx is cancelled.
func (r *Reconciler) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	_ = r.RunOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_ = r.RunOnce(ctx)
		}
	}
}
