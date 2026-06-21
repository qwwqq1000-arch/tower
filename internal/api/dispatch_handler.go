package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/qwwqq1000-arch/tower/internal/auth"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
	"github.com/qwwqq1000-arch/tower/internal/dispatch"
)

func extractKey(r *http.Request) string {
	if k := r.Header.Get("x-api-key"); k != "" {
		return k
	}
	if a := r.Header.Get("Authorization"); strings.HasPrefix(a, "Bearer ") {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

// resolveDispatchOwner verifies a dk_ key and returns its owner_id.
func resolveDispatchOwner(r *http.Request, q *sqlc.Queries) (string, bool) {
	key := extractKey(r)
	prefix := auth.PrefixOf(key)
	if prefix == "" || q == nil {
		return "", false
	}
	rows, err := q.GetDispatchKeysByPrefix(r.Context(), prefix)
	if err != nil {
		return "", false
	}
	for _, row := range rows {
		if auth.VerifyDispatchKey(key, row.KeyHash, row.Salt) {
			return row.OwnerID, true
		}
	}
	return "", false
}

func dispatchMessagesHandler(svc *dispatch.Service, q *sqlc.Queries) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ownerID, ok := resolveDispatchOwner(r, q)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid dispatch key"})
			return
		}
		body, _ := io.ReadAll(r.Body)
		var parsed struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &parsed)
		if parsed.Stream {
			svc.DispatchStream(r.Context(), w, ownerID, parsed.Model, body)
			return
		}
		out := svc.Dispatch(r.Context(), ownerID, parsed.Model, string(body), body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write([]byte(out.Body))
	}
}
