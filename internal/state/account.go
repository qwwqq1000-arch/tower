package state

// Account aggregates the operational state of one (node, profile) account.
type Account struct {
	Breaker      Breaker
	Slots        *Slots
	Disabled     bool
	Offline      bool
	LimitedUntil map[string]int64 // model class ("opus"/"sonnet"/"all") -> reset time ms
	WarmupCap    int              // 0 = no warmup limit; >0 = max in-flight during warmup
	CoolUntil    int64            // ms; temporary error-cooldown (e.g. 429); 0 = none. Ephemeral.
}

// NewAccount builds an account with a slot set of the given capacity.
func NewAccount(capacity int) *Account {
	return &Account{Slots: NewSlots(capacity), LimitedUntil: map[string]int64{}}
}

// Status returns the account's headline state at now
// (priority: disabled > offline > permanent > banned > half_open > active).
func (a *Account) Status(now int64) string {
	if a.Disabled {
		return "disabled"
	}
	if a.Offline {
		return "offline"
	}
	bs := a.Breaker.State(now)
	if bs == "permanent" {
		return "permanent"
	}
	// An active error-cooldown (e.g. 429) takes precedence over a recoverable
	// breaker state when it outlasts the breaker's recovery time, so a rate-limit
	// shows as 限流·冷却 for its full duration instead of a shorter 封控·冷却.
	if now < a.CoolUntil && a.CoolUntil >= a.Breaker.RecoverAt() {
		return "cooldown"
	}
	switch bs {
	case "open":
		return "banned"
	case "half_open":
		return "half_open"
	}
	return "active"
}

// limitedFor reports whether the model class is rate-limited at now.
func (a *Account) limitedFor(now int64, model string) bool {
	if a.LimitedUntil == nil {
		return false
	}
	if until, ok := a.LimitedUntil["all"]; ok && now < until {
		return true
	}
	if until, ok := a.LimitedUntil[model]; ok && now < until {
		return true
	}
	return false
}

// CanDispatch reports whether the account may take a request for model at now,
// and whether this dispatch would be a half-open recovery trial. It has no side
// effects; the caller claims the trial via Breaker.TakeTrial when trial is true.
func (a *Account) CanDispatch(now int64, model string, cfg BreakerCfg) (ok bool, trial bool) {
	if a.Disabled {
		return false, false
	}
	if a.Offline {
		return false, false
	}
	if now < a.CoolUntil { // temporary error-cooldown (e.g. 429)
		return false, false
	}
	if a.limitedFor(now, model) {
		return false, false
	}
	if a.Slots.Available(now) <= 0 {
		return false, false
	}
	// Warmup cap: effective concurrency = min(slot capacity, WarmupCap).
	if a.WarmupCap > 0 && a.Slots.InUse() >= a.WarmupCap {
		return false, false
	}
	switch a.Breaker.State(now) {
	case "closed":
		return true, false
	case "half_open":
		// Only eligible if no trial is already in flight.
		if a.Breaker.trial {
			return false, false
		}
		return true, true
	default: // open
		return false, false
	}
}
