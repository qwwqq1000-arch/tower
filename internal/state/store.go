package state

import "sync"

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

// Ensure returns the account for key, creating it with capacity if absent.
func (s *Store) Ensure(key string, capacity int) *Account {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accts[key]
	if a == nil {
		a = NewAccount(capacity)
		s.accts[key] = a
	}
	return a
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
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.accts[key]
	if a == nil {
		return false
	}
	now := s.now()
	ok, trial := a.CanDispatch(now, model, cfg)
	if !ok {
		return false
	}
	if trial {
		a.Breaker.TakeTrial(now)
	}
	a.Slots.Acquire(now)
	return true
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
