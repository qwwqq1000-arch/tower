package reconcile

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/crypto"
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

	// Cipher decrypts a node's stored api_key before building the meridian
	// client (vault-crypto-3). May be nil when secrets are stored as plaintext.
	Cipher *crypto.Cipher
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
		_, _ = ReconcileNode(ctx, nodeclient.New(n.BaseUrl, r.Cipher.DecryptOrPlaintext(n.ApiKey)), desired)
	}
	return nil
}

// Run reconciles on an interval until ctx is cancelled.
//
// Activation note (provision-1): in the current deployment the reconciler is
// constructed but its Run loop is not started by default. To enable active
// feature reconciliation, call r.Run(ctx, interval) from cmd/tower/main.go
// after building the Reconciler. A 5-minute interval is recommended for most
// fleets. Leave the loop disabled if your nodes are managed solely through
// the dashboard (desired-state pushes are applied immediately on save via
// the PUT /api/admin/desired-features route); the reconciler is only needed
// for eventual-consistency healing after network interruptions or node restarts.
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
