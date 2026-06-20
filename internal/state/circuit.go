// Package state holds the in-memory authoritative dispatch state engine.
package state

import "math"

// BreakerCfg configures a circuit breaker's threshold and backoff.
type BreakerCfg struct {
	PersistStreak int     // consecutive ban signals before opening
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

// OnBanSignal records a ban signal; opens only once streak reaches PersistStreak.
func (b *Breaker) OnBanSignal(cfg BreakerCfg, now int64) (opened bool) {
	b.streak++
	if b.streak >= cfg.PersistStreak {
		b.open(cfg, now)
		return true
	}
	return false
}

// OnSuccess clears all failure state (closes the breaker).
func (b *Breaker) OnSuccess() {
	b.streak = 0
	b.failCount = 0
	b.openUntil = 0
	b.trial = false
}

// State returns "closed", "open", or "half_open" at the given time.
func (b *Breaker) State(now int64) string {
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

// OnTrialResult closes on success or reopens (bigger backoff) on failure.
func (b *Breaker) OnTrialResult(cfg BreakerCfg, now int64, ok bool) {
	if ok {
		b.OnSuccess()
		return
	}
	b.open(cfg, now)
}
