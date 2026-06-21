package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

func randHex(prefix string) string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

func createNodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Name, BaseUrl, ApiKey, OwnerId string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.BaseUrl == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name/baseUrl required"})
			return
		}
		n, err := q.CreateNode(r.Context(), sqlc.CreateNodeParams{
			ID: randHex("n_"), Name: body.Name, BaseUrl: body.BaseUrl, ApiKey: body.ApiKey, OwnerID: body.OwnerId,
		})
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "enabled": n.Enabled})
	}
}

func listNodesHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, n := range rows {
			out = append(out, map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "ownerId": n.OwnerID, "enabled": n.Enabled, "version": n.Version})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteNodeHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := q.DeleteNode(r.Context(), id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func createDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Label, OwnerId string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		plaintext, prefix, hash, salt, err := auth.NewDispatchKey()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		id := randHex("k_")
		if _, err := q.CreateDispatchKey(r.Context(), sqlc.CreateDispatchKeyParams{
			ID: id, KeyHash: hash, Salt: salt, Prefix: prefix, OwnerID: body.OwnerId, Label: body.Label,
		}); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"id": id, "key": plaintext})
	}
}

func listDispatchKeysHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListAllDispatchKeys(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		out := make([]map[string]any, 0, len(rows))
		for _, k := range rows {
			out = append(out, map[string]any{"id": k.ID, "prefix": k.Prefix, "label": k.Label, "ownerId": k.OwnerID, "enabled": k.Enabled})
		}
		writeJSON(w, http.StatusOK, out)
	}
}

func deleteDispatchKeyHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := q.DisableDispatchKey(r.Context(), r.PathValue("id")); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
	}
}

func dashboardHandler(q *sqlc.Queries, svc *dispatch.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rows, err := q.ListNodes(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		nodes := make([]map[string]any, 0, len(rows))
		for _, n := range rows {
			m := map[string]any{"id": n.ID, "name": n.Name, "baseUrl": n.BaseUrl, "enabled": n.Enabled, "ownerId": n.OwnerID}
			nodes = append(nodes, m)
		}
		writeJSON(w, http.StatusOK, map[string]any{"nodes": nodes})
	}
}

// adminID is a tiny path-suffix helper: not used (we use r.PathValue).
var _ = strings.TrimPrefix
