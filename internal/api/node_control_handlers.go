package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

// nodeConsoleURLHandler returns the management-console URL for a node. For meridian it
// includes the node's own key (its dashboard authenticates via ?key=<apiKey>), so the
// 控制台 button opens an already-authenticated panel (node-console-1). The decrypted key
// is returned only to an authenticated admin who already owns the node.
func nodeConsoleURLHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n, err := q.GetNode(r.Context(), r.PathValue("id"))
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
			return
		}
		if owner, all := scope(r); !all && n.OwnerID != owner {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "node not found"})
			return
		}
		base := strings.TrimRight(n.BaseUrl, "/")
		url := base + "/management.html" // CPA panel
		if !strings.EqualFold(n.Kind, "cpa") {
			url = base + "/?key=" + cipher.DecryptOrPlaintext(n.ApiKey) // meridian dashboard at root
		}
		writeJSON(w, http.StatusOK, map[string]string{"url": url})
	}
}

// accountsRefreshQuotaHandler refreshes quota for ALL CPA accounts on demand (the 号库
// 刷新全部额度 button). It first runs SyncAll to ingest any newly-added CPA node
// accounts into the pool (best-effort: SyncAll errors are logged but do not abort
// the quota refresh). Then RefreshAllQuota fetches live quota for every ingested
// account. This means clicking 刷新 immediately after adding a CPA node loads its
// accounts without waiting for the 5-minute auto-Sync timer.
func accountsRefreshQuotaHandler(q *sqlc.Queries, cipher *crypto.Cipher, rot *cpaclient.RotateConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Ingest accounts from all CPA nodes first (best-effort).
		if err := cpaclient.SyncAll(r.Context(), q, rot); err != nil {
			log.Printf("accountsRefreshQuota: SyncAll (best-effort): %v", err)
		}
		n, err := cpaclient.RefreshAllQuota(r.Context(), q, cipher)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"refreshed": n, "synced": true})
	}
}

// accountRefreshQuotaHandler refreshes quota for one account (the per-row 刷新 button).
// For CPA accounts, it first runs Sync on the owning node to ingest any newly-added
// accounts into the pool (best-effort: Sync errors are logged but do not abort the
// quota refresh). Then RefreshQuotaForNode fetches live quota for that node's accounts.
// Meridian accounts are a no-op (meridian quota is already live).
func accountRefreshQuotaHandler(q *sqlc.Queries, cipher *crypto.Cipher, rot *cpaclient.RotateConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		aid := r.PathValue("accountId")
		if strings.HasPrefix(aid, "cpa:") {
			if parts := strings.SplitN(aid, ":", 3); len(parts) >= 2 {
				if node, err := q.GetNode(r.Context(), parts[1]); err == nil {
					// Ingest accounts from this node first (best-effort).
					if _, serr := cpaclient.Sync(r.Context(), q, node, rot); serr != nil {
						log.Printf("accountRefreshQuota: Sync node %s (best-effort): %v", node.ID, serr)
					}
					n, rerr := cpaclient.RefreshQuotaForNode(r.Context(), q, node, cipher)
					if rerr != nil {
						writeJSON(w, http.StatusBadGateway, map[string]string{"error": rerr.Error()})
						return
					}
					writeJSON(w, http.StatusOK, map[string]any{"refreshed": n, "synced": true})
					return
				}
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"refreshed": 0})
	}
}

func nodeFeaturesGetHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}
		f, err := cl.GetFeatures(r.Context())
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, f)
	}
}

func nodeFeaturesPatchHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}
		var patch map[string]any
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad patch"})
			return
		}
		if err := cl.PatchFeatures(r.Context(), r.PathValue("adapter"), patch); err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func nodeRefreshHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}
		if err := cl.AuthRefresh(r.Context(), ""); err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func nodeEnableHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownsNodeID(r, q, r.PathValue("id")) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct{ Enabled bool }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetNodeEnabled(r.Context(), sqlc.SetNodeEnabledParams{ID: r.PathValue("id"), Enabled: body.Enabled}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func nodePassthroughHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !ownsNodeID(r, q, r.PathValue("id")) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		var body struct{ Passthrough bool }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetNodePassthrough(r.Context(), sqlc.SetNodePassthroughParams{ID: r.PathValue("id"), Passthrough: body.Passthrough}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

// isCPAKind reports whether a node kind string identifies a CLIProxyAPI node.
// Extracted as a pure helper so it can be unit-tested independently of the DB.
func isCPAKind(kind string) bool { return strings.EqualFold(kind, "cpa") }

// cpaNotApplicable writes a 409 Conflict response when the node is a CPA node
// and the called endpoint is meridian-only. Returns true if the response was
// written (caller must return immediately), false otherwise.
func cpaNotApplicable(w http.ResponseWriter, kind string) bool {
	if !isCPAKind(kind) {
		return false
	}
	writeJSON(w, 409, map[string]string{"error": "not applicable for CPA nodes"})
	return true
}

// cpaQuotaAll fetches usage for every account on a CPA node by listing accounts
// and then calling the per-account usage endpoint. The result mirrors the shape of
// nodeclient.QuotaAll so callers can render it uniformly.
func cpaQuotaAll(ctx context.Context, c *cpaclient.Client) (map[string]any, error) {
	accounts, err := c.ListAccounts(ctx)
	if err != nil {
		return nil, err
	}
	type accountUsage struct {
		ID      string          `json:"id"`
		Email   string          `json:"email"`
		Usage   *cpaclient.Usage `json:"usage"`
		FetchErr string         `json:"fetchErr,omitempty"`
	}
	out := make([]accountUsage, 0, len(accounts))
	for _, a := range accounts {
		au := accountUsage{ID: a.ID, Email: a.Email}
		if u, uerr := c.Usage(ctx, a.AuthIndex, a.DispatchSelector()); uerr == nil {
			au.Usage = u
		} else {
			au.FetchErr = uerr.Error()
		}
		out = append(out, au)
	}
	return map[string]any{"accounts": out}, nil
}

func nodeQuotaHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		// CPA nodes expose quota via the CPA management API, not the meridian
		// /v1/usage/quota/all endpoint (cpa-1).
		if isCPAKind(n.Kind) {
			cc := cpaclient.New(n.BaseUrl, cipher.DecryptOrPlaintext(n.MgmtKey))
			result, err := cpaQuotaAll(r.Context(), cc)
			if err != nil {
				writeJSON(w, 502, map[string]string{"error": err.Error()})
				return
			}
			writeJSON(w, 200, result)
			return
		}
		quota, err := cl.QuotaAll(r.Context())
		if err != nil {
			writeJSON(w, 502, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, quota)
	}
}

func nodeTelemetryHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, n, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
			return
		}
		if cpaNotApplicable(w, n.Kind) {
			return
		}

		health, healthErr := cl.Health(r.Context())
		var healthOut any
		if healthErr == nil {
			healthOut = map[string]any{
				"version":          health.Version,
				"loggedIn":         health.Auth.LoggedIn,
				"email":            health.Auth.Email,
				"subscriptionType": health.Auth.SubscriptionType,
				"mode":             health.Mode,
			}
		} else {
			healthOut = nil
		}

		var telemetryOut *nodeclient.TelemetrySummary
		if ts, err := cl.TelemetrySummary(r.Context()); err == nil {
			telemetryOut = &ts
		}

		writeJSON(w, 200, map[string]any{
			"health":    healthOut,
			"telemetry": telemetryOut,
		})
	}
}
