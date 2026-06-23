package api

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/policy"
)

func readAll(r *http.Request) ([]byte, error) { return io.ReadAll(r.Body) }
func validJSON(b []byte) bool                 { return json.Valid(b) }

func limitParam(r *http.Request, def int32) int32 {
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 1000 {
			return int32(n)
		}
	}
	return def
}

func listPoliciesHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListPolicies(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, p := range rows {
			out = append(out, map[string]any{"scopeType": p.ScopeType, "scopeId": p.ScopeID, "params": json.RawMessage(p.Params)})
		}
		writeJSON(w, 200, out)
	}
}

func putGlobalPolicyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readAll(r)
		if !validJSON(raw) {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		// Merge the incoming patch over the existing global policy params so a
		// partial save only updates the provided keys (never wipes other settings).
		merged := map[string]json.RawMessage{}
		if rows, err := q.ListPolicies(r.Context()); err == nil {
			for _, p := range rows {
				if p.ScopeType == "global" {
					_ = json.Unmarshal(p.Params, &merged)
					break
				}
			}
		}
		var incoming map[string]json.RawMessage
		if err := json.Unmarshal(raw, &incoming); err != nil {
			writeJSON(w, 400, map[string]string{"error": "patch must be a JSON object"})
			return
		}
		for k, v := range incoming {
			merged[k] = v
		}
		out, err := json.Marshal(merged)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := q.UpsertPolicy(r.Context(), sqlc.UpsertPolicyParams{ScopeType: "global", ScopeID: "_", Params: out, UpdatedAt: time.Now().UnixMilli()}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "policy.update", "global", nil, json.RawMessage(out))
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

// putTenantPolicyHandler stores a per-tenant policy override (scope_type "owner",
// scope_id = the tenant/owner id from the path). The dispatch service resolves
// this layer over the global policy so a tenant's override wins. Like the global
// handler it merges the incoming patch over the existing tenant params so a
// partial save only updates the provided keys. Superadmin-gated by the router.
func putTenantPolicyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := r.PathValue("id")
		if tenant == "" {
			writeJSON(w, 400, map[string]string{"error": "tenant id required"})
			return
		}
		raw, _ := readAll(r)
		if !validJSON(raw) {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		// Merge the incoming patch over the existing tenant policy params so a
		// partial save only updates the provided keys (never wipes other settings).
		merged := map[string]json.RawMessage{}
		if rows, err := q.ListPolicies(r.Context()); err != nil {
			log.Printf("putTenantPolicyHandler: ListPolicies: %v", err)
		} else {
			for _, p := range rows {
				if p.ScopeType == "owner" && p.ScopeID == tenant {
					_ = json.Unmarshal(p.Params, &merged)
					break
				}
			}
		}
		var incoming map[string]json.RawMessage
		if err := json.Unmarshal(raw, &incoming); err != nil {
			writeJSON(w, 400, map[string]string{"error": "patch must be a JSON object"})
			return
		}
		for k, v := range incoming {
			merged[k] = v
		}
		out, err := json.Marshal(merged)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := q.UpsertPolicy(r.Context(), sqlc.UpsertPolicyParams{ScopeType: "owner", ScopeID: tenant, Params: out, UpdatedAt: time.Now().UnixMilli()}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "policy.update", "tenant:"+tenant, nil, json.RawMessage(out))
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

// loadStoredGlobalConfig reads the global policy row from the DB and returns
// the effective Config (Defaults merged with the stored global patch). If q is
// nil or no global row exists the returned Config is policy.Defaults() so the
// handler degrades gracefully without a DB.
func loadStoredGlobalConfig(ctx context.Context, q *sqlc.Queries) policy.Config {
	base := policy.Defaults()
	if q == nil {
		return base
	}
	rows, err := q.ListPolicies(ctx)
	if err != nil {
		return base
	}
	for _, p := range rows {
		if p.ScopeType == "global" {
			var stored policy.Patch
			if err := json.Unmarshal(p.Params, &stored); err == nil {
				return policy.Resolve(base, stored)
			}
			break
		}
	}
	return base
}

// dryRunPolicyHandler previews the effect of applying a policy patch over the
// currently stored effective policy (Defaults + stored global patch). This
// matches exactly what PUT /api/admin/policies/global will produce, so the
// preview is accurate rather than diffing against hard-coded Defaults.
func dryRunPolicyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var patch policy.Patch
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid patch"})
			return
		}
		current := loadStoredGlobalConfig(r.Context(), q)
		final, diffs := policy.DryRun(current, patch)
		writeJSON(w, 200, map[string]any{"final": final, "diffs": diffs})
	}
}

func getDesiredHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, err := q.GetDesiredFeatures(r.Context())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(raw)
	}
}

func putDesiredHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		raw, _ := readAll(r)
		if !validJSON(raw) {
			writeJSON(w, 400, map[string]string{"error": "invalid json"})
			return
		}
		if err := q.SetDesiredFeatures(r.Context(), sqlc.SetDesiredFeaturesParams{Features: raw, UpdatedAt: time.Now().UnixMilli()}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func listLogsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		limit := limitParam(r, 100)
		// Push the owner filter into SQL so LIMIT applies after filtering (events-audit-4).
		// A scoped admin calling with limit=100 would otherwise receive at most
		// 100 global rows pre-filtered, potentially returning fewer than limit rows.
		var rows []sqlc.DispatchLog
		var err error
		if all {
			rows, err = q.ListRecentDispatchLogs(r.Context(), limit)
		} else {
			rows, err = q.ListLogsByOwner(r.Context(), sqlc.ListLogsByOwnerParams{OwnerID: owner, Limit: limit})
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, l := range rows {
			out = append(out, map[string]any{
				"ts": l.Ts, "model": l.Model, "target": l.Target,
				"status": l.Status, "httpStatus": l.HttpStatus,
				"latencyMs": l.LatencyMs, "tokensIn": l.TokensIn,
				"tokensOut": l.TokensOut, "fallbackReason": l.FallbackReason,
				"ttfbMs": l.TtfbMs, "stream": l.Stream, "costUsd": l.CostUsd,
			})
		}
		writeJSON(w, 200, out)
	}
}

func listEventsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		limit := limitParam(r, 100)
		// Push the owner filter into SQL so LIMIT applies after filtering (events-audit-4).
		var rows []sqlc.DispatchEvent
		var err error
		if all {
			rows, err = q.ListRecentEvents(r.Context(), limit)
		} else {
			rows, err = q.ListEventsByOwner(r.Context(), sqlc.ListEventsByOwnerParams{OwnerID: owner, Limit: limit})
		}
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, e := range rows {
			out = append(out, map[string]any{
				"ts": e.Ts, "type": e.Type, "target": e.Target,
				"detail": json.RawMessage(e.Detail),
			})
		}
		writeJSON(w, 200, out)
	}
}

func listAuditHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// The audit log is a global record with no per-owner dimension; restrict
		// it to superadmin rather than leaking cross-owner activity.
		if _, all := scope(r); !all {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "superadmin required"})
			return
		}
		rows, err := q.ListRecentAudit(r.Context(), limitParam(r, 100))
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, a := range rows {
			out = append(out, map[string]any{
				"ts": a.Ts, "actor": a.Actor, "action": a.Action, "target": a.Target,
			})
		}
		writeJSON(w, 200, out)
	}
}
