package api

import (
	"net/http"
	"time"

	"github.com/qwwqq1000-arch/tower/internal/audit"
	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// auditFrom returns the caller's subject id (the actor) for audit attribution,
// or "" when no session is attached to the request.
func auditFrom(r *http.Request) string {
	if p, ok := sessionFrom(r); ok {
		return p.Sub
	}
	return ""
}

// recordAudit writes one audit entry for a mutating admin action, attributed to
// the caller. It is best-effort: a failed audit write must not fail the action
// the operator already performed, so the error is intentionally swallowed.
func recordAudit(r *http.Request, q *sqlc.Queries, action, target string, before, after any) {
	_ = audit.Record(r.Context(), q, time.Now().UnixMilli(), audit.Entry{
		Actor:  auditFrom(r),
		Action: action,
		Target: target,
		Before: before,
		After:  after,
	})
}
