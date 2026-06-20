// Package api wires the HTTP surface of the Tower control plane.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NewRouter builds the HTTP handler. pool may be nil (health reports degraded).
func NewRouter(pool *pgxpool.Pool, secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", healthzHandler(pool))
	mux.HandleFunc("POST /auth/login", loginHandler(pool, secret))
	mux.HandleFunc("POST /auth/logout", logoutHandler())
	mux.HandleFunc("GET /auth/me", requireSession(secret, meHandler(pool)))
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
