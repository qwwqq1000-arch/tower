package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/cpaclient"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

func nodeFeaturesGetHandler(q *sqlc.Queries, cipher *crypto.Cipher) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
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
		cl, _, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
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
		cl, _, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
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

// isCPAKind reports whether a node kind string identifies a CLIProxyAPI node.
// Extracted as a pure helper so it can be unit-tested independently of the DB.
func isCPAKind(kind string) bool { return strings.EqualFold(kind, "cpa") }

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
		if u, uerr := c.Usage(ctx, a.DispatchSelector()); uerr == nil {
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
		cl, _, ok := nodeClientFor(q, cipher, r, r.PathValue("id"))
		if !ok {
			writeJSON(w, 404, map[string]string{"error": "node not found"})
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
