package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// setUserInternalHandler marks/unmarks a tenant as an internal employee (they
// receive auto-assigned accounts from the yanghao pool for aging).
func setUserInternalHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var b struct{ IsInternal bool }
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if err := q.SetTenantInternal(r.Context(), id, b.IsInternal); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}

func getAgingConfigHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := q.GetAgingConfig(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]any{
			"accountsPerEmployee": c.AccountsPerEmployee,
			"agingDays":           c.AgingDays,
			"enabled":             c.Enabled,
		})
	}
}

func setAgingConfigHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			AccountsPerEmployee int  `json:"accountsPerEmployee"`
			AgingDays           int  `json:"agingDays"`
			Enabled             bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid body"})
			return
		}
		if b.AccountsPerEmployee < 0 {
			b.AccountsPerEmployee = 0
		}
		if b.AgingDays < 1 {
			b.AgingDays = 1
		}
		if err := q.SetAgingConfig(r.Context(), b.AccountsPerEmployee, b.AgingDays, b.Enabled); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]bool{"ok": true})
	}
}
