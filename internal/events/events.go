// Package events records dispatch timeline events and ban episodes.
package events

import (
	"context"
	"encoding/json"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// Event is one dispatch timeline event.
type Event struct {
	Type    string
	Target  string
	OwnerID string
	Detail  any
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

// Record writes one dispatch event (detail JSON-encoded; '{}' on nil/error).
func Record(ctx context.Context, q *sqlc.Queries, ts int64, e Event) error {
	return q.InsertDispatchEvent(ctx, sqlc.InsertDispatchEventParams{
		Ts: ts, Type: e.Type, Target: e.Target, OwnerID: e.OwnerID, Detail: toJSON(e.Detail),
	})
}
