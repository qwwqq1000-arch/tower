package api

import (
	"net/http"
	"strconv"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

func listInterceptedSecretsHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit := 50
		offset := 0
		if v := r.URL.Query().Get("limit"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
				limit = n
			}
		}
		if v := r.URL.Query().Get("offset"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				offset = n
			}
		}
		rows, _ := q.ListInterceptedSecrets(r.Context(), sqlc.ListInterceptedSecretsParams{
			Limit:  int32(limit),
			Offset: int32(offset),
		})
		if rows == nil {
			rows = []sqlc.InterceptedSecret{}
		}
		total, _ := q.CountInterceptedSecrets(r.Context())
		writeJSON(w, 200, map[string]any{"items": rows, "total": total})
	}
}

func getInterceptedSecretHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid id"})
			return
		}
		row, err := q.GetInterceptedSecret(r.Context(), id)
		if err != nil {
			writeJSON(w, 404, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, 200, row)
	}
}

func deleteInterceptedSecretHandler(q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeJSON(w, 400, map[string]string{"error": "invalid id"})
			return
		}
		_ = q.DeleteInterceptedSecret(r.Context(), id)
		writeJSON(w, 200, map[string]string{"status": "ok"})
	}
}
