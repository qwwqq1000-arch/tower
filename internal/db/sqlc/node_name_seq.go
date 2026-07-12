package sqlc

import "context"

// NextNodeNameSeq atomically allocates the next sequential node display name via a
// Postgres sequence. Batch provisioning fires concurrently (Promise.all), so a
// read-max-then-increment approach collided (all reads saw the same max before any
// job row committed). A sequence guarantees unique, monotonic names under any
// concurrency.
func (q *Queries) NextNodeNameSeq(ctx context.Context) (int64, error) {
	var n int64
	err := q.db.QueryRow(ctx, `SELECT nextval('node_name_seq')`).Scan(&n)
	return n, err
}
