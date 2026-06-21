package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/nodeclient"
)

func nodeFeaturesGetHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, r, r.PathValue("id"))
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

func nodeFeaturesPatchHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, r, r.PathValue("id"))
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

func nodeRefreshHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, r, r.PathValue("id"))
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

func nodeTelemetryHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cl, _, ok := nodeClientFor(q, r, r.PathValue("id"))
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
