// Package api wires the HTTP surface of the Tower control plane.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/crypto"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
	"github.com/qwwqq1000-arch/tower/web"
)

// NewRouter builds the HTTP handler. pool may be nil (health reports degraded).
// svc and q may be nil for test/partial setups; the dispatch route is only registered when svc != nil.
// secureCookies controls the Secure flag on the session cookie; set true only for TLS deployments.
// cipher is the runtime master-key cipher (vault-crypto-1) made available to
// handlers that encrypt secrets on write / decrypt on read (vault-crypto-2/3); it
// may be nil in tests that do not exercise secret persistence.
func NewRouter(pool *pgxpool.Pool, secret string, svc *dispatch.Service, q *sqlc.Queries, secureCookies bool, cipher *crypto.Cipher) http.Handler {
	mux := http.NewServeMux()
	loginThrottle := auth.NewThrottle(5, time.Minute, 15*time.Minute)
	// DB-backed permission loader for requirePerm (turns the seeded role
	// permissions into real server-side authz on the sensitive routes below).
	loadRolePerms := func(r *http.Request, role string) []string { return loadPerms(pool, r, role) }
	mux.HandleFunc("GET /healthz", healthzHandler(pool))
	mux.HandleFunc("POST /auth/login", loginHandler(pool, secret, loginThrottle, secureCookies))
	mux.HandleFunc("POST /auth/logout", requireSameOrigin(logoutHandler()))
	mux.HandleFunc("GET /auth/me", requireSession(secret, q, meHandler(pool)))
	mux.HandleFunc("GET /api/admin/server-status", requireAdmin(secret, q, serverStatusHandler()))
	if svc != nil {
		mux.HandleFunc("POST /v1/messages", dispatchMessagesHandler(svc, q))
	}
	if q != nil {
		mux.HandleFunc("POST /api/admin/nodes", requireAdmin(secret, q, createNodeHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/nodes", requireAdmin(secret, q, listNodesHandler(q, cipher)))
		mux.HandleFunc("DELETE /api/admin/nodes/{id}", requireAdmin(secret, q, deleteNodeHandler(pool, q, svc)))
		mux.HandleFunc("POST /api/admin/dispatch-keys", requireAdmin(secret, q, createDispatchKeyHandler(q)))
		mux.HandleFunc("GET /api/admin/dispatch-keys", requireAdmin(secret, q, listDispatchKeysHandler(q)))
		mux.HandleFunc("DELETE /api/admin/dispatch-keys/{id}", requireAdmin(secret, q, deleteDispatchKeyHandler(q)))
		mux.HandleFunc("GET /api/dashboard", requireAdmin(secret, q, dashboardHandler(q, svc)))
		mux.HandleFunc("POST /api/admin/provision", requireAdmin(secret, q, startProvisionHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/provision/{id}", requireAdmin(secret, q, getProvisionHandler(q)))
		mux.HandleFunc("POST /api/admin/settle", requireSuperadmin(secret, q, requirePerm(secret, q, loadRolePerms, "billing:settle", settleHandler(pool, q))))
		mux.HandleFunc("GET /api/admin/ledger", requireAdmin(secret, q, ledgerHandler(q)))
		mux.HandleFunc("GET /api/admin/policies", requireAdmin(secret, q, listPoliciesHandler(q)))
		mux.HandleFunc("PUT /api/admin/policies/global", requireSuperadmin(secret, q, putGlobalPolicyHandler(q)))
		mux.HandleFunc("PUT /api/admin/policies/tenant/{id}", requireSuperadmin(secret, q, putTenantPolicyHandler(q)))
		mux.HandleFunc("POST /api/admin/policies/dry-run", requireSuperadmin(secret, q, dryRunPolicyHandler(q)))
		mux.HandleFunc("GET /api/admin/desired", requireAdmin(secret, q, getDesiredHandler(q)))
		mux.HandleFunc("PUT /api/admin/desired", requireAdmin(secret, q, putDesiredHandler(q)))
		mux.HandleFunc("GET /api/admin/logs", requireAdmin(secret, q, listLogsHandler(q)))
		mux.HandleFunc("GET /api/admin/logs/detail", requireAdmin(secret, q, logDetailHandler(q)))
		mux.HandleFunc("GET /api/admin/events", requireAdmin(secret, q, listEventsHandler(q)))
		mux.HandleFunc("GET /api/admin/audit", requireAdmin(secret, q, listAuditHandler(q)))
		mux.HandleFunc("GET /api/admin/accounts", requireAdmin(secret, q, listAccountsHandler(q, svc)))
		mux.HandleFunc("DELETE /api/admin/accounts/{nodeId}/{accountId}", requireAdmin(secret, q, unassignAccountHandler(q)))
		mux.HandleFunc("PATCH /api/admin/accounts/{nodeId}/{accountId}", requireAdmin(secret, q, updateNodeAccountHandler(q)))
		mux.HandleFunc("PATCH /api/admin/accounts/{accountId}/expiry", requireAdmin(secret, q, setAccountExpiryHandler(q)))
		mux.HandleFunc("PATCH /api/admin/accounts/{accountId}/owner", requireAdmin(secret, q, setAccountOwnerHandler(q)))
		mux.HandleFunc("POST /api/admin/accounts/{accountId}/recover", requireAdmin(secret, q, recoverAccountHandler(q, svc)))
		mux.HandleFunc("POST /api/admin/accounts/refresh-quota", requireAdmin(secret, q, accountsRefreshQuotaHandler(q, cipher)))
		mux.HandleFunc("POST /api/admin/accounts/{accountId}/refresh-quota", requireAdmin(secret, q, accountRefreshQuotaHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/profiles", requireAdmin(secret, q, listProfilesHandler(q, cipher)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/accounts/import", requireAdmin(secret, q, importProfileHandler(q, cipher)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/oauth/start", requireAdmin(secret, q, oauthStartHandler(q, cipher)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/oauth/exchange", requireAdmin(secret, q, oauthExchangeHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/dispatch/status", requireAdmin(secret, q, dispatchStatusHandler(q, svc)))
		mux.HandleFunc("GET /api/admin/dispatch/stream", requireAdmin(secret, q, dispatchStreamHandler(q, svc)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/features", requireAdmin(secret, q, nodeFeaturesGetHandler(q, cipher)))
		mux.HandleFunc("PATCH /api/admin/nodes/{id}/features/{adapter}", requireAdmin(secret, q, nodeFeaturesPatchHandler(q, cipher)))
		mux.HandleFunc("POST /api/admin/nodes/{id}/refresh", requireAdmin(secret, q, nodeRefreshHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/console-url", requireAdmin(secret, q, nodeConsoleURLHandler(q, cipher)))
		mux.HandleFunc("PATCH /api/admin/nodes/{id}/enabled", requireAdmin(secret, q, nodeEnableHandler(q)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/telemetry", requireAdmin(secret, q, nodeTelemetryHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/nodes/{id}/quota", requireAdmin(secret, q, nodeQuotaHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/ban-analysis", requireAdmin(secret, q, banAnalysisHandler(q)))
		mux.HandleFunc("GET /api/admin/slots", requireAdmin(secret, q, listSlotsHandler(q)))
		mux.HandleFunc("POST /api/admin/slots", requireAdmin(secret, q, createSlotHandler(q)))
		mux.HandleFunc("DELETE /api/admin/slots/{id}", requireAdmin(secret, q, deleteSlotHandler(q)))
		mux.HandleFunc("PATCH /api/admin/slots/{id}/enabled", requireAdmin(secret, q, setSlotEnabledHandler(q)))
		mux.HandleFunc("GET /api/admin/fallback-channels", requireAdmin(secret, q, listFallbackHandler(q)))
		mux.HandleFunc("POST /api/admin/fallback-channels", requireAdmin(secret, q, createFallbackHandler(q, cipher)))
		mux.HandleFunc("PATCH /api/admin/fallback-channels/{id}", requireAdmin(secret, q, updateFallbackHandler(q, cipher)))
		mux.HandleFunc("PATCH /api/admin/fallback-channels/{id}/enabled", requireAdmin(secret, q, enableFallbackHandler(q)))
		mux.HandleFunc("DELETE /api/admin/fallback-channels/{id}", requireAdmin(secret, q, deleteFallbackHandler(q)))
		mux.HandleFunc("POST /api/admin/fallback-channels/{id}/balance", requireAdmin(secret, q, fetchFallbackBalanceHandler(q, cipher)))
		mux.HandleFunc("GET /api/admin/users", requireSuperadmin(secret, q, listUsersHandler(q)))
		mux.HandleFunc("POST /api/admin/users", requireSuperadmin(secret, q, requirePerm(secret, q, loadRolePerms, "users:manage", createUserHandler(q))))
		mux.HandleFunc("DELETE /api/admin/users/{id}", requireSuperadmin(secret, q, requirePerm(secret, q, loadRolePerms, "users:manage", deleteUserHandler(q))))
		mux.HandleFunc("PATCH /api/admin/users/{id}/role", requireSuperadmin(secret, q, requirePerm(secret, q, loadRolePerms, "users:manage", setUserRoleHandler(q))))
		mux.HandleFunc("PATCH /api/admin/users/{id}/hosting-rate", requireSuperadmin(secret, q, setUserHostingRateHandler(q)))
		mux.HandleFunc("PATCH /api/admin/users/{id}/channel-rate", requireSuperadmin(secret, q, setUserChannelRateHandler(q)))
		mux.HandleFunc("PATCH /api/admin/users/{id}/fallback-limit", requireSuperadmin(secret, q, setUserFallbackLimitHandler(q)))
		mux.HandleFunc("POST /auth/change-password", requireSession(secret, q, changePasswordHandler(secret, q, secureCookies)))
		// Tenant self-service: strictly scoped to the caller's session sub.
		mux.HandleFunc("GET /api/me/accounts", requireSession(secret, q, meAccountsHandler(q, svc)))
		mux.HandleFunc("POST /api/me/accounts/{accountId}/pause", requireSession(secret, q, mePauseAccountHandler(q)))
		mux.HandleFunc("GET /api/me/dashboard", requireSession(secret, q, meDashboardHandler(q)))
		mux.HandleFunc("GET /api/me/logs", requireSession(secret, q, meLogsHandler(q)))
		mux.HandleFunc("GET /api/me/logs/detail", requireSession(secret, q, meLogDetailHandler(q)))
		mux.HandleFunc("GET /api/me/events", requireSession(secret, q, meEventsHandler(q)))
		mux.HandleFunc("GET /api/me/ledger", requireSession(secret, q, meLedgerHandler(q)))
		mux.HandleFunc("GET /api/me/fallback-channels", requireSession(secret, q, meListFallbackHandler(q)))
		mux.HandleFunc("POST /api/me/fallback-channels", requireSession(secret, q, meCreateFallbackHandler(q, cipher)))
		mux.HandleFunc("PATCH /api/me/fallback-channels/{id}", requireSession(secret, q, meUpdateFallbackHandler(q, cipher)))
		mux.HandleFunc("DELETE /api/me/fallback-channels/{id}", requireSession(secret, q, meDeleteFallbackHandler(q)))
		mux.HandleFunc("PATCH /api/me/fallback-channels/{id}/enabled", requireSession(secret, q, meEnableFallbackHandler(q)))
		mux.HandleFunc("GET /api/me/slots", requireSession(secret, q, meListSlotsHandler(q)))
		mux.HandleFunc("POST /api/me/slots", requireSession(secret, q, meCreateSlotHandler(q)))
		mux.HandleFunc("DELETE /api/me/slots/{id}", requireSession(secret, q, meDeleteSlotHandler(q)))
		mux.HandleFunc("PATCH /api/me/slots/{id}/enabled", requireSession(secret, q, meSetSlotEnabledHandler(q)))
		mux.HandleFunc("GET /api/me/dispatch-keys", requireSession(secret, q, meListDispatchKeysHandler(q)))
		mux.HandleFunc("POST /api/me/dispatch-keys", requireSession(secret, q, meCreateDispatchKeyHandler(q)))
		mux.HandleFunc("DELETE /api/me/dispatch-keys/{id}", requireSession(secret, q, meDeleteDispatchKeyHandler(q)))
		mux.HandleFunc("GET /api/me/dispatch/status", requireSession(secret, q, meDispatchStatusHandler(q, svc)))
		mux.HandleFunc("GET /api/me/ban-analysis", requireSession(secret, q, meBanAnalysisHandler(q)))
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
