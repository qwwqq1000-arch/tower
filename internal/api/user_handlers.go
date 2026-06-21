package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

var validRoles = map[string]bool{"superadmin": true, "admin": true, "operator": true, "tenant": true, "viewer": true}

func listUsersHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ts, err := q.ListTenantsBasic(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(ts))
		for _, t := range ts {
			rate, _ := q.GetHostingRate(r.Context(), t.ID)
			out = append(out, map[string]any{"id": t.ID, "username": t.Username, "role": t.Role, "rate": rate})
		}
		writeJSON(w, 200, out)
	}
}

func createUserHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Username, Password, Role string }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || b.Username == "" || len(b.Password) < 8 {
			writeJSON(w, 400, map[string]string{"error": "username and password(>=8) required"})
			return
		}
		if b.Role == "" {
			b.Role = "tenant"
		}
		if !validRoles[b.Role] {
			writeJSON(w, 400, map[string]string{"error": "invalid role"})
			return
		}
		hash, salt, err := auth.HashPassword(b.Password)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		id := randHex("u_")
		if _, err := q.CreateTenant(r.Context(), sqlc.CreateTenantParams{
			ID: id, Username: b.Username, PwHash: hash, Salt: salt, Role: b.Role, IngestKey: randHex("ik_"),
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": id})
	}
}

func deleteUserHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if p, ok := sessionFrom(r); ok && p.Sub == id {
			writeJSON(w, 400, map[string]string{"error": "cannot delete yourself"})
			return
		}
		if err := q.DeleteTenant(r.Context(), id); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func setUserRoleHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Role string }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || !validRoles[b.Role] {
			writeJSON(w, 400, map[string]string{"error": "invalid role"})
			return
		}
		if err := q.SetTenantRole(r.Context(), sqlc.SetTenantRoleParams{ID: r.PathValue("id"), Role: b.Role}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func setUserHostingRateHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct{ Rate float64 }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetHostingRate(r.Context(), sqlc.SetHostingRateParams{TenantID: r.PathValue("id"), Rate: b.Rate, EffectiveFrom: time.Now().UnixMilli()}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func changePasswordHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := sessionFrom(r)
		if !ok {
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		var b struct{ OldPassword, NewPassword string }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil || len(b.NewPassword) < 8 {
			writeJSON(w, 400, map[string]string{"error": "newPassword must be >= 8"})
			return
		}
		u, err := q.GetTenantByID(r.Context(), p.Sub)
		if err != nil {
			auth.DummyVerify(b.OldPassword) // equalize timing; always false
			writeJSON(w, 401, map[string]string{"error": "old password incorrect"})
			return
		}
		if !auth.VerifyPassword(b.OldPassword, u.PwHash, u.Salt) {
			writeJSON(w, 401, map[string]string{"error": "old password incorrect"})
			return
		}
		hash, salt, err := auth.HashPassword(b.NewPassword)
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		if err := q.SetTenantPassword(r.Context(), sqlc.SetTenantPasswordParams{ID: p.Sub, PwHash: hash, Salt: salt}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
