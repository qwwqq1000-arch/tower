package state

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/qwwqq1000-arch/tower/internal/db/sqlc"
)

// Store is the thread-safe in-memory registry of per-account state.
type Store struct {
	mu    sync.Mutex
	now   func() int64
	rnd   func(min, max int64) int64
	accts map[string]*Account
}

// NewStore builds a Store with injected clock and RNG (for deterministic tests).
func NewStore(clock func() int64, rnd func(min, max int64) int64) *Store {
	return &Store{now: clock, rnd: rnd, accts: map[string]*Account{}}
}

// ensureLocked returns (creating if needed) the account; caller MUST hold s.mu.
func (s *Store) ensureLocked(key string, capacity int) *Account {
	a := s.accts[key]
	if a == nil {
		a = NewAccount(capacity)
		s.accts[key] = a
	}
	return a
}

// Ensure returns the account for key, creating it with capacity if absent.
func (s *Store) Ensure(key string, capacity int) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ensureLocked(key, capacity)
}

// WithLock runs fn while holding the store lock (for compound atomic ops).
func (s *Store) WithLock(fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn()
}

// RestoreBreaker warm-starts one account's durable verdict atomically (single lock).
// Ephemeral state (slots, trial) is not affected. Intended for startup warm-start.
func (s *Store) RestoreBreaker(key string, capacity int, openUntil int64, streak, failCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accts[key]
	if a == nil {
		a = NewAccount(capacity)
		s.accts[key] = a
	}
	a.Breaker.Restore(openUntil, streak, failCount)
}

// TryDispatch atomically checks eligibility and, if dispatchable, acquires a
// slot (claiming a half-open trial when applicable). Returns whether dispatched.
func (s *Store) TryDispatch(key, model string, cfg BreakerCfg) bool {
	ok, _ := s.TryDispatchTrial(key, model, cfg)
	return ok
}

// TryDispatchTrial is like TryDispatch but also reports whether the dispatched
// attempt claimed a half-open recovery trial (so the caller settles via OnTrialResult).
func (s *Store) TryDispatchTrial(key, model string, cfg BreakerCfg) (ok bool, trial bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accts[key]
	if a == nil {
		return false, false
	}
	now := s.now()
	can, tr := a.CanDispatch(now, model, cfg)
	if !can {
		return false, false
	}
	if tr {
		a.Breaker.TakeTrial(now)
	}
	a.Slots.Acquire(now)
	return true, tr
}

// OnTrialResult settles a half-open trial: success closes the breaker, failure
// reopens it (always clearing the in-flight trial flag — no wedge possible).
func (s *Store) OnTrialResult(key string, cfg BreakerCfg, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.accts[key]; a != nil {
		a.Breaker.OnTrialResult(cfg, s.now(), ok)
	}
}

// Complete releases one slot with a randomized cooldown in [min,max] ms.
func (s *Store) Complete(key string, cooldownMin, cooldownMax int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accts[key]
	if a == nil {
		return
	}
	cd := int64(0)
	if cooldownMax > 0 {
		cd = s.rnd(cooldownMin, cooldownMax)
	}
	a.Slots.Release(s.now(), cd)
}

// OnSuccess records a successful response (closes the breaker / resolves trial).
func (s *Store) OnSuccess(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.accts[key]; a != nil {
		a.Breaker.OnSuccess()
	}
}

// SetClock replaces the clock (test/helper use; not for concurrent runtime swaps).
func (s *Store) SetClock(clock func() int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.now = clock
}

// OnBanSignal records a ban signal; returns whether the breaker opened.
func (s *Store) OnBanSignal(key string, cfg BreakerCfg) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accts[key]
	if a == nil {
		return false
	}
	return a.Breaker.OnBanSignal(cfg, s.now())
}

// NodeStatus returns an aggregate status for all accounts whose key begins with
// nodeID+":". Priority order: disabled > banned > half_open > active.
// Returns "" if no accounts are found for the node.
func (s *Store) NodeStatus(nodeID string) string {
	prefix := nodeID + ":"
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	rank := map[string]int{"disabled": 3, "banned": 2, "half_open": 1, "active": 0}
	best := ""
	for k, a := range s.accts {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		st := a.Status(now)
		if best == "" || rank[st] > rank[best] {
			best = st
		}
	}
	return best
}

// SetLimited replaces an account's model-class rate-limit map (creating it if absent).
func (s *Store) SetLimited(key string, capacity int, limits map[string]int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.ensureLocked(key, capacity)
	a.LimitedUntil = limits
}

// SetOffline marks an account online/offline.
func (s *Store) SetOffline(key string, capacity int, offline bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(key, capacity).Offline = offline
}

// SetDisabled marks an account enabled/disabled.
func (s *Store) SetDisabled(key string, capacity int, disabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked(key, capacity).Disabled = disabled
}

// SetCapacity updates the slot capacity for an existing account (no-op if absent).
func (s *Store) SetCapacity(key string, cap int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.accts[key]
	if !ok {
		return
	}
	a.Slots.SetCapacity(cap)
}

// SetWarmupCap sets the warmup concurrency cap for an existing account (no-op if absent).
// cap=0 disables warmup limiting.
func (s *Store) SetWarmupCap(key string, cap int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if a := s.accts[key]; a != nil {
		a.WarmupCap = cap
	}
}

// AccountSnapshot is a read-only view of one account's live state.
type AccountSnapshot struct {
	Key       string
	Status    string
	Inflight  int
	Available int
}

// Snapshot returns a sorted, point-in-time view of every account's live state.
func (s *Store) Snapshot(now int64) []AccountSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AccountSnapshot, 0, len(s.accts))
	for key, a := range s.accts {
		avail := a.Slots.Available(now)
		if a.WarmupCap > 0 {
			warmupAvail := a.WarmupCap - a.Slots.InUse()
			if warmupAvail < 0 {
				warmupAvail = 0
			}
			if warmupAvail < avail {
				avail = warmupAvail
			}
		}
		out = append(out, AccountSnapshot{
			Key: key, Status: a.Status(now), Inflight: a.Slots.InUse(), Available: avail,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// accountDurable holds a snapshot of an account's durable fields for PersistAll.
type accountDurable struct {
	nodeID    string
	profileID string
	account   Account // shallow copy (Breaker is a value type, safe to copy)
	now       int64
}

// PersistAll persists every node:profile account's durable verdict to the DB.
// It copies durable fields under the lock and then performs DB writes without
// holding the lock, preventing mutex + I/O deadlock risk.
// Keys prefixed with "fb:" (fallback channel slots) are skipped.
// Errors are best-effort; the last error encountered is returned.
func (s *Store) PersistAll(ctx context.Context, q *sqlc.Queries, now int64) error {
	s.mu.Lock()
	snapshots := make([]accountDurable, 0, len(s.accts))
	for key, a := range s.accts {
		if strings.HasPrefix(key, "fb:") {
			continue
		}
		i := strings.LastIndex(key, ":")
		if i < 0 {
			continue
		}
		snapshots = append(snapshots, accountDurable{
			nodeID:    key[:i],
			profileID: key[i+1:],
			// Copy the Account value; Breaker and scalar fields are value types.
			account: Account{
				Breaker:   a.Breaker,
				Slots:     a.Slots,   // pointer — only durable Breaker fields are read by SaveState
				Disabled:  a.Disabled,
				Offline:   a.Offline,
			},
			now: now,
		})
	}
	s.mu.Unlock()

	var lastErr error
	for i := range snapshots {
		sn := &snapshots[i]
		if err := SaveState(ctx, q, sn.nodeID, sn.profileID, &sn.account, sn.now); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
