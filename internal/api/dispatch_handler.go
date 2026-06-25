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

// maxDispatchBodyBytes caps the inbound /v1/messages body (security-audit:
// prevent memory exhaustion from an unbounded upload on a valid dispatch key).
const maxDispatchBodyBytes = 64 << 20 // 64 MiB

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
		// Cap the inbound body so a valid dispatch key can't exhaust memory with an
		// unbounded upload (security-audit). 64MiB comfortably fits max-context +
		// image requests; oversized bodies get a 413 from MaxBytesReader.
		r.Body = http.MaxBytesReader(w, r.Body, maxDispatchBodyBytes)
		body, err := io.ReadAll(r.Body)
		if err != nil {
			writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return
		}
		var parsed struct {
			Model  string `json:"model"`
			Stream bool   `json:"stream"`
		}
		_ = json.Unmarshal(body, &parsed)
		// Pure passthrough: carry the client's original request headers so the proxy
		// forwards them verbatim upstream (only auth/host/account-pin are re-set).
		// Generate the request id here (not inside Dispatch) so we can record the
		// response detail after dispatch returns (logs-detail-2).
		ctx := dispatch.WithClientHeaders(r.Context(), r.Header.Clone())
		ctx = dispatch.WithRequestID(ctx, dispatch.NewRequestID())
		if parsed.Stream {
			out := svc.DispatchStream(ctx, w, ownerID, parsed.Model, body)
			svc.UpdateRequestDetailResponse(ctx, out.Status, out.Body)
			return
		}
		out := svc.Dispatch(ctx, ownerID, parsed.Model, string(body), body)
		svc.UpdateRequestDetailResponse(ctx, out.Status, out.Body)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(out.Status)
		_, _ = w.Write([]byte(out.Body))
	}
}
