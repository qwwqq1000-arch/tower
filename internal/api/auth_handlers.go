package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

const sessionTTLSec = 30 * 24 * 3600

func nowUnix() int64 { return time.Now().Unix() }

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func loginHandler(pool *pgxpool.Pool, secret string, throttle *auth.Throttle, secureCookies bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Username, Password string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
			return
		}
		key := body.Username + "|" + clientIP(r)
		if !throttle.Allowed(key, time.Now()) {
			writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts"})
			return
		}
		q := sqlc.New(pool)
		u, err := q.GetTenantByUsername(r.Context(), body.Username)
		var ok bool
		if err != nil {
			auth.DummyVerify(body.Password) // burn equivalent time
			ok = false
		} else {
			ok = auth.VerifyPassword(body.Password, u.PwHash, u.Salt)
		}
		if !ok {
			throttle.RecordFailure(key, time.Now())
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		throttle.Reset(key)
		tok := auth.IssueSession(secret, u.ID, u.Role, u.SessionEpoch, nowUnix(), sessionTTLSec)
		http.SetCookie(w, &http.Cookie{
			Name: "tower_session", Value: tok, Path: "/",
			HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: sessionTTLSec,
			Secure: secureCookies,
		})
		writeJSON(w, http.StatusOK, map[string]string{"role": u.Role})
	}
}

func meHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := sessionFrom(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		perms := loadPerms(pool, r, p.Role)
		writeJSON(w, http.StatusOK, map[string]any{"sub": p.Sub, "role": p.Role, "perms": perms})
	}
}

func loadPerms(pool *pgxpool.Pool, r *http.Request, role string) []string {
	var raw []byte
	err := pool.QueryRow(r.Context(), `SELECT permissions FROM roles WHERE name=$1`, role).Scan(&raw)
	if err != nil {
		return []string{}
	}
	var perms []string
	_ = json.Unmarshal(raw, &perms)
	return perms
}

func logoutHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "tower_session", Value: "", Path: "/", MaxAge: -1})
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}
