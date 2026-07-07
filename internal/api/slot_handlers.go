package api

import (
	"encoding/json"
	"net/http"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func listSlotsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		owner, all := scope(r)
		rows, err := q.ListSlots(r.Context())
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, s := range rows {
			if !all && s.OwnerID != owner { // owner scoping: non-superadmin sees only own
				continue
			}
			out = append(out, map[string]any{
				"id":       s.ID,
				"name":     s.Name,
				"startMin": s.StartMin,
				"endMin":   s.EndMin,
				"enabled":  s.Enabled,
				"ownerId":  s.OwnerID,
			})
		}
		writeJSON(w, 200, out)
	}
}

// ownsSlotID reports whether the caller may act on the slot (superadmin or owner).
func ownsSlotID(r *http.Request, q *sqlc.Queries, id string) bool {
	owner, all := scope(r)
	if all {
		return true
	}
	s, err := q.GetSlot(r.Context(), id)
	if err != nil {
		return false
	}
	return s.OwnerID == owner
}

func createSlotHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Name     string `json:"name"`
			StartMin int32  `json:"startMin"`
			EndMin   int32  `json:"endMin"`
			OwnerId  string `json:"ownerId"`
		}
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, 400, map[string]string{"error": "bad body"})
			return
		}
		// owner default: a non-superadmin owns the slots it creates (so they remain
		// visible/editable under owner scoping instead of becoming orphaned).
		if owner, all := scope(r); !all {
			b.OwnerId = owner
		}
		s, err := q.CreateSlotOwned(r.Context(), sqlc.CreateSlotOwnedParams{
			ID:       randHex("slot_"),
			Name:     b.Name,
			StartMin: b.StartMin,
			EndMin:   b.EndMin,
			OwnerID:  b.OwnerId,
		})
		if err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "slot.create", "slot:"+s.ID, nil, map[string]any{"name": b.Name, "startMin": b.StartMin, "endMin": b.EndMin, "ownerId": b.OwnerId})
		writeJSON(w, 200, map[string]string{"id": s.ID})
	}
}

func deleteSlotHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !ownsSlotID(r, q, id) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
		if err := q.DeleteSlot(r.Context(), id); err != nil {
			writeJSON(w, 500, map[string]string{"error": err.Error()})
			return
		}
		recordAudit(r, q, "slot.delete", "slot:"+id, nil, nil)
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}

func setSlotEnabledHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !ownsSlotID(r, q, id) {
			writeJSON(w, 403, map[string]string{"error": "forbidden"})
			return
		}
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
		recordAudit(r, q, "slot.enable", "slot:"+id, nil, map[string]any{"enabled": b.Enabled})
		writeJSON(w, 200, map[string]string{"ok": "true"})
	}
}
