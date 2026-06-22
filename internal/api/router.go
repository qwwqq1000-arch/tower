// Package api wires the HTTP surface of the Tower control plane.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/web"
)

// NewRouter builds the HTTP handler. pool may be nil (health reports degraded).
// svc and q may be nil for test/partial setups; the dispatch route is only registered when svc != nil.
func NewRouter(pool *pgxpool.Pool, secret string, svc *dispatch.Service, q *sqlc.Queries) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler(pool))
	mux.HandleFunc("POST /auth/login", loginHandler(pool, secret))
	mux.HandleFunc("POST /auth/logout", logoutHandler())
	mux.HandleFunc("GET /auth/me", requireSession(secret, meHandler(pool)))
	mux.HandleFunc("GET /api/admin/server-status", requireAdmin(secret, serverStatusHandler()))
	if svc != nil {
		mux.HandleFunc("POST /v1/messages", dispatchMessagesHandler(svc, q))
	}
	if q != nil {
		mux.HandleFunc("POST /api/admin/nodes", requireAdmin(secret, createNodeHandler(q)))
		mux.HandleFunc("GET /api/admin/nodes", requireAdmin(secret, listNodesHandler(q)))
		mux.HandleFunc("DELETE /api/admin/nodes/{id}", requireAdmin(secret, deleteNodeHandler(q)))
		mux.HandleFunc("POST /api/admin/dispatch-keys", requireAdmin(secret, createDispatchKeyHandler(q)))
		mux.HandleFunc("GET /api/admin/dispatch-keys", requireAdmin(secret, listDispatchKeysHandler(q)))
		mux.HandleFunc("DELETE /api/admin/dispatch-keys/{id}", requireAdmin(secret, deleteDispatchKeyHandler(q)))
		mux.HandleFunc("GET /api/dashboard", requireAdmin(secret, dashboardHandler(q, svc)))
		mux.HandleFunc("POST /api/admin/provision", requireAdmin(secret, startProvisionHandler(q)))
		mux.HandleFunc("GET /api/admin/provision/{id}", requireAdmin(secret, getProvisionHandler(q)))
		mux.HandleFunc("POST /api/admin/settle", requireAdmin(secret, settleHandler(pool, q)))
		mux.HandleFunc("GET /api/admin/ledger", requireAdmin(secret, ledgerHandler(q)))
		mux.HandleFunc("GET /api/admin/policies", requireAdmin(secret, listPoliciesHandler(q)))
		mux.HandleFunc("PUT /api/admin/policies/global", requireAdmin(secret, putGlobalPolicyHandler(q)))
		mux.HandleFunc("POST /api/admin/policies/dry-run", requireAdmin(secret, dryRunPolicyHandler()))
		mux.HandleFunc("GET /api/admin/desired", requireAdmin(secret, getDesiredHandler(q)))
		mux.HandleFunc("PUT /api/admin/desired", requireAdmin(secret, putDesiredHandler(q)))
		mux.HandleFunc("GET /api/admin/logs", requireAdmin(secret, listLogsHandler(q)))
		mux.HandleFunc("GET /api/admin/events", requireAdmin(secret, listEventsHandler(q)))
		mux.HandleFunc("GET /api/admin/audit", requireAdmin(secret, listAuditHandler(q)))
		mux.HandleFunc("GET /api/admin/accounts", requireAdmin(secret, listAccountsHandler(q)))
		mux.HandleFunc("DELETE /api/admin/accounts/{nodeId}/{accountId}", requireAdmin(secret, unassignAccountHandler(q)))
		mux.HandleFunc("PATCH /api/admin/accounts/{nodeId}/{accountId}", requireAdmin(secret, updateNodeAccountHandler(q)))
		mux.HandleFunc("PATCH /api/admin/accounts/{accountId}/expiry", requireAdmin(secret, setAccountExpiryHandler(q)))
		mux.HandleFunc("PATCH /api/admin/accounts/{accountId}/owner", requireAdmin(secret, setAccountOwnerHandler(q)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/profiles", requireAdmin(secret, listProfilesHandler(q)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/accounts/import", requireAdmin(secret, importProfileHandler(q)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/oauth/start", requireAdmin(secret, oauthStartHandler(q)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/oauth/exchange", requireAdmin(secret, oauthExchangeHandler(q)))
		mux.HandleFunc("GET /api/admin/dispatch/status", requireAdmin(secret, dispatchStatusHandler(q, svc)))
		mux.HandleFunc("GET /api/admin/dispatch/stream", requireAdmin(secret, dispatchStreamHandler(q, svc)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/features", requireAdmin(secret, nodeFeaturesGetHandler(q)))
		mux.HandleFunc("PATCH /api/admin/nodes/{id}/features/{adapter}", requireAdmin(secret, nodeFeaturesPatchHandler(q)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/refresh", requireAdmin(secret, nodeRefreshHandler(q)))
		mux.HandleFunc("PATCH /api/admin/nodes/{id}/enabled", requireAdmin(secret, nodeEnableHandler(q)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/telemetry", requireAdmin(secret, nodeTelemetryHandler(q)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/quota", requireAdmin(secret, nodeQuotaHandler(q)))
		mux.HandleFunc("GET /api/admin/ban-analysis", requireAdmin(secret, banAnalysisHandler(q)))
		mux.HandleFunc("GET /api/admin/slots", requireAdmin(secret, listSlotsHandler(q)))
		mux.HandleFunc("POST /api/admin/slots", requireAdmin(secret, createSlotHandler(q)))
		mux.HandleFunc("DELETE /api/admin/slots/{id}", requireAdmin(secret, deleteSlotHandler(q)))
		mux.HandleFunc("PATCH /api/admin/slots/{id}/enabled", requireAdmin(secret, setSlotEnabledHandler(q)))
		mux.HandleFunc("GET /api/admin/fallback-channels", requireAdmin(secret, listFallbackHandler(q)))
		mux.HandleFunc("POST /api/admin/fallback-channels", requireAdmin(secret, createFallbackHandler(q)))
		mux.HandleFunc("PATCH /api/admin/fallback-channels/{id}", requireAdmin(secret, updateFallbackHandler(q)))
		mux.HandleFunc("PATCH /api/admin/fallback-channels/{id}/enabled", requireAdmin(secret, enableFallbackHandler(q)))
		mux.HandleFunc("DELETE /api/admin/fallback-channels/{id}", requireAdmin(secret, deleteFallbackHandler(q)))
		mux.HandleFunc("POST /api/admin/fallback-channels/{id}/balance", requireAdmin(secret, fetchFallbackBalanceHandler(q)))
		mux.HandleFunc("GET /api/admin/users", requireAdmin(secret, listUsersHandler(q)))
		mux.HandleFunc("POST /api/admin/users", requireAdmin(secret, createUserHandler(q)))
		mux.HandleFunc("DELETE /api/admin/users/{id}", requireAdmin(secret, deleteUserHandler(q)))
		mux.HandleFunc("PATCH /api/admin/users/{id}/role", requireAdmin(secret, setUserRoleHandler(q)))
		mux.HandleFunc("PATCH /api/admin/users/{id}/hosting-rate", requireAdmin(secret, setUserHostingRateHandler(q)))
		mux.HandleFunc("PATCH /api/admin/users/{id}/channel-rate", requireAdmin(secret, setUserChannelRateHandler(q)))
		mux.HandleFunc("PATCH /api/admin/users/{id}/fallback-limit", requireAdmin(secret, setUserFallbackLimitHandler(q)))
		mux.HandleFunc("POST /auth/change-password", requireSession(secret, changePasswordHandler(q)))
		// Tenant self-service: strictly scoped to the caller's session sub.
		mux.HandleFunc("GET /api/me/accounts", requireSession(secret, meAccountsHandler(q)))
		mux.HandleFunc("POST /api/me/accounts/{accountId}/pause", requireSession(secret, mePauseAccountHandler(q)))
		mux.HandleFunc("GET /api/me/dashboard", requireSession(secret, meDashboardHandler(q)))
		mux.HandleFunc("GET /api/me/logs", requireSession(secret, meLogsHandler(q)))
		mux.HandleFunc("GET /api/me/events", requireSession(secret, meEventsHandler(q)))
		mux.HandleFunc("GET /api/me/ledger", requireSession(secret, meLedgerHandler(q)))
		mux.HandleFunc("GET /api/me/fallback-channels", requireSession(secret, meListFallbackHandler(q)))
		mux.HandleFunc("POST /api/me/fallback-channels", requireSession(secret, meCreateFallbackHandler(q)))
		mux.HandleFunc("PATCH /api/me/fallback-channels/{id}", requireSession(secret, meUpdateFallbackHandler(q)))
		mux.HandleFunc("DELETE /api/me/fallback-channels/{id}", requireSession(secret, meDeleteFallbackHandler(q)))
		mux.HandleFunc("PATCH /api/me/fallback-channels/{id}/enabled", requireSession(secret, meEnableFallbackHandler(q)))
		mux.HandleFunc("GET /api/me/slots", requireSession(secret, meListSlotsHandler(q)))
		mux.HandleFunc("POST /api/me/slots", requireSession(secret, meCreateSlotHandler(q)))
		mux.HandleFunc("DELETE /api/me/slots/{id}", requireSession(secret, meDeleteSlotHandler(q)))
		mux.HandleFunc("PATCH /api/me/slots/{id}/enabled", requireSession(secret, meSetSlotEnabledHandler(q)))
		mux.HandleFunc("GET /api/me/dispatch-keys", requireSession(secret, meListDispatchKeysHandler(q)))
		mux.HandleFunc("POST /api/me/dispatch-keys", requireSession(secret, meCreateDispatchKeyHandler(q)))
		mux.HandleFunc("DELETE /api/me/dispatch-keys/{id}", requireSession(secret, meDeleteDispatchKeyHandler(q)))
		mux.HandleFunc("GET /api/me/dispatch/status", requireSession(secret, meDispatchStatusHandler(q, svc)))
		mux.HandleFunc("GET /api/me/ban-analysis", requireSession(secret, meBanAnalysisHandler(q)))
	}
	mux.Handle("/", web.SPAHandler())
	return mux
}

func healthzHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		ok := false
		if pool != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			ok = pool.Ping(ctx) == nil
		}
		if !ok {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "degraded"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}
}
