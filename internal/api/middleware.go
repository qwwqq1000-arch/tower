package api

import (
	"context"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

type ctxKey string

const ctxKeySession ctxKey = "session"

// requireSession rejects requests without a valid tower_session cookie and
// injects the verified payload into the request context.
func requireSession(secret string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie("tower_session")
		if err != nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		p, ok := auth.VerifySession(secret, c.Value, nowUnix())
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxKeySession, p)
		next(w, r.WithContext(ctx))
	}
}

func sessionFrom(r *http.Request) (auth.SessionPayload, bool) {
	p, ok := r.Context().Value(ctxKeySession).(auth.SessionPayload)
	return p, ok
}

// scope returns the owner-id filter for the caller and whether the caller sees
// everything. superadmin → ("", true) = no filter; every other role →
// (callerSub, false) = restrict to resources they own. Callers must apply the
// filter themselves (skip non-matching owner_id rows when all is false).
func scope(r *http.Request) (ownerID string, all bool) {
	p, ok := sessionFrom(r)
	if !ok {
		return "", false
	}
	if p.Role == "superadmin" {
		return "", true
	}
	return p.Sub, false
}

// requireAdmin wraps requireSession and additionally requires an admin role.
// Role hierarchy: superadmin >= admin >= operator all get full admin access.
// Tenant and viewer roles are handled by future scoped endpoints.
func requireAdmin(secret string, next http.HandlerFunc) http.HandlerFunc {
	return requireSession(secret, func(w http.ResponseWriter, r *http.Request) {
		p, ok := sessionFrom(r)
		if !ok || (p.Role != "superadmin" && p.Role != "admin" && p.Role != "operator") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		next(w, r)
	})
}

// requireSuperadmin restricts a route to the superadmin role. Used for global,
// non-owner-scoped management (user/tenant administration) where an owner_id
// basis does not exist and where allowing admin/operator would permit privilege
// escalation (e.g. promoting oneself to superadmin).
func requireSuperadmin(secret string, next http.HandlerFunc) http.HandlerFunc {
	return requireSession(secret, func(w http.ResponseWriter, r *http.Request) {
		p, ok := sessionFrom(r)
		if !ok || p.Role != "superadmin" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "superadmin required"})
			return
		}
		next(w, r)
	})
}

// ownsNodeID reports whether the caller may act on the node: superadmin always,
// otherwise only when the node's owner matches the caller. Missing node → false.
func ownsNodeID(r *http.Request, q *sqlc.Queries, nodeID string) bool {
	owner, all := scope(r)
	if all {
		return true
	}
	n, err := q.GetNode(r.Context(), nodeID)
	if err != nil {
		return false
	}
	return n.OwnerID == owner
}

// ownsAccountID reports whether the caller may act on the account (superadmin or owner).
func ownsAccountID(r *http.Request, q *sqlc.Queries, accountID string) bool {
	owner, all := scope(r)
	if all {
		return true
	}
	a, err := q.GetAccount(r.Context(), accountID)
	if err != nil {
		return false
	}
	return a.OwnerID == owner
}

// ownsFallbackID reports whether the caller may act on the fallback channel.
func ownsFallbackID(r *http.Request, q *sqlc.Queries, id string) bool {
	owner, all := scope(r)
	if all {
		return true
	}
	c, err := q.GetFallbackChannel(r.Context(), id)
	if err != nil {
		return false
	}
	return c.OwnerID == owner
}
