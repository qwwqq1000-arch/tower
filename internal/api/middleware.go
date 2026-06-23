package api

import (
	"context"
	"net"
	"net/http"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/rbac"
)

// clientIP extracts the caller's source IP.
//
// X-Forwarded-For is only trusted when the actual TCP peer (RemoteAddr) is a
// loopback address (127.0.0.1 / ::1), which is the case when Tower sits behind
// a local reverse proxy (e.g. nginx on the same host). When Tower is exposed
// directly, XFF is attacker-controlled and must NOT be used — doing so would
// allow an attacker to spoof their IP and bypass the login throttle.
func clientIP(r *http.Request) string {
	remoteHost, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteHost == "" {
		remoteHost = r.RemoteAddr
	}
	// Only honour XFF when the immediate peer is a trusted loopback proxy.
	if remoteHost == "127.0.0.1" || remoteHost == "::1" {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			first, _, _ := strings.Cut(xff, ",")
			if ip := strings.TrimSpace(first); ip != "" {
				return ip
			}
		}
	}
	return remoteHost
}

type ctxKey string

const ctxKeySession ctxKey = "session"

// requireSession rejects requests without a valid tower_session cookie and
// injects the verified payload into the request context.
//
// q may be nil (test/partial setups that wire requireSession without a DB); in
// that case only the token's signature and expiry are checked. When q is
// present, the token's epoch is additionally compared against the user's current
// session_epoch in the DB so that a role or password change (which bumps the
// epoch) immediately revokes outstanding tokens, and a deleted user (no row) is
// rejected.
func requireSession(secret string, q *sqlc.Queries, next http.HandlerFunc) http.HandlerFunc {
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
		if q != nil {
			epoch, err := q.GetSessionEpoch(r.Context(), p.Sub)
			if err != nil || epoch != p.Epoch {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
		ctx := context.WithValue(r.Context(), ctxKeySession, p)
		// Delegate CSRF enforcement to requireSameOrigin so that the check
		// lives in exactly one place and the name on the call-site matches
		// the function that actually rejects the request.
		requireSameOrigin(next)(w, r.WithContext(ctx))
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
func requireAdmin(secret string, q *sqlc.Queries, next http.HandlerFunc) http.HandlerFunc {
	return requireSession(secret, q, func(w http.ResponseWriter, r *http.Request) {
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
func requireSuperadmin(secret string, q *sqlc.Queries, next http.HandlerFunc) http.HandlerFunc {
	return requireSession(secret, q, func(w http.ResponseWriter, r *http.Request) {
		p, ok := sessionFrom(r)
		if !ok || p.Role != "superadmin" {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "superadmin required"})
			return
		}
		next(w, r)
	})
}

// permLoader returns the capability strings granted to a role. It is a seam so
// requirePerm can be exercised without a database; the router passes the
// DB-backed loadPerms.
type permLoader func(r *http.Request, role string) []string

// requirePerm wraps requireSession and additionally enforces that the caller's
// seeded role permissions grant capability via rbac.Can. This turns the
// otherwise-decorative role permissions into real server-side authz
// (defense in depth atop the coarse role gates). superadmin holds the "*"
// wildcard and so passes any capability; a role lacking the capability gets 403.
func requirePerm(secret string, q *sqlc.Queries, load permLoader, capability string, next http.HandlerFunc) http.HandlerFunc {
	return requireSession(secret, q, func(w http.ResponseWriter, r *http.Request) {
		p, ok := sessionFrom(r)
		if !ok {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		perms := load(r, p.Role)
		if !rbac.Can(perms, capability, rbac.Scope{IsAdmin: p.Role == "superadmin"}) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
			return
		}
		next(w, r)
	})
}

// requireSameOrigin is the CSRF guard for cookie-authenticated mutation routes.
// It requires that all state-changing requests (any method other than GET and
// HEAD) carry the header "X-Requested-With: tower". The SPA sends this header
// on every non-GET fetch; a cross-origin attacker's form/script cannot set
// custom headers, so the check stops CSRF even when SameSite=Lax allows the
// cookie to be sent (e.g. top-level navigation POST). GET/HEAD are exempted
// because they must be safe (idempotent, no state change) per HTTP semantics.
//
// requireSession calls this function internally so that the CSRF logic lives
// in exactly one place; every cookie-auth route goes through requireSession
// and therefore through this check automatically.
func requireSameOrigin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			if r.Header.Get("X-Requested-With") != "tower" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "csrf check failed"})
				return
			}
		}
		next(w, r)
	}
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
