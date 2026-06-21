package api

import (
	"context"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/auth"
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
