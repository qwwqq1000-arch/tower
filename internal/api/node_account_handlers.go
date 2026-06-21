package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func updateNodeAccountHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Egress  string
			Weight  int32
			Role    string
			Enabled bool
			SlotId  string
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.UpdateNodeAccount(r.Context(), sqlc.UpdateNodeAccountParams{
			NodeID: r.PathValue("nodeId"), AccountID: r.PathValue("accountId"),
			Egress: b.Egress, Weight: b.Weight, Role: b.Role, Enabled: b.Enabled, SlotID: b.SlotId,
		}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
