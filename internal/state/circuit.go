// Package state holds the in-memory authoritative dispatch state engine.
package state

import "math"

// BreakerCfg configures a circuit breaker's threshold and backoff.
type BreakerCfg struct {
	PersistStreak int     // consecutive ban signals before opening (recoverable)
	PermStreak    int     // consecutive ban signals before PERMANENT ban (0 = never); takes precedence
	BaseMs        int64   // base cooldown
	MaxMs         int64   // cooldown cap
	Mult          float64 // backoff multiplier per reopen
}

// Breaker is a per-account circuit breaker. Zero value is a closed breaker.
type Breaker struct {
	streak    int   // consecutive ban signals
	failCount int   // number of opens (drives backoff)
	openUntil int64 // ms; 0 = closed
	trial     bool  // a half-open trial is in flight
	permanent bool  // permanently banned — never recovers, never half-opens
}

func backoffMs(cfg BreakerCfg, failCount int) int64 {
	if failCount < 1 {
		failCount = 1
	}
	d := float64(cfg.BaseMs) * math.Pow(cfg.Mult, float64(failCount-1))
	if d > float64(cfg.MaxMs) {
		return cfg.MaxMs
	}
	return int64(d)
}

func (b *Breaker) open(cfg BreakerCfg, now int64) {
	b.failCount++
	b.openUntil = now + backoffMs(cfg, b.failCount)
	b.trial = false
}

// OnBanSignal records a ban signal. Reaching PermStreak permanently bans the
// account (never recovers); otherwise reaching PersistStreak opens a recoverable
// breaker. Returns whether the breaker opened (either path).
func (b *Breaker) OnBanSignal(cfg BreakerCfg, now int64) (opened bool) {
	if b.permanent {
		return false
	}
	b.streak++
	if cfg.PermStreak > 0 && b.streak >= cfg.PermStreak {
		b.permanent = true
		b.open(cfg, now) // also set a cooldown verdict for persistence consistency
		return true
	}
	if b.streak >= cfg.PersistStreak {
		b.open(cfg, now)
		return true
	}
	return false
}

// Permanent reports whether the account is permanently banned.
func (b *Breaker) Permanent() bool { return b.permanent }

// OnSuccess clears all failure state (closes the breaker), including a permanent
// ban — used by manual recovery.
func (b *Breaker) OnSuccess() {
	b.streak = 0
	b.failCount = 0
	b.openUntil = 0
	b.trial = false
	b.permanent = false
}

// State returns "permanent", "closed", "open", or "half_open" at the given time.
// A permanently banned account is always "permanent" and never half-opens.
func (b *Breaker) State(now int64) string {
	if b.permanent {
		return "permanent"
	}
	if b.openUntil == 0 {
		return "closed"
	}
	if now >= b.openUntil {
		return "half_open"
	}
	return "open"
}

// TakeTrial returns true exactly once per half-open window, claiming the trial.
func (b *Breaker) TakeTrial(now int64) bool {
	if b.State(now) != "half_open" || b.trial {
		return false
	}
	b.trial = true
	return true
}

// OnTrialResult closes on success, or on failure escalates: a failed half-open
// recovery trial is itself a ban signal, so it advances the streak and trips the
// permanent ban once the streak reaches PermStreak (otherwise it reopens with a
// bigger backoff). Without this, an account that opens recoverably could never
// climb to a permanent ban, because after opening the only further signals come
// through trials — see OnBanSignal which handles the still-closed case.
func (b *Breaker) OnTrialResult(cfg BreakerCfg, now int64, ok bool) {
	if ok {
		b.OnSuccess()
		return
	}
	b.streak++
	if cfg.PermStreak > 0 && b.streak >= cfg.PermStreak {
		b.permanent = true
	}
	b.open(cfg, now)
}

// Snapshot exports the durable verdict (excludes the in-flight trial flag).
func (b *Breaker) Snapshot() (openUntil int64, streak, failCount int) {
	return b.openUntil, b.streak, b.failCount
}

// Restore loads a durable verdict (used for warm-start after restart).
func (b *Breaker) Restore(openUntil int64, streak, failCount int) {
	b.openUntil = openUntil
	b.streak = streak
	b.failCount = failCount
	b.trial = false
}

// SetPermanent sets the permanent-ban flag (used by warm-start restore).
func (b *Breaker) SetPermanent(p bool) { b.permanent = p }
