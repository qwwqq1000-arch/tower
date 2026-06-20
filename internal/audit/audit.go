// Package audit records administrative actions (who changed what) for compliance.
package audit

import (
	"context"
	"encoding/json"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// Entry is one audit record's logical content.
type Entry struct {
	Actor  string
	Action string
	Target string
	Before any
	After  any
}

func toJSON(v any) []byte {
	if v == nil {
		return []byte(`{}`)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return []byte(`{}`)
	}
	return b
}

// Record writes one audit entry; before/after are JSON-encoded (empty object on error).
func Record(ctx context.Context, q *sqlc.Queries, ts int64, e Entry) error {
	return q.InsertAudit(ctx, sqlc.InsertAuditParams{
		Ts:     ts,
		Actor:  e.Actor,
		Action: e.Action,
		Target: e.Target,
		Before: toJSON(e.Before),
		After:  toJSON(e.After),
	})
}
