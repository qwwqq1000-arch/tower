package auth

import (
	"sync"
	"time"
)

// bucket tracks consecutive failures for a single throttle key.
type bucket struct {
	fails     int
	first     time.Time
	lockUntil time.Time
}

// Throttle is an in-memory, per-key failure tracker used to rate-limit and lock
// out repeated failed login attempts. Keys are caller-defined (e.g.
// "username|clientIP"). It is safe for concurrent use.
type Throttle struct {
	maxFails int
	window   time.Duration
	lockout  time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewThrottle builds a Throttle that locks a key for lockout once it accrues
// maxFails failures within the rolling window.
func NewThrottle(maxFails int, window, lockout time.Duration) *Throttle {
	return &Throttle{
		maxFails: maxFails,
		window:   window,
		lockout:  lockout,
		buckets:  make(map[string]*bucket),
	}
}

// Allowed reports whether an attempt for key may proceed at now. It returns
// false while the key is locked out. An expired failure window resets the
// bucket so stale failures don't accumulate indefinitely.
func (t *Throttle) Allowed(key string, now time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	b, ok := t.buckets[key]
	if !ok {
		// No tracked failures: allow.
		return true
	}
	switch {
	case now.Before(b.lockUntil):
		// Still within an active lockout: deny.
		return false
	case !b.lockUntil.IsZero():
		// Lockout was set and has now elapsed: forget the bucket and allow.
		delete(t.buckets, key)
		return true
	case now.Sub(b.first) > t.window:
		// Never locked, but the failure window has expired: forget the bucket
		// so stale failures don't accumulate, and allow.
		delete(t.buckets, key)
		return true
	default:
		// Within the failure window and not locked: allow.
		return true
	}
}

// RecordFailure registers a failed attempt for key at now, locking the key once
// it reaches maxFails within the window.
func (t *Throttle) RecordFailure(key string, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now)
	b, ok := t.buckets[key]
	if !ok || now.Sub(b.first) > t.window {
		b = &bucket{first: now}
		t.buckets[key] = b
	}
	b.fails++
	if b.fails >= t.maxFails {
		b.lockUntil = now.Add(t.lockout)
	}
}

// Reset clears any tracked failures for key (e.g. after a successful login).
func (t *Throttle) Reset(key string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.buckets, key)
}

// Sweep drops buckets that are no longer relevant: past their lockout and
// outside the failure window. Called opportunistically from RecordFailure.
func (t *Throttle) Sweep(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now)
}

func (t *Throttle) sweepLocked(now time.Time) {
	for k, b := range t.buckets {
		if !b.lockUntil.IsZero() {
			if !now.Before(b.lockUntil) {
				delete(t.buckets, k)
			}
			continue
		}
		if now.Sub(b.first) > t.window {
			delete(t.buckets, k)
		}
	}
}
