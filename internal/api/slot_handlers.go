package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func listSlotsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListSlots(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, s := range rows {
			out = append(out, map[string]any{
				"id":       s.ID,
				"name":     s.Name,
				"startMin": s.StartMin,
				"endMin":   s.EndMin,
				"enabled":  s.Enabled,
			})
		}
		writeJSON(w, 200, out)
	}
}

func createSlotHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Name     string `json:"name"`
			StartMin int32  `json:"startMin"`
			EndMin   int32  `json:"endMin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		s, err := q.CreateSlot(r.Context(), sqlc.CreateSlotParams{
			ID:       randHex("slot_"),
			Name:     b.Name,
			StartMin: b.StartMin,
			EndMin:   b.EndMin,
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"id": s.ID})
	}
}

func deleteSlotHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := q.DeleteSlot(r.Context(), id); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func setSlotEnabledHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		var b struct {
			Enabled bool `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		if err := q.SetSlotEnabled(r.Context(), sqlc.SetSlotEnabledParams{ID: id, Enabled: b.Enabled}); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
