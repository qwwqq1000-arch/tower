package state

import (
	"context"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// stateKey is the store key convention for a (node, profile) account.
func stateKey(nodeID, profileID string) string { return nodeID + ":" + profileID }

// SaveState persists the durable verdict of an account.
func SaveState(ctx context.Context, q *sqlc.Queries, nodeID, profileID string, a *Account, now int64) error {
	openUntil, streak, failCount := a.Breaker.Snapshot()
	return q.UpsertAccountState(ctx, sqlc.UpsertAccountStateParams{
		NodeID:        nodeID,
		ProfileID:     profileID,
		Status:        a.Status(now),
		CooldownUntil: openUntil,
		BanStreak:     int32(streak),
		FailCount:     int32(failCount),
		Permanent:     a.Breaker.Permanent(),
		UpdatedAt:     now,
	})
}

// LoadStates warm-starts the store from persisted verdicts. Ephemeral state
// (in-flight slots, slot cooldowns, half-open trial) is intentionally not restored.
func LoadStates(ctx context.Context, q *sqlc.Queries, store *Store, capacity int) error {
	rows, err := q.ListAccountState(ctx)
	if err != nil {
		return err
	}
	for _, r := range rows {
		store.RestoreBreaker(stateKey(r.NodeID, r.ProfileID), capacity, r.CooldownUntil, int(r.BanStreak), int(r.FailCount), r.Permanent)
	}
	return nil
}
